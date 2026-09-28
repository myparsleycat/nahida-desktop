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

type LaunchSpec struct {
	Mode            RuntimeMode  `json:"mode"`
	ProcessName     string       `json:"processName"`
	StartExe        string       `json:"startExe"`
	StartArgs       []string     `json:"startArgs"`
	WorkDir         string       `json:"workDir"`
	StartMethod     string       `json:"startMethod"`
	Priority        string       `json:"priority"`
	CustomLaunchCmd string       `json:"customLaunchCmd"`
	InjectMode      string       `json:"injectMode"`
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
	if _, err := priorityClass(spec.Priority); err != nil {
		return err
	}
	if spec.CustomLaunchCmd != "" && spec.StartMethod == "Manual" {
		return errors.New("custom launch cannot use manual start")
	}
	if spec.StartMethod != "Manual" {
		if err := validateRegularLocalFile(spec.StartExe); err != nil {
			return fmt.Errorf("start executable: %w", err)
		}
	}
	if err := validateLocalDirectory(spec.WorkDir); err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	if err := validateRegularLocalFile(spec.ModuleDLL); err != nil {
		return fmt.Errorf("module DLL: %w", err)
	}
	if spec.Mode == ModeXXMI {
		if err := verifyFile(spec.LoaderDLL); err != nil {
			return fmt.Errorf("XXMI loader DLL: %w", err)
		}
	} else if err := verifyFile(spec.LegacyLoader); err != nil {
		return fmt.Errorf("legacy loader: %w", err)
	}
	for _, dll := range spec.ExtraDLLs {
		if err := validateRegularLocalFile(dll); err != nil {
			return fmt.Errorf("extra DLL: %w", err)
		}
	}
	return nil
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

func validateRegularLocalFile(path string) error {
	if err := validateLocalPath(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a regular file")
	}
	return rejectReparsePoint(path)
}

func validateLocalDirectory(path string) error {
	if err := validateLocalPath(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a directory")
	}
	return rejectReparsePoint(path)
}

func validateLocalPath(path string) error {
	if !filepath.IsAbs(path) || filepath.VolumeName(path) == "" || strings.HasPrefix(path, `\\`) ||
		strings.ContainsRune(path, 0) {
		return errors.New("path must be an absolute local drive path")
	}
	return nil
}

func rejectReparsePoint(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		windowsPath, err := windows.UTF16PtrFromString(current)
		if err != nil {
			return err
		}
		attributes, err := windows.GetFileAttributes(windowsPath)
		if err != nil {
			return err
		}
		if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return errors.New("reparse point is not allowed")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}
