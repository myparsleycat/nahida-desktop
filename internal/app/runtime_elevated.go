package app

import (
	"context"

	"nahida.live/desktop/internal/infra"
)

func (rt *runtime) startElevatedHelperIfEnabled() {
	if rt == nil || rt.setting == nil || rt.elevated == nil {
		return
	}
	enabled, err := rt.setting.GetElevatedHelperEnabled(context.Background())
	if err != nil {
		rt.reportElevatedHelperError(err, "read-setting")
		return
	}
	rt.configureElevatedHelper(enabled)
}

func (rt *runtime) configureElevatedHelper(enabled bool) {
	if rt == nil || rt.elevated == nil {
		return
	}
	stage := "stop"
	var err error
	if enabled {
		stage = "start"
		err = rt.elevated.Start(context.Background())
	} else {
		err = rt.elevated.Close()
	}
	rt.reportElevatedHelperError(err, stage)
}

func (rt *runtime) reportElevatedHelperError(err error, stage string) {
	if err == nil || rt == nil || rt.log == nil {
		return
	}
	_ = infra.ReportError(rt.log, err, "ElevatedHelper", infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "elevated-helper", Stage: stage,
	})
}
