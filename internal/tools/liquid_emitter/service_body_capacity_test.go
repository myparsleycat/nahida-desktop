package liquidemitter

import (
	"context"
	"testing"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

type oversizedSurfaceViewer struct{ surfaceViewer }

func (v oversizedSurfaceViewer) SurfaceGeometry(
	ctx context.Context,
	session, mesh string,
) (modelviewer.SurfaceGeometry, error) {
	geometry, err := v.surfaceViewer.SurfaceGeometry(ctx, session, mesh)
	geometry.Indices = make([]uint32, 260000*3)
	return geometry, err
}

func TestApplyBodyCapacityKeepsLegacyNozzles(t *testing.T) {
	for _, nozzle := range []string{NozzleSpray, NozzleJet, NozzleOoze, NozzleYogurt} {
		t.Run(nozzle, func(t *testing.T) {
			f := newServiceFixture(t)
			f.service.viewer = oversizedSurfaceViewer{surfaceViewer{f.service.viewer.(fakeViewer)}}
			input := f.applyInput(0)
			look := defaultLook()
			look.Nozzle = nozzle
			input.Look = &look
			result, err := f.service.Apply(context.Background(), input)
			if nozzle == NozzleYogurt {
				if !hasCode(err, ErrorSurfaceUnavailable) {
					t.Fatalf("capacity error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Emitters) != 1 || result.Emitters[0].SurfaceTracked ||
				result.Emitters[0].Look.Nozzle != nozzle {
				t.Fatalf("legacy emitter = %+v", result.Emitters)
			}
		})
	}
}
