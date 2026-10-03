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
	options := WWMIOptions{UnlockFPS: true, MeshLODDistanceBaseFOV: 165}
	userPath := filepath.Join(game, "Client", "Config", "UserEngine.ini")
	enginePath := filepath.Join(game, "Client", "Saved", "Config", "WindowsNoEditor", "Engine.ini")
	for path, content := range map[string]string{
		userPath: "[ConsoleVariables]\r\nr.Kuro.SkeletalMesh.DistanceLODBaseFOV=1\r\n" +
			"r.Kuro.SkeletalMesh.DistanceLODBaseFOV=2\r\nr.Streaming.Boost=20.0\r\n",
		enginePath: "[SystemSettings]\r\nr.Streaming.HLODStrategy=2.0\r\nr.User.Option=1\r\n" +
			"[ConsoleVariables]\r\nr.Kuro.SkeletalMesh.DistanceLODBaseFOV=90\r\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := configureWWMIINIFiles(context.Background(), game, options); err != nil {
		t.Fatal(err)
	}
	userData, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}

	// Values the user set through earlier versions stay; only the option still managed here is rewritten.
	if strings.Count(string(userData), "r.Kuro.SkeletalMesh.DistanceLODBaseFOV=") != 1 ||
		!strings.Contains(string(userData), "r.Kuro.SkeletalMesh.DistanceLODBaseFOV=165") ||
		!strings.Contains(string(userData), "r.Streaming.Boost=20.0") {
		t.Fatalf("UserEngine.ini = %q", userData)
	}
	fpsPath := filepath.Join(game, "Client", "Saved", "Config", "WindowsNoEditor", "GameUserSettings.ini")
	fpsData, err := os.ReadFile(fpsPath)
	if err != nil || !strings.Contains(string(fpsData), "FrameRateLimit=120.000000") {
		t.Fatalf("GameUserSettings.ini = %q, error = %v", fpsData, err)
	}
	engineData, err := os.ReadFile(enginePath)
	if err != nil || strings.Contains(string(engineData), "r.Streaming.HLODStrategy") ||
		strings.Contains(string(engineData), "DistanceLODBaseFOV") ||
		!strings.Contains(string(engineData), "r.User.Option=1") {
		t.Fatalf("Engine.ini = %q, error = %v", engineData, err)
	}
}
