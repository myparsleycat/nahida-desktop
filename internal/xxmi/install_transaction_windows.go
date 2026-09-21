//go:build windows

package xxmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	installStatePrepared      = "prepared"
	installStateTreeBackedUp  = "tree-backed-up"
	installStateTreeInstalled = "tree-installed"
	installStateCommitted     = "committed"
)

type importerInstallJournal struct {
	State   string `json:"state"`
	HadLive bool   `json:"hadLive"`
}

type importerInstallTransaction struct {
	parent       *installRoot
	configRoot   *installRoot
	liveName     string
	stageName    string
	backupName   string
	journalName  string
	configBackup string
	configName   string
	hadLive      bool
	liveIdentity os.FileInfo
	configRaw    []byte
	configInfo   os.FileInfo
	state        string
}

func beginImporterInstallTransaction(
	ctx context.Context,
	importerFolder string,
	configPath string,
	importerKey string,
) (*importerInstallTransaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	importerAbsolute, err := filepath.Abs(importerFolder)
	if err != nil {
		return nil, err
	}
	importerAbsolute = filepath.Clean(importerAbsolute)
	if filepath.Dir(importerAbsolute) == importerAbsolute {
		return nil, errors.New("importer folder cannot be a volume root")
	}
	parent, err := openInstallRoot(filepath.Dir(importerAbsolute))
	if err != nil {
		return nil, err
	}

	configAbsolute, err := filepath.Abs(configPath)
	if err != nil {
		_ = parent.Close()
		return nil, err
	}
	configAbsolute = filepath.Clean(configAbsolute)
	configRoot, err := openInstallRoot(filepath.Dir(configAbsolute))
	if err != nil {
		_ = parent.Close()
		return nil, err
	}

	prefix := ".nahida-" + strings.ToLower(importerKey) + "-install"
	transaction := &importerInstallTransaction{
		parent:       parent,
		configRoot:   configRoot,
		liveName:     filepath.Base(importerAbsolute),
		stageName:    prefix + ".stage",
		backupName:   prefix + ".backup",
		journalName:  prefix + ".json",
		configBackup: prefix + ".config-backup",
		configName:   filepath.Base(configAbsolute),
	}
	if err := transaction.recoverInterrupted(); err != nil {
		_ = transaction.Close()
		return nil, fmt.Errorf("recover interrupted importer installation: %w", err)
	}

	configRaw, configInfo, err := transaction.configRoot.readFile(transaction.configName)
	if err != nil {
		_ = transaction.Close()
		return nil, err
	}
	transaction.configRaw = configRaw
	transaction.configInfo = configInfo

	liveInfo, err := transaction.parent.childInfo(transaction.liveName)
	switch {
	case err == nil:
		if !liveInfo.IsDir() {
			_ = transaction.Close()
			return nil, fmt.Errorf("importer folder is not a directory: %q", importerAbsolute)
		}
		transaction.hadLive = true
		transaction.liveIdentity = liveInfo
	case errors.Is(err, os.ErrNotExist):
	default:
		_ = transaction.Close()
		return nil, err
	}
	return transaction, nil
}

func (t *importerInstallTransaction) Close() error {
	return errors.Join(t.configRoot.Close(), t.parent.Close())
}

func (t *importerInstallTransaction) prepare(ctx context.Context) (*installRoot, error) {
	if err := t.writeJournal(ctx, installStatePrepared); err != nil {
		return nil, err
	}
	if err := t.parent.writeFileAtomic(
		ctx, t.configBackup, bytes.NewReader(t.configRaw), 0o600, nil,
	); err != nil {
		return nil, err
	}
	if err := t.parent.root.Mkdir(t.stageName, 0o755); err != nil {
		return nil, err
	}
	stage, err := t.parent.openChild(t.stageName)
	if err != nil {
		return nil, err
	}
	if !t.hadLive {
		return stage, nil
	}
	live, err := t.parent.openChild(t.liveName)
	if err != nil {
		_ = stage.Close()
		return nil, err
	}
	defer func() { _ = live.Close() }()
	liveInfo, err := live.root.Stat(".")
	if err != nil {
		_ = stage.Close()
		return nil, err
	}
	if !os.SameFile(t.liveIdentity, liveInfo) {
		_ = stage.Close()
		return nil, errors.New("importer folder identity changed before staging")
	}
	if err := copyTreeRoots(ctx, live, stage, nil); err != nil {
		_ = stage.Close()
		return nil, err
	}
	return stage, nil
}

func (t *importerInstallTransaction) commit(
	ctx context.Context,
	configJSON []byte,
) (map[string]any, parsedConfig, error) {
	currentConfig, currentInfo, err := t.configRoot.readFile(t.configName)
	if err != nil {
		return nil, parsedConfig{}, err
	}
	if !os.SameFile(t.configInfo, currentInfo) || !bytes.Equal(t.configRaw, currentConfig) {
		return nil, parsedConfig{}, errors.New("XXMI configuration changed during installation")
	}
	if err := ctx.Err(); err != nil {
		return nil, parsedConfig{}, err
	}

	if t.hadLive {
		currentLive, err := t.parent.childInfo(t.liveName)
		if err != nil {
			return nil, parsedConfig{}, err
		}
		if !os.SameFile(t.liveIdentity, currentLive) {
			return nil, parsedConfig{}, errors.New("importer folder identity changed before replacement")
		}
		if err := t.parent.renameChild(t.liveName, t.backupName); err != nil {
			return nil, parsedConfig{}, err
		}
	}
	if err := t.writeJournal(context.Background(), installStateTreeBackedUp); err != nil {
		return nil, parsedConfig{}, err
	}
	if err := t.parent.renameChild(t.stageName, t.liveName); err != nil {
		return nil, parsedConfig{}, err
	}
	if err := t.writeJournal(context.Background(), installStateTreeInstalled); err != nil {
		return nil, parsedConfig{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, parsedConfig{}, err
	}

	if err := t.configRoot.writeFileAtomic(
		context.Background(), t.configName, bytes.NewReader(configJSON), 0o644, t.configInfo,
	); err != nil {
		return nil, parsedConfig{}, err
	}
	written, _, err := t.configRoot.readFile(t.configName)
	if err != nil {
		return nil, parsedConfig{}, err
	}
	loaded, parsed, err := parseAndValidateConfig(written)
	if err != nil {
		return nil, parsedConfig{}, err
	}
	if err := t.writeJournal(context.Background(), installStateCommitted); err != nil {
		return nil, parsedConfig{}, err
	}
	t.state = installStateCommitted
	return loaded, parsed, nil
}

func (t *importerInstallTransaction) rollback() error {
	journal := importerInstallJournal{State: t.state, HadLive: t.hadLive}
	return t.rollbackJournal(context.Background(), journal)
}

func (t *importerInstallTransaction) finish() error {
	var result error
	for _, name := range []string{t.backupName, t.stageName, t.configBackup} {
		result = errors.Join(result, t.parent.removeAll(name))
	}
	if result != nil {
		return result
	}
	return t.parent.removeAll(t.journalName)
}

func (t *importerInstallTransaction) recoverInterrupted() error {
	raw, _, err := t.parent.readFile(t.journalName)
	if errors.Is(err, os.ErrNotExist) {
		for _, name := range []string{t.stageName, t.backupName, t.configBackup} {
			if _, childErr := t.parent.childInfo(name); childErr == nil {
				return fmt.Errorf("installation artifact %q exists without a recovery journal", name)
			} else if !errors.Is(childErr, os.ErrNotExist) {
				return childErr
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	var journal importerInstallJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		return err
	}
	switch journal.State {
	case installStatePrepared, installStateTreeBackedUp, installStateTreeInstalled:
		return t.rollbackJournal(context.Background(), journal)
	case installStateCommitted:
		return t.finish()
	default:
		return fmt.Errorf("invalid importer installation journal state %q", journal.State)
	}
}

func (t *importerInstallTransaction) rollbackJournal(ctx context.Context, journal importerInstallJournal) error {
	var result error
	backupExists := false
	if _, err := t.parent.childInfo(t.backupName); err == nil {
		backupExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		result = errors.Join(result, err)
	}
	if backupExists {
		result = errors.Join(result, t.parent.removeAll(t.liveName))
		if result == nil {
			result = errors.Join(result, t.parent.renameChild(t.backupName, t.liveName))
		}
	} else if !journal.HadLive && journal.State != installStatePrepared {
		result = errors.Join(result, t.parent.removeAll(t.liveName))
	}

	if journal.State != installStatePrepared {
		backupConfig, _, configErr := t.parent.readFile(t.configBackup)
		if configErr == nil {
			result = errors.Join(
				result,
				t.configRoot.writeFileAtomic(ctx, t.configName, bytes.NewReader(backupConfig), 0o644, nil),
			)
		} else if !errors.Is(configErr, os.ErrNotExist) {
			result = errors.Join(result, configErr)
		}
	}
	if result != nil {
		return result
	}
	return t.finish()
}

func (t *importerInstallTransaction) writeJournal(ctx context.Context, state string) error {
	raw, err := json.Marshal(importerInstallJournal{State: state, HadLive: t.hadLive})
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := t.parent.writeFileAtomic(ctx, t.journalName, bytes.NewReader(raw), 0o600, nil); err != nil {
		return err
	}
	t.state = state
	return nil
}
