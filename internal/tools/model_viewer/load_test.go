package modelviewer

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"nahida.live/desktop/internal/infra"
)

type viewerCancelWriter struct {
	cancel context.CancelFunc
	marker string
}

func TestModelViewerLoadSkipsFullyInvalidMeshes(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		name := "all invalid"
		if mixed {
			name = "with valid mesh"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeViewerGeometry(t, dir)
			position := make([]byte, 8*40)
			copy(position, modelViewerFloat32Bytes([]float32{float32(math.NaN())}))
			ini := "[TextureOverrideBad]\nib = ResourceBodyIB\nvb0 = ResourcePos\nvb1 = ResourceTc\n" +
				"drawindexed = 3, 0, 0\n" +
				"[TextureOverrideAlsoBad]\nib = ResourceBodyIB\nvb0 = ResourcePos\nvb1 = ResourceTc\n" +
				"drawindexed = 3, 0, 0\n"
			if mixed {
				ini += "[TextureOverrideGood]\nib = ResourceBodyIB\nvb0 = ResourcePos\nvb1 = ResourceTc\n" +
					"drawindexed = 3, 3, 0\n"
			}
			ini += viewerBodyResources
			for name, data := range map[string][]byte{
				"mod.ini": []byte(ini), "pos.buf": position,
				"body.ib": modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var logs bytes.Buffer
			protocol := infra.NewProtocol()
			service := NewWithOptions(Options{
				Protocol: protocol, Log: infra.NewLogWithOptions(infra.LogOptions{Writer: &logs, DisableFile: true}),
			})
			result, err := service.LoadModViewer(context.Background(), dir)
			if mixed {
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _, _ = service.CleanupModelViewer(context.Background(), result.MemorySessionID) })
				if len(result.Meshes) != 1 || result.Meshes[0].Component != "Good" {
					t.Fatalf("surviving meshes = %v", result.Meshes)
				}
			}
			if !mixed {
				if err == nil || !strings.Contains(err.Error(), "No mesh data could be extracted") {
					t.Fatalf("empty recovery error = %v", err)
				}
				if len(service.modelViewerSessions) != 0 ||
					reflect.ValueOf(protocol).Elem().FieldByName("sessions").Len() != 0 {
					t.Fatal("failed recovery retained a session")
				}
			}
			if strings.Count(logs.String(), "Skipped invalid model viewer geometry data") != 1 {
				t.Fatalf("shared geometry diagnostics were duplicated: %s", logs.String())
			}
		})
	}
}

func (w viewerCancelWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.marker) {
		w.cancel()
	}
	return len(p), nil
}

func TestModelViewerLoadCancellation(t *testing.T) {
	for _, marker := range []string{"before-load", "INI discovery completed", "Texture preparation completed"} {
		t.Run(marker, func(t *testing.T) {
			dir := t.TempDir()
			writeViewerGeometry(t, dir)
			if err := os.WriteFile(
				filepath.Join(dir, "mod.ini"),
				[]byte(
					"[TextureOverrideBody]\nib = ResourceBodyIB\nvb0 = ResourcePos\nvb1 = ResourceTc\ndrawindexed = 3, 0, 0\n"+viewerBodyResources,
				),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			protocol := infra.NewProtocol()
			log := infra.NewLogWithOptions(
				infra.LogOptions{
					Dev:         true,
					DisableFile: true,
					Writer:      viewerCancelWriter{cancel: cancel, marker: marker},
				},
			)
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

func TestModelViewerTextureJobsCancelDuringActiveHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.png")
	writeModelViewerTestPNG(t, path, color.NRGBA{R: 255, A: 255})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	var once sync.Once
	modelViewerTextureIOHook = func() {
		once.Do(func() {
			close(entered)
			cancel()
		})
	}
	t.Cleanup(func() { modelViewerTextureIOHook = nil })
	output, stats, err := runModelViewerTextureJobs(
		ctx,
		modelViewerTextureSettings{TextureFormat: "png", JPEGQuality: 85},
		1,
		[]modelViewerTextureJob{
			{path: path, resourceName: "BodyDiffuse", keys: []string{"body"}, role: "diffuse", canonicalKey: "body"},
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	select {
	case <-entered:
	default:
		t.Fatal("canceled before hashing started")
	}
	if len(output[0]) != 0 || stats.Decodes != 0 || stats.Encodes != 0 {
		t.Fatalf("canceled texture work ran: %+v", stats)
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
	err := writeModelViewerPayload(
		ctx,
		service,
		sessionID,
		&transport,
		nil,
		map[string]modelViewerTexturePayload{"texture": {Bytes: []byte("data")}},
		modelViewerPayloadOptions{},
	)
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
	fixture := loadViewerFixture(
		t,
		dir,
		"[TextureOverrideBody]\nib = ResourceBodyIB\nvb0 = ResourcePos\nvb1 = ResourceTc\ndrawindexed = 3, 0, 0\n"+viewerBodyResources,
	)
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
	if len(readViewerMesh(t, fixture.protocol, fixture.result.Meshes[0].GeometryURL).Positions) == 0 {
		t.Fatal("existing buffers were released")
	}
}
