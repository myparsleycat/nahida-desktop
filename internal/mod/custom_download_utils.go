package mod

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

var (
	archiveExtRE   = regexp.MustCompile(`(?i)\.(tar\.gz|tar\.bz2|tar\.xz|tgz|tbz2|txz)$`)
	filenameStarRE = regexp.MustCompile(`(?i)filename\*\s*=\s*(?:UTF-8''|')?([^;]+)`)
	// RE2 has no backreferences; keep the Electron filename= matcher without \1.
	filenameRE = regexp.MustCompile(`(?i)filename\s*=\s*"?([^";]+)"?`)
)

func parseContentLength(contentLength string) *int64 {
	if contentLength == "" {
		return nil
	}
	size, err := strconv.ParseInt(contentLength, 10, 64)
	if err != nil || size <= 0 {
		return nil
	}
	return &size
}

func parseDownloadFileName(rawURL string, sanitize func(string) string, contentDisposition string) string {
	if match := filenameStarRE.FindStringSubmatch(contentDisposition); len(match) > 1 {
		value := strings.TrimSpace(match[1])
		value = strings.TrimPrefix(strings.TrimSuffix(value, `"`), `"`)
		if decoded, err := url.QueryUnescape(value); err == nil {
			return sanitize(decoded)
		}
	}
	if match := filenameRE.FindStringSubmatch(contentDisposition); len(match) > 1 {
		return sanitize(strings.TrimSpace(match[1]))
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "download"
	}
	rawFileName := filepath.Base(parsed.Path)
	if rawFileName == "" || rawFileName == "." || rawFileName == "/" {
		rawFileName = "download"
	}
	if decoded, err := url.PathUnescape(rawFileName); err == nil {
		rawFileName = decoded
	}
	return sanitize(rawFileName)
}

func createSiblingTempPath(targetPath, suffix string) string {
	return filepath.Join(filepath.Dir(targetPath), filepath.Base(targetPath)+"."+suffix+"-"+uuid.NewString())
}

func getDownloadTempExtension(fileName string) string {
	if match := archiveExtRE.FindString(fileName); match != "" {
		return match
	}
	if ext := filepath.Ext(fileName); ext != "" {
		return ext
	}
	return ".download"
}

func getStagingPaths(fileName string, sanitize func(string) string) (stagingPath, stagedDownloadPath string) {
	stagingRoot := filepath.Join(os.TempDir(), "nahida-desktop-downloads")
	stagingPath = filepath.Join(stagingRoot, sanitize(fileName)+".staging-"+uuid.NewString())
	return stagingPath, filepath.Join(stagingPath, fileName)
}

func getPreviewTargetDir(stagedPath string) string {
	if filepath.Ext(stagedPath) != "" {
		return filepath.Dir(stagedPath)
	}
	return stagedPath
}

func archiveRootName(fileName string, sanitize func(string) string) string {
	sanitized := sanitize(fileName)
	without := regexp.MustCompile(`(?i)\.(zip|7z|rar)$`).ReplaceAllString(sanitized, "")
	if without == "" {
		return sanitized
	}
	return without
}
