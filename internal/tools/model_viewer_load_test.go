package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

type viewerCancelWriter struct {
	cancel context.CancelFunc
	marker string
}

func (w viewerCancelWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.marker) {
		w.cancel()
	}
	return len(p), nil
}

func TestModelViewerLoadCancellation(t *testing.T) {
	for _, marker := range []string{"before-load", "INI discovery completed", "Texture encoding completed"} {
		t.Run(marker, func(t *testing.T) {
			dir := t.TempDir()
			writeViewerGeometry(t, dir)
			if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte("[TextureOverrideBody]\nib = ResourceBodyIB\nvb0 = ResourcePos\nvb1 = ResourceTc\ndrawindexed = 3, 0, 0\n"+viewerBodyResources), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			protocol := infra.NewProtocol()
			log := infra.NewLogWithOptions(infra.LogOptions{Dev: true, DisableFile: true, Writer: viewerCancelWriter{cancel: cancel, marker: marker}})
			service := NewWithOptions(Options{Protocol: protocol, Log: log})
			if marker == "before-load" {
				cancel()
			}
			result, err := service.LoadModViewer(ctx, dir)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v; expected cancellation", err)
			}
			if !reflect.DeepEqual(result, ModelViewerTransport{}) {
				t.Fatal("canceled load returned a transport")
			}
			if len(service.modelViewerSessions) != 0 {
				t.Fatal("canceled load registered a viewer session")
			}
			// Inspect the protocol's private registry without exposing a production API for tests.
			if count := reflect.ValueOf(protocol).Elem().FieldByName("sessions").Len(); count != 0 {
				t.Fatalf("protocol sessions = %d", count)
			}
		})
	}
}

func TestModelViewerCanceledTextureJobs(t *testing.T) {
	for _, jobs := range [][]modelViewerTextureJob{nil, {{path: filepath.Join(t.TempDir(), "missing.png")}}} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		output, stats, err := runModelViewerTextureJobs(ctx, modelViewerTextureSettings{}, 1, jobs)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
		if len(output[0]) != 0 || stats.Decodes != 0 || stats.Encodes != 0 {
			t.Fatalf("canceled texture work ran: %+v", stats)
		}
	}
}

func TestModelViewerCanceledPayloadDoesNotWrite(t *testing.T) {
	protocol := infra.NewProtocol()
	service := NewWithOptions(Options{Protocol: protocol})
	sessionID := protocol.CreateMemorySession()
	defer protocol.CleanupMemorySession(sessionID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transport := ModelViewerTransport{Textures: make(map[string]ModelViewerTextureTransport)}
	err := writeModelViewerPayload(ctx, service, sessionID, &transport, nil, map[string]modelViewerTexturePayload{"texture": {Bytes: []byte("data")}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if len(transport.Textures) != 0 {
		t.Fatal("canceled payload published textures")
	}
	sessions := reflect.ValueOf(protocol).Elem().FieldByName("sessions")
	if sessions.MapIndex(reflect.ValueOf(sessionID)).Elem().FieldByName("buffers").Len() != 0 {
		t.Fatal("canceled payload retained buffers")
	}
}

func TestModelViewerFailedLoadPreservesExistingSession(t *testing.T) {
	dir := t.TempDir()
	fixture := loadViewerFixture(t, dir, "[TextureOverrideBody]\nib = ResourceBodyIB\nvb0 = ResourcePos\nvb1 = ResourceTc\ndrawindexed = 3, 0, 0\n"+viewerBodyResources)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fixture.service.LoadModViewer(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if _, err := fixture.service.LoadModViewer(context.Background(), t.TempDir()); err == nil {
		t.Fatal("empty directory succeeded")
	}
	if len(fixture.service.modelViewerSessions) != 1 {
		t.Fatal("failed load changed existing session ownership")
	}
	if len(readViewerFloat32s(t, fixture.protocol, fixture.result.Meshes[0].PositionsURL)) == 0 {
		t.Fatal("existing buffers were released")
	}
}
