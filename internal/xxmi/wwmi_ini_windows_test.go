//go:build windows

package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureWWMIINIFiles(t *testing.T) {
	game := t.TempDir()
	options := WWMIOptions{
		UnlockFPS: true, ApplyPerfTweaks: true,
		PerfTweaks:             map[string]float64{"r.Streaming.HLODStrategy": 2},
		MeshLODDistanceBaseFOV: 165, MeshLODDistanceScale: 1.25, MeshLODDistanceOffset: -10,
		TextureStreamingBoost: 20, TextureStreamingUseAll: true,
	}
	userPath := filepath.Join(game, "Client", "Config", "UserEngine.ini")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		userPath,
		[]byte("[ConsoleVariables]\r\nr.Streaming.Boost=1\r\nr.Streaming.Boost=2\r\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := configureWWMIINIFiles(context.Background(), game, options); err != nil {
		t.Fatal(err)
	}
	userData, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(userData), "r.Streaming.Boost=") != 1 ||
		!strings.Contains(string(userData), "r.Streaming.Boost=20.0") ||
		!strings.Contains(string(userData), "r.Kuro.SkeletalMesh.LODDistanceScale=1.25") {
		t.Fatalf("UserEngine.ini = %q", userData)
	}
	fpsPath := filepath.Join(game, "Client", "Saved", "Config", "WindowsNoEditor", "GameUserSettings.ini")
	fpsData, err := os.ReadFile(fpsPath)
	if err != nil || !strings.Contains(string(fpsData), "FrameRateLimit=120.000000") {
		t.Fatalf("GameUserSettings.ini = %q, error = %v", fpsData, err)
	}
	enginePath := filepath.Join(game, "Client", "Saved", "Config", "WindowsNoEditor", "Engine.ini")
	engineData, err := os.ReadFile(enginePath)
	if err != nil || !strings.Contains(string(engineData), "r.Streaming.HLODStrategy=2.0") {
		t.Fatalf("Engine.ini = %q, error = %v", engineData, err)
	}
	options.ApplyPerfTweaks = false
	if err := configureWWMIINIFiles(context.Background(), game, options); err != nil {
		t.Fatal(err)
	}
	engineData, err = os.ReadFile(enginePath)
	if err != nil || strings.Contains(string(engineData), "r.Streaming.HLODStrategy") {
		t.Fatalf("Engine.ini after disabling tweaks = %q, error = %v", engineData, err)
	}
}
