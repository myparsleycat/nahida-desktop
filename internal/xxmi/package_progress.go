package xxmi

import (
	"context"

	"nahida.live/desktop/internal/github"
)

func (x *XXMI) downloadPackageFile(ctx context.Context, pkg, version string, request github.FileRequest) error {
	payload := func(stage string) map[string]any {
		data := map[string]any{"package": pkg, "version": version, "stage": stage}
		if run, ok := updateRunOf(ctx); ok {
			data["run"] = run
		}
		return data
	}
	if x.eventEmit != nil {
		request.Progress = func(downloaded, total int64) {
			data := payload("download")
			data["downloaded"], data["total"] = downloaded, total
			x.eventEmit("xxmi:package-progress", data)
		}
	}
	err := x.github.DownloadFile(ctx, request)
	if x.eventEmit != nil {
		stage := "downloaded"
		if err != nil {
			stage = "failed"
		}
		x.eventEmit("xxmi:package-progress", payload(stage))
	}
	return err
}
