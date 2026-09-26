package app

import (
	"context"
	"errors"

	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/infra"
)

// dispatchDeepLinkDownload starts a nahida://download request found in args.
// The download waits on startup and on the path selector, so it runs off the
// launch goroutine.
func (rt *runtime) dispatchDeepLinkDownload(args []string) {
	download := nahidaDeepLinkDownload(args)
	if download == nil {
		return
	}
	ctx := context.Background()
	if rt.startup != nil {
		// Shutdown cancels the startup context, releasing a pending path selector.
		ctx = rt.startup.ctx
	}
	go rt.runDeepLinkDownload(ctx, *download)
}

func (rt *runtime) runDeepLinkDownload(ctx context.Context, download deepLinkDownload) {
	status, err := rt.startDeepLinkDownload(ctx, download)
	switch {
	case err != nil && ctx.Err() != nil:
		return
	case err != nil:
		_ = infra.ReportError(rt.log, err, "DeepLink:download", infra.Diagnostic{
			Severity:  infra.DiagnosticError,
			Operation: "deep-link-download",
			Fields:    map[string]any{"kind": download.Kind, "id": download.ID, "name": download.Name},
		})
		emitAppEvent("fn:toast", "다운로드를 시작하지 못했습니다", map[string]any{"description": download.Name})
	case status == "unauthorized":
		emitAppEvent("fn:toast", "로그인이 필요합니다", map[string]any{"description": download.Name})
		emitAppEvent("fn:navi", "/auth")
	case status == "canceled":
		emitAppEvent("fn:toast", "다운로드가 취소되었습니다", map[string]any{"description": download.Name})
	}
}

func (rt *runtime) startDeepLinkDownload(ctx context.Context, download deepLinkDownload) (string, error) {
	if err := rt.startup.wait(ctx); err != nil {
		return "", err
	}
	// Shared links and mods carry their own credentials; only the user's own
	// drive needs a session.
	if download.Link == nil && download.Mod == nil {
		if rt.auth == nil {
			return "", errors.New("auth service is not configured")
		}
		loggedIn, err := rt.auth.IsLoggedIn(ctx)
		if err != nil {
			return "", err
		}
		if !loggedIn {
			return "unauthorized", nil
		}
	}
	if rt.drive == nil {
		return "", errors.New("drive service is not configured")
	}

	name := download.Name
	if name == "" {
		name = "item"
	}
	result, err := rt.drive.StartDownload(ctx, drive.StartDownloadParams{
		Items: []drive.DownloadItem{{ID: download.ID, IsDir: download.IsDir, Name: name}},
		Link:  download.Link,
		Mod:   download.Mod,
	})
	if err != nil {
		return "", err
	}
	return result.Status, nil
}
