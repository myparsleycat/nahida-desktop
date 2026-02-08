package main

/*
#include <stdlib.h>

typedef void (*ProgressCallback)(double, char*);

static void callCallback(ProgressCallback cb, double percent, char* msg) {
	if (cb) {
		cb(percent, msg);
	}
}
*/
import "C"

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"golift.io/xtractr"
)

//export FreeString
func FreeString(str *C.char) {
	C.free(unsafe.Pointer(str))
}

type ExtractorResult struct {
	Success bool   `json:"success"`
	Data    string `json:"data"`
}

//export ExtractArchive
func ExtractArchive(archivePath *C.char, outputDir *C.char, cb C.ProgressCallback) *C.char {
	goArchivePath := C.GoString(archivePath)
	goOutputDir := C.GoString(outputDir)

	invokeCallback := func(percent float64, msg string) {
		cMsg := C.CString(msg)
		defer C.free(unsafe.Pointer(cMsg))
		C.callCallback(cb, C.double(percent), cMsg)
	}

	res := ExtractorResult{Success: false}

	if _, err := os.Stat(goArchivePath); os.IsNotExist(err) {
		res.Data = fmt.Sprintf("Archive file not found: %s", goArchivePath)
	} else if err := os.MkdirAll(goOutputDir, 0755); err != nil {
		res.Data = fmt.Sprintf("Failed to create output directory: %v", err)
	} else {
		invokeCallback(0, "Starting extraction...")
		finalPath, err := extractArchive(goArchivePath, goOutputDir, invokeCallback)
		if err != nil {
			res.Data = err.Error()
		} else {
			res.Success = true
			res.Data = finalPath
		}
	}

	jsonBytes, _ := json.Marshal(res)
	return C.CString(string(jsonBytes))
}

func main() {}

func extractArchive(archivePath, outputDir string, onProgress func(float64, string)) (string, error) {
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

	extracted := false
	if r, err := zip.OpenReader(absArchive); err == nil {
		extracted = true
		defer r.Close()

		totalFiles := len(r.File)
		for i, f := range r.File {
			name := f.Name
			if f.Flags&0x800 == 0 {
				decoder := simplifiedchinese.GBK.NewDecoder()
				if decoded, _, err := transform.String(decoder, f.Name); err == nil {
					name = decoded
				}
			}

			percent := float64(i) / float64(totalFiles) * 100
			onProgress(percent, fmt.Sprintf("Extracting %s", name))

			fpath := filepath.Join(tempDir, name)

			if !strings.HasPrefix(filepath.Clean(fpath), filepath.Clean(tempDir)) {
				continue
			}

			if f.FileInfo().IsDir() {
				os.MkdirAll(fpath, 0755)
				continue
			}

			if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
				return "", fmt.Errorf("failed to create directory: %v", err)
			}

			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("failed to open file in zip: %v", err)
			}

			outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				rc.Close()
				return "", fmt.Errorf("failed to create output file: %v", err)
			}

			_, err = io.Copy(outFile, rc)
			outFile.Close()
			rc.Close()

			if err != nil {
				return "", fmt.Errorf("failed to copy file content: %v", err)
			}
		}
	}

	if !extracted {
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
