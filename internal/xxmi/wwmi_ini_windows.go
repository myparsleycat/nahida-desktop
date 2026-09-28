//go:build windows

package xxmi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func configureWWMIINIFiles(ctx context.Context, game string, options WWMIOptions) error {
	if err := editWWMIINI(ctx, filepath.Join(game, "Client", "Saved", "Config", "WindowsNoEditor", "Engine.ini"),
		func(doc *iniDocument) {
			doc.RemoveOption("ConsoleVariables", "r.Kuro.SkeletalMesh.DistanceLODBaseFOV")
			for key, value := range options.PerfTweaks {
				if options.ApplyPerfTweaks {
					doc.SetOption("SystemSettings", key, wwmiFloat(value), false)
				} else {
					doc.RemoveOption("SystemSettings", key)
				}
			}
		}); err != nil {
		return fmt.Errorf("edit WWMI Engine.ini: %w", err)
	}
	if err := editWWMIINI(ctx, filepath.Join(game, "Client", "Config", "UserEngine.ini"),
		func(doc *iniDocument) {
			values := map[string]string{
				"r.Kuro.SkeletalMesh.DistanceLODBaseFOV":           strconv.Itoa(options.MeshLODDistanceBaseFOV),
				"r.Kuro.SkeletalMesh.LODDistanceScaleDeviceOffset": wwmiFloat(options.MeshLODDistanceOffset),
				"r.Streaming.Boost":                                wwmiFloat(options.TextureStreamingBoost),
				"r.Streaming.MinBoost":                             wwmiFloat(options.TextureStreamingMinBoost),
				"r.Streaming.UseAllMips":                           wwmiBool(options.TextureStreamingUseAll),
				"r.Streaming.PoolSize":                             strconv.Itoa(options.TextureStreamingPoolSize),
				"r.Streaming.LimitPoolSizeToVRAM":                  wwmiBool(options.TextureStreamingLimitVRAM),
				"r.Streaming.UseFixedPoolSize":                     wwmiBool(options.TextureStreamingFixedPool),
			}
			for key, value := range values {
				doc.SetOptionUnique("ConsoleVariables", key, value, false)
			}
		}); err != nil {
		return fmt.Errorf("edit WWMI UserEngine.ini: %w", err)
	}
	if options.UnlockFPS {
		if err := editWWMIINI(ctx,
			filepath.Join(game, "Client", "Saved", "Config", "WindowsNoEditor", "GameUserSettings.ini"),
			func(doc *iniDocument) {
				doc.SetOptionUnique("/Script/Engine.GameUserSettings", "FrameRateLimit", "120.000000", false)
			}); err != nil {
			return fmt.Errorf("edit WWMI GameUserSettings.ini: %w", err)
		}
	}
	return nil
}

func editWWMIINI(ctx context.Context, path string, edit func(*iniDocument)) error {
	root, err := ensureInstallRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	data, info, err := root.readFile(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(data) > 8<<20 {
		return errors.New("WWMI INI file exceeds size limit")
	}
	doc := parseINI(data)
	edit(doc)
	if !doc.Changed() {
		return nil
	}
	return root.writeFileAtomic(ctx, name, bytes.NewReader(doc.Bytes()), 0o600, info)
}

func wwmiFloat(value float64) string {
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if !strings.Contains(text, ".") {
		text += ".0"
	}
	return text
}

func wwmiBool(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
