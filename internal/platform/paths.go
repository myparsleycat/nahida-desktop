package platform

import (
	"os"
	"path/filepath"
)

// InstallDir is the directory that contains the running executable.
// On Windows NSIS installs this is the install folder. It is not the
// process working directory and not Electron userData.
func InstallDir() (string, error) {
	exePath, err := executablePath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exePath), nil
}

func executablePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func installDirFromExe(exePath string) string {
	return filepath.Clean(filepath.Dir(exePath))
}
