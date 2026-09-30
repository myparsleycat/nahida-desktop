package xxmi

import (
	"context"

	"nahida.live/desktop/internal/github"
)

func (x *XXMI) downloadPackageFile(ctx context.Context, pkg, version string, request github.FileRequest) error {
	if x.eventEmit != nil {
		request.Progress = func(downloaded, total int64) {
			x.eventEmit("xxmi:package-progress", map[string]any{
				"package": pkg, "version": version, "stage": "download",
				"downloaded": downloaded, "total": total,
			})
		}
	}
	err := x.github.DownloadFile(ctx, request)
	if x.eventEmit != nil {
		stage := "downloaded"
		if err != nil {
			stage = "failed"
		}
		x.eventEmit("xxmi:package-progress", map[string]any{
			"package": pkg, "version": version, "stage": stage,
		})
	}
	return err
}
