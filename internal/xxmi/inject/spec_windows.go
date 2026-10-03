//go:build windows

package inject

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

type RuntimeMode string

const (
	ModeXXMI   RuntimeMode = "xxmi"
	ModeLegacy RuntimeMode = "legacy"
)

type VerifiedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// ReadyEventPrefix starts the name of every event a caller may pass as LaunchSpec.ReadyEvent.
const ReadyEventPrefix = `Local\nahida-xxmi-launch-`

type LaunchSpec struct {
	Mode            RuntimeMode `json:"mode"`
	ProcessName     string      `json:"processName"`
	StartExe        string      `json:"startExe"`
	StartArgs       []string    `json:"startArgs"`
	WorkDir         string      `json:"workDir"`
	StartMethod     string      `json:"startMethod"`
	Priority        string      `json:"priority"`
	CustomLaunchCmd string      `json:"customLaunchCmd"`
	// ReadyEvent names an event the helper signals once the game may start. The caller then starts
	// the game itself, which keeps a store client it opens from running elevated.
	ReadyEvent      string       `json:"readyEvent"`
	InjectMode      string       `json:"injectMode"`
	InjectionMethod string       `json:"injectionMethod"`
	UseHook         bool         `json:"useHook"`
	LoaderDLL       VerifiedFile `json:"loaderDLL"`
	ModuleDLL       string       `json:"moduleDLL"`
	ExtraDLLs       []string     `json:"extraDLLs"`
	LegacyLoader    VerifiedFile `json:"legacyLoader"`
	TimeoutSeconds  int          `json:"timeoutSeconds"`
}

type LaunchResult struct {
	PID               int      `json:"pid"`
	InjectionVerified bool     `json:"injectionVerified"`
	Warnings          []string `json:"warnings"`
}

func ValidateLaunchSpec(spec LaunchSpec) error {
	if spec.Mode != ModeXXMI && spec.Mode != ModeLegacy {
		return errors.New("invalid runtime mode")
	}
	if spec.ProcessName == "" || filepath.Base(spec.ProcessName) != spec.ProcessName ||
		!strings.EqualFold(filepath.Ext(spec.ProcessName), ".exe") {
		return errors.New("invalid target process name")
	}
	if spec.TimeoutSeconds < 5 || spec.TimeoutSeconds > 600 {
		return errors.New("invalid process timeout")
	}
	if spec.StartMethod != "Native" && spec.StartMethod != "Shell" && spec.StartMethod != "Manual" {
		return errors.New("invalid start method")
	}
	if spec.InjectMode != "Hook" && spec.InjectMode != "Inject" && spec.InjectMode != "Bypass" {
		return errors.New("invalid injection mode")
	}
	if spec.InjectionMethod != "" && spec.InjectionMethod != "Default" && spec.InjectionMethod != "Native" {
		return errors.New("invalid injection method")
	}
	if _, err := priorityClass(spec.Priority); err != nil {
		return err
	}
	if spec.CustomLaunchCmd != "" && spec.StartMethod == "Manual" {
		return errors.New("custom launch cannot use manual start")
	}
	if spec.ReadyEvent != "" {
		// The helper only signals events this app names itself.
		suffix, ok := strings.CutPrefix(spec.ReadyEvent, ReadyEventPrefix)
		if !ok || suffix == "" || strings.Trim(suffix, "0123456789abcdef") != "" {
			return errors.New("invalid ready event")
		}
		if spec.CustomLaunchCmd != "" || spec.StartMethod == "Manual" {
			return errors.New("ready event cannot be combined with another launch")
		}
	}

	// A custom command, a caller-started game, and a manual start launch the game without the start executable.
	if spec.StartMethod != "Manual" && spec.CustomLaunchCmd == "" && spec.ReadyEvent == "" {
		if err := validateRegularLocalFile(spec.StartExe); err != nil {
			return fmt.Errorf("start executable: %w", err)
		}
	}
	if err := validateLocalDirectory(spec.WorkDir); err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	// A bypass launch that leaves the XXMI DLL out names no module.
	if spec.InjectMode != "Bypass" || spec.ModuleDLL != "" {
		if err := validateRegularLocalFile(spec.ModuleDLL); err != nil {
			return fmt.Errorf("module DLL: %w", err)
		}
	}
	if spec.InjectionMethod != "Native" && spec.Mode == ModeXXMI &&
		(spec.InjectMode != "Bypass" || len(spec.ExtraDLLs) > 0) {
		if err := verifyFile(spec.LoaderDLL); err != nil {
			return fmt.Errorf("XXMI loader DLL: %w", err)
		}
	} else if spec.InjectionMethod != "Native" && spec.Mode == ModeLegacy && spec.InjectMode != "Bypass" {
		if err := verifyFile(spec.LegacyLoader); err != nil {
			return fmt.Errorf("legacy loader: %w", err)
		}
	}
	if spec.InjectionMethod != "Native" && spec.Mode == ModeLegacy && len(spec.ExtraDLLs) > 0 {
		if err := verifyFile(spec.LoaderDLL); err != nil {
			return fmt.Errorf("XXMI extra DLL injector: %w", err)
		}
	}
	for _, dll := range spec.ExtraDLLs {
		if err := validateRegularLocalFile(dll); err != nil {
			return fmt.Errorf("extra DLL: %w", err)
		}
	}
	return nil
}

// timeout bounds a single launch wait. Like the reference launcher, process spawn and window appearance
// each get the full timeout, so launchers that start the game through a wrapper do not share one budget.
func (spec LaunchSpec) timeout() time.Duration {
	return time.Duration(spec.TimeoutSeconds) * time.Second
}

func priorityClass(priority string) (uint32, error) {
	switch priority {
	case "Low":
		return windows.IDLE_PRIORITY_CLASS, nil
	case "BelowNormal":
		return windows.BELOW_NORMAL_PRIORITY_CLASS, nil
	case "Normal":
		return windows.NORMAL_PRIORITY_CLASS, nil
	case "AboveNormal":
		return windows.ABOVE_NORMAL_PRIORITY_CLASS, nil
	case "High":
		return windows.HIGH_PRIORITY_CLASS, nil
	case "Realtime":
		return windows.REALTIME_PRIORITY_CLASS, nil
	default:
		return 0, errors.New("invalid process priority")
	}
}

func verifyFile(file VerifiedFile) error {
	if err := validateRegularLocalFile(file.Path); err != nil {
		return err
	}
	want, err := hex.DecodeString(file.SHA256)
	if err != nil || len(want) != sha256.Size {
		return errors.New("invalid SHA-256 hash")
	}
	handle, err := os.Open(file.Path)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, handle); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), file.SHA256) {
		return errors.New("file SHA-256 mismatch")
	}
	return nil
}

// validateRegularLocalFile follows links like the reference launcher: game and importer folders are often
// relocated with junctions, and these paths come from the user's own settings, so a link grants nothing.
func validateRegularLocalFile(path string) error {
	if err := validateLocalPath(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("path is not a regular file")
	}
	return nil
}

func validateLocalDirectory(path string) error {
	if err := validateLocalPath(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("path is not a directory")
	}
	return nil
}

func validateLocalPath(path string) error {
	if !filepath.IsAbs(path) || filepath.VolumeName(path) == "" || strings.HasPrefix(path, `\\`) ||
		strings.ContainsRune(path, 0) {
		return errors.New("path must be an absolute local drive path")
	}
	return nil
}
