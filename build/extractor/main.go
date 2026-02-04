package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"extractor/ipc"

	"golift.io/xtractr"
)

func main() {
	if len(os.Args) != 3 {
		ipc.SendError("InvalidArguments", "Usage: extractor <archive-path> <output-dir>")
		os.Exit(1)
	}

	archivePath := os.Args[1]
	outputDir := os.Args[2]

	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		ipc.SendError("FileNotFound", fmt.Sprintf("Archive file not found: %s", archivePath))
		os.Exit(1)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		ipc.SendError("DirectoryCreationFailed", fmt.Sprintf("Failed to create output directory: %v", err))
		os.Exit(1)
	}

	ipc.SendProgress(0, "Starting extraction...")

	resultPath, err := extractArchive(archivePath, outputDir)
	if err != nil {
		ipc.SendError("ExtractionFailed", err.Error())
		os.Exit(1)
	}

	ipc.SendSuccess(resultPath)
	os.Exit(0)
}

func extractArchive(archivePath, outputDir string) (string, error) {
	absArchive, err := filepath.Abs(archivePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute archive path: %v", err)
	}

	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute output path: %v", err)
	}

	tempDir, err := os.MkdirTemp(absOutput, ".xtractr_temp_*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	xfile := &xtractr.XFile{
		FilePath:  absArchive,
		OutputDir: tempDir,
		FileMode:  0644,
		DirMode:   0755,
	}

	_, _, _, err = xtractr.ExtractFile(xfile)
	if err != nil {
		return "", fmt.Errorf("extraction failed: %v", err)
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return "", fmt.Errorf("failed to read temp directory: %v", err)
	}

	if len(entries) == 0 {
		return absOutput, nil
	}

	var finalPath string
	if len(entries) == 1 {
		entry := entries[0]
		srcPath := filepath.Join(tempDir, entry.Name())
		dstPath := filepath.Join(absOutput, entry.Name())

		if err := os.Rename(srcPath, dstPath); err != nil {
			return "", fmt.Errorf("failed to move single entry: %v", err)
		}

		finalPath = dstPath
	} else {
		filename := filepath.Base(absArchive)
		ext := filepath.Ext(filename)
		stem := strings.TrimSuffix(filename, ext)
		targetDir := filepath.Join(absOutput, stem)

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create wrapper directory: %v", err)
		}

		for _, entry := range entries {
			srcPath := filepath.Join(tempDir, entry.Name())
			dstPath := filepath.Join(targetDir, entry.Name())
			if err := os.Rename(srcPath, dstPath); err != nil {
				return "", fmt.Errorf("failed to move files to wrapper: %v", err)
			}
		}
		finalPath = targetDir
	}

	return finalPath, nil
}
