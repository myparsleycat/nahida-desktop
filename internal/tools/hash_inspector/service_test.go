package hashinspector

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/gamelink"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi"
)

// A body drawn by the game under a dress drawn later by a mod's command list, in a
// reversed-Z game: the dress is visible and nearest, the body is the layer behind it.
func pickOverBody(modsRoot string) gamelink.PickResult {
	ini := filepath.Join(modsRoot, "Dress", "dress.ini")
	return gamelink.PickResult{
		CaptureID: 7, Trigger: gamelink.TriggerKey,
		Hits: []gamelink.Hit{
			{Record: 1, Primitive: 40, Depth: 0.30},
			{Record: 1, Primitive: 40, Depth: 0.30, Flags: gamelink.HitEqual},
			{Record: 2, Primitive: 5, Depth: 0.31, Flags: gamelink.HitFrontFace},
			{Record: 2, Primitive: 5, Depth: 0.31, Flags: gamelink.HitEqual | gamelink.HitFrontFace},
		},
		Records: []gamelink.Record{
			{
				ID: 1, Origin: gamelink.OriginGame, Call: gamelink.CallDrawIndexed, Start: 300, Topology: 4,
				DepthFunc: 7, IB: &gamelink.Buffer{Hash: 0xaa, HasRegion: true, RegionHash: 0xbb},
			},
			{
				ID:        2,
				Origin:    gamelink.OriginCommandList,
				Call:      gamelink.CallDrawIndexed,
				Start:     900,
				Topology:  4,
				DepthFunc: 7,
				Overrides: []gamelink.Override{
					{Kind: gamelink.OverrideTexture, Section: "TextureOverrideDressIB", INIPath: ini},
				},
				CommandList: &gamelink.CommandOrigin{
					Section: "TextureOverrideDressIB", INIPath: ini, Line: "drawindexed = 600, 900, 0",
				},
			},
		},
	}
}

func TestAssembleCaptureOrdersLayersAndNamesTheVisibleDraw(t *testing.T) {
	modsRoot := filepath.Join(t.TempDir(), "GIMI", "Mods")
	capture := assembleCapture(pickOverBody(modsRoot), modsRoot)

	if len(capture.Layers) != 2 {
		t.Fatalf("layers %+v", capture.Layers)
	}
	dress, body := capture.Layers[0], capture.Layers[1]
	if dress.RecordID != 2 || !dress.Visible || body.Visible {
		t.Fatalf("nearest layer %+v, behind %+v", dress, body)
	}
	if dress.CommandList == nil || dress.CommandList.Mod != "Dress" ||
		dress.CommandList.Line != "drawindexed = 600, 900, 0" {
		t.Fatalf("command list origin %+v", dress.CommandList)
	}
	if dress.IndexOffset == nil || *dress.IndexOffset != 915 || !dress.FrontFace {
		t.Fatalf("dress primitive offset %v front %v", dress.IndexOffset, dress.FrontFace)
	}
	if body.IB == nil || body.IB.MatchHash != "000000bb" || body.IB.Hash != "000000aa" {
		t.Fatalf("body IB %+v", body.IB)
	}
	if got := capture.summary().Visible; got != "TextureOverrideDressIB" {
		t.Fatalf("summary names %q", got)
	}
}

type fakeLink struct {
	states    []gamelink.State
	reply     gamelink.Message
	err       error
	sub       gamelink.Subscriber
	onRequest func()
}

func (f *fakeLink) States() []gamelink.State { return f.states }

func (f *fakeLink) Request(context.Context, string, gamelink.Message) (gamelink.Message, error) {
	if f.onRequest != nil {
		f.onRequest()
	}
	return f.reply, f.err
}

func (f *fakeLink) Subscribe(s gamelink.Subscriber) func() {
	f.sub = s
	return func() {}
}

type fakeImporters struct{ folder string }

func (f fakeImporters) ImporterDLLFeatures(context.Context, string) (xxmi.DLLFeatures, error) {
	return xxmi.DLLFeatures{DesktopChannel: true}, nil
}

func (f fakeImporters) GetImporterConfig(context.Context, string) (xxmi.ImporterConfig, error) {
	return xxmi.ImporterConfig{ImporterFolder: f.folder, Migoto: xxmi.MigotoOptions{InspectKey: "ctrl alt I"}}, nil
}

func TestPickStoresTheResultTheDLLSendsLater(t *testing.T) {
	importerFolder := filepath.Join(t.TempDir(), "GIMI")
	link := &fakeLink{
		states: []gamelink.State{{
			Importer: "GIMI", PID: 30, Status: gamelink.StatusConnected,
			Info: gamelink.HelloReply{
				ImporterFolder: importerFolder,
				RenderAPI:      gamelink.RenderD3D11, Capabilities: gamelink.CapabilityPickD3D11,
			},
		}},
		reply: gamelink.PickArmed{CaptureID: 7},
	}
	data, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	configuredFolder := filepath.Join(t.TempDir(), "Builtin", "GIMI")
	service := NewWithOptions(Options{Link: link, Importers: fakeImporters{folder: configuredFolder}})
	service.UseAppData(data)
	service.Start()

	ctx := t.Context()
	if err := service.Pick(ctx, "gimi"); err != nil {
		t.Fatal(err)
	}
	state, err := service.State(ctx, "GIMI")
	if err != nil || state.Status != StatusConnected || !state.Picking {
		t.Fatalf("state after arming %+v, %v", state, err)
	}
	var contract infra.ContractError
	if err := service.Pick(ctx, "GIMI"); !errors.As(err, &contract) {
		t.Fatalf("second pick while one is armed: %v", err)
	}

	result := pickOverBody(filepath.Join(importerFolder, "Mods"))
	result.Trigger = gamelink.TriggerDesktop
	link.sub.OnMessage("GIMI", 30, result)

	// A new service reads what the first one stored.
	reopened := NewWithOptions(Options{Link: link, Importers: fakeImporters{folder: configuredFolder}})
	reopened.UseAppData(data)
	state, err = reopened.State(ctx, "GIMI")
	if err != nil || state.Picking || len(state.Captures) != 1 ||
		state.Captures[0].Visible != "TextureOverrideDressIB" {
		t.Fatalf("state after the result %+v, %v", state, err)
	}
	capture, err := reopened.Capture(ctx, "GIMI", state.Captures[0].ID)
	if err != nil || capture.Layers[0].CommandList.Mod != "Dress" {
		t.Fatalf("stored capture %+v, %v", capture, err)
	}
	if got := capture.Layers[0].Overrides[0].Mod; got != "Dress" {
		t.Fatalf("matched section mod = %q, want Dress", got)
	}
}

func TestPickIgnoresCaptureIDsFromPreviousRequests(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		previousPID       uint32
		resultBeforeReply bool
	}{
		{name: "previous process", previousPID: 29},
		{name: "reconnected process", previousPID: 30},
		{name: "result before reply", previousPID: 30, resultBeforeReply: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, err := appdata.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			history := NewWithOptions(Options{})
			history.UseAppData(data)
			history.receive("GIMI", tc.previousPID, gamelink.PickFailed{CaptureID: 7, Code: "NO_ELIGIBLE_DRAW"})
			link := &fakeLink{
				states: []gamelink.State{
					{
						Importer: "GIMI",
						PID:      30,
						Status:   gamelink.StatusConnected,
						Info: gamelink.HelloReply{
							RenderAPI:    gamelink.RenderD3D11,
							Capabilities: gamelink.CapabilityPickD3D11,
						},
					},
				},
				reply: gamelink.PickArmed{CaptureID: 7},
			}
			service := NewWithOptions(Options{Link: link, Importers: fakeImporters{folder: t.TempDir()}})
			service.UseAppData(data)
			service.Start()
			t.Cleanup(func() { _ = service.Shutdown() })
			result := gamelink.PickResult{CaptureID: 7, Trigger: gamelink.TriggerDesktop}
			if tc.resultBeforeReply {
				link.onRequest = func() { link.sub.OnMessage("GIMI", 30, result) }
			}

			if err := service.Pick(t.Context(), "GIMI"); err != nil {
				t.Fatal(err)
			}
			state, err := service.State(t.Context(), "GIMI")
			if err != nil || state.Picking == tc.resultBeforeReply {
				t.Fatalf("state after arming = %+v, %v", state, err)
			}
			if !tc.resultBeforeReply {
				var contract infra.ContractError
				if err := service.Pick(t.Context(), "GIMI"); !errors.As(err, &contract) {
					t.Fatalf("second pick while one is armed: %v", err)
				}
				link.sub.OnMessage("GIMI", 30, result)
			}
			state, err = service.State(t.Context(), "GIMI")
			if err != nil || state.Picking || len(state.Captures) != 2 {
				t.Fatalf("state after receiving result = %+v, %v", state, err)
			}
		})
	}
}

func TestPickClearsPendingWhenArmingFails(t *testing.T) {
	t.Parallel()
	link := &fakeLink{
		states: []gamelink.State{{
			Importer: "GIMI", PID: 30, Status: gamelink.StatusConnected,
			Info: gamelink.HelloReply{RenderAPI: gamelink.RenderD3D11, Capabilities: gamelink.CapabilityPickD3D11},
		}},
		err: &gamelink.RemoteError{Code: gamelink.ErrorBusy},
	}
	service := NewWithOptions(Options{Link: link, Importers: fakeImporters{folder: t.TempDir()}})
	var contract infra.ContractError
	if err := service.Pick(t.Context(), "GIMI"); !errors.As(err, &contract) {
		t.Fatalf("busy game error = %v, want a contract error", err)
	}
	state, err := service.State(t.Context(), "GIMI")
	if err != nil || state.Picking {
		t.Fatalf("state after arming failed = %+v, %v", state, err)
	}

	link.err, link.reply = nil, gamelink.PickArmed{CaptureID: 8}
	if err := service.Pick(t.Context(), "GIMI"); err != nil {
		t.Fatalf("retry after arming failed: %v", err)
	}
	state, err = service.State(t.Context(), "GIMI")
	if err != nil || !state.Picking {
		t.Fatalf("state after retry = %+v, %v", state, err)
	}
}

func TestImporterKeysCannotLeaveTheCapturesFolder(t *testing.T) {
	service := NewWithOptions(Options{Now: func() time.Time { return time.Unix(0, 0) }})
	for _, importer := range []string{"", `..\..\data`, "GIMI/../x", "TOOLONGIMPORTERKEY"} {
		if err := service.DeleteCaptures(t.Context(), importer); err == nil {
			t.Fatalf("DeleteCaptures accepted %q", importer)
		}
	}
}
