package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// appDataAccess restores the permissions of the files startup needs when they refuse the current
// account, as happens to an app data folder copied with the permissions of another Windows account.
type appDataAccess struct {
	denied  func(path string) bool
	confirm func(message string) (bool, error)
	files   elevated.FileGateway
	locale  func() string
}

// appDataAccessError is the startup failure shown when denied entries were not restored. Its text
// is the dialog message; cause keeps the underlying failure for the log.
type appDataAccessError struct {
	message string
	paths   []string
	cause   error
}

func (e *appDataAccessError) Error() string { return e.message }

func (e *appDataAccessError) Unwrap() error { return e.cause }

type appDataAccessTexts struct {
	denied      string
	confirm     string
	notRestored string
}

var appDataAccessTextsByLanguage = map[string]appDataAccessTexts{
	"en": {
		denied:      "This Windows account cannot read or write the following Nahida Desktop data files:",
		confirm:     "Restore the permissions of these files for this account? Administrator approval is required.",
		notRestored: "The permissions were not restored. Start Nahida Desktop again to retry, or reset the permissions of these files for this account manually.",
	},
	"ja": {
		denied:      "この Windows アカウントは次の Nahida Desktop データファイルを読み書きできません。",
		confirm:     "このアカウントに対してこれらのファイルのアクセス許可を復元しますか？管理者の承認が必要です。",
		notRestored: "アクセス許可は復元されませんでした。Nahida Desktop を再起動して再試行するか、このアカウントに対するこれらのファイルのアクセス許可を手動でリセットしてください。",
	},
	"ko": {
		denied:      "현재 Windows 계정이 다음 Nahida Desktop 데이터 파일을 읽거나 쓸 수 없습니다.",
		confirm:     "이 계정에 대한 파일 권한을 복구할까요? 관리자 승인이 필요합니다.",
		notRestored: "권한을 복구하지 않았습니다. Nahida Desktop을 다시 실행해 다시 시도하거나, 이 계정에 대한 파일 권한을 직접 재설정하세요.",
	},
	"zh": {
		denied:      "当前 Windows 帐户无法读取或写入以下 Nahida Desktop 数据文件：",
		confirm:     "要为此帐户恢复这些文件的权限吗？需要管理员批准。",
		notRestored: "权限未恢复。请重新启动 Nahida Desktop 重试，或手动为此帐户重置这些文件的权限。",
	},
}

// startupAppDataEntries lists the entries startup opens for writing, each parent before its children.
// The DB folder is listed because SQLite creates the -wal and -shm files in it; a development or
// NAHIDA_DB_PATH database lives outside the app data root.
func startupAppDataEntries(data *appdata.Store, dbPath string) []string {
	root := data.Root()
	logs := filepath.Join(root, appdata.LogsDir)
	entries := []string{root, logs, infra.DesktopLogPath(logs)}
	if dbFolder := filepath.Dir(dbPath); !platform.SamePathFold(dbFolder, root) {
		entries = append(entries, dbFolder)
	}
	return append(entries,
		dbPath,
		dbPath+"-wal",
		dbPath+"-shm",
		filepath.Join(root, appdata.AgentDir),
		filepath.Join(root, appdata.AgentSkillsDir),
	)
}

// restore asks to restore the entries of paths whose permissions refuse the current account and
// returns the restored ones. A nil failure with no restored entries means nothing was denied.
func (a appDataAccess) restore(ctx context.Context, paths []string) ([]string, *appDataAccessError) {
	if a.denied == nil {
		return nil, nil
	}
	denied := slices.DeleteFunc(slices.Clone(paths), func(path string) bool { return !a.denied(path) })
	if len(denied) == 0 {
		return nil, nil
	}

	texts := appDataAccessTextsFor(a.locale)
	listed := texts.denied + "\n\n" + strings.Join(denied, "\n")
	fail := func(cause error) *appDataAccessError {
		message := listed + "\n\n" + texts.notRestored + "\n\n" + cause.Error()
		return &appDataAccessError{message: message, paths: denied, cause: cause}
	}
	ops, err := resetAccessOps(denied)
	if err != nil {
		return nil, fail(err)
	}

	// The restore hands each target to this account, and a link below the app data folder can point
	// anywhere, so the prompt names the resolved targets the helper will change.
	targets := make([]string, 0, len(ops))
	for _, op := range ops {
		targets = append(targets, op.Target)
	}
	accepted, err := a.confirm(texts.denied + "\n\n" + strings.Join(targets, "\n") + "\n\n" + texts.confirm)
	if err != nil {
		return nil, fail(err)
	}
	if !accepted {
		return nil, fail(errors.New("the permission restore was declined"))
	}

	// A folder that refuses the account does not list, so an 8.3 alias below it keeps its short spelling
	// until the folder is restored, and the helper refuses a target spelled differently from the path
	// Windows resolves. Each target is expanded again once the ops before it are applied, under the same
	// lease so that it still costs one UAC prompt.
	lease := elevated.NewFileLease(a.files)
	defer lease.Release()
	expanded := make([]string, 0, len(ops))
	for i, op := range ops {
		folder := filepath.Dir(op.Target)
		if parent := slices.IndexFunc(
			ops[:i],
			func(other elevated.FileOp) bool { return platform.SamePathFold(other.Target, folder) },
		); parent >= 0 {
			folder = expanded[parent]
		}
		op.Target = filepath.Join(folder, longEntryName(folder, filepath.Base(op.Target)))
		expanded = append(expanded, op.Target)
		if err := lease.Apply(ctx, []elevated.FileOp{op}); err != nil {
			return nil, fail(err)
		}
	}
	still := slices.DeleteFunc(slices.Clone(denied), func(path string) bool { return !a.denied(path) })
	if len(still) > 0 {
		return nil, fail(fmt.Errorf("access is still denied after the restore: %s", strings.Join(still, ", ")))
	}
	return denied, nil
}

// resetAccessOps builds a reset-access for each path in order. A parent restored earlier in the same
// request is taken from the earlier result because it may not open to be resolved either.
func resetAccessOps(paths []string) ([]elevated.FileOp, error) {
	ops := make([]elevated.FileOp, 0, len(paths))
	for i, path := range paths {
		restored := func(parent string) (string, bool) {
			index := slices.IndexFunc(
				paths[:i],
				func(other string) bool { return platform.SamePathFold(other, parent) },
			)
			if index < 0 {
				return "", false
			}
			return ops[index].Target, true
		}
		target, err := resetAccessTarget(path, restored)
		if err != nil {
			return nil, err
		}
		ops = append(ops, elevated.FileOp{Kind: elevated.FileOpResetAccess, Target: target})
	}
	return ops, nil
}

// resetAccessTarget resolves path to the entry a reset-access changes. The helper accepts only the
// path Windows resolves an entry to and refuses a link, so a junction or symbolic link is replaced by
// its destination. An entry that refuses the account may not open to be resolved; it then becomes
// its resolved parent joined with its long name.
func resetAccessTarget(path string, restored func(parent string) (string, bool)) (string, error) {
	const maxLinkHops = 8
	for range maxLinkHops {
		if final, err := platform.FinalPath(path); err == nil {
			return final, nil
		}

		// A link whose destination refuses the account does not open, but the link itself still reads.
		link, err := os.Readlink(path)
		if err != nil {
			break
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(path), link)
		}
		path = link
	}

	parent := filepath.Dir(path)
	folder, ok := restored(parent)
	if !ok {
		final, err := platform.FinalPath(parent)
		if err != nil {
			return "", err
		}
		folder = final
	}
	return filepath.Join(folder, longEntryName(folder, filepath.Base(path))), nil
}

// longEntryName returns the long name of the entry name in folder. The helper compares the target with the
// path Windows resolves, so an 8.3 alias such as DATABA~1.DB must not reach it. Listing the folder needs no
// access to the entry itself; the name is kept when the folder does not list.
func longEntryName(folder, name string) string {
	pattern, err := windows.UTF16PtrFromString(filepath.Join(folder, name))
	if err != nil {
		return name
	}
	var data windows.Win32finddata
	handle, err := windows.FindFirstFile(pattern, &data)
	if err != nil {
		return name
	}
	_ = windows.FindClose(handle)

	// A wildcard in name would match another entry, so only an entry this name refers to is taken.
	long := windows.UTF16ToString(data.FileName[:])
	if !strings.EqualFold(long, name) && !strings.EqualFold(windows.UTF16ToString(data.AlternateFileName[:]), name) {
		return name
	}
	return long
}

func appDataAccessTextsFor(locale func() string) appDataAccessTexts {
	language := ""
	if locale != nil {
		language, _, _ = strings.Cut(strings.ReplaceAll(locale(), "_", "-"), "-")
	}
	if texts, ok := appDataAccessTextsByLanguage[strings.ToLower(strings.TrimSpace(language))]; ok {
		return texts
	}
	return appDataAccessTextsByLanguage["en"]
}
