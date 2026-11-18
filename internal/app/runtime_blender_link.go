package app

import (
	"context"

	"nahida.live/desktop/internal/setting"
)

func (rt *runtime) startBlenderLink(ctx context.Context) error {
	value, err := rt.setting.Get(ctx, setting.KeyModelViewerBlenderLink)
	if err != nil {
		return err
	}
	enabled, _ := value.(bool)
	rt.configureBlenderLink(enabled)
	return nil
}

// configureBlenderLink starts or stops the pipe server Blender connects to. Start reports its own failures.
func (rt *runtime) configureBlenderLink(enabled bool) {
	if rt.blenderLink == nil {
		return
	}
	if enabled {
		_ = rt.blenderLink.Start(context.Background())
		return
	}
	rt.blenderLink.Shutdown()
}
