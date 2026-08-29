package mod

import (
	"context"
	"net/http"
	"os"
	"regexp"
	"strings"

	"nahida.live/desktop/internal/infra"
)

var archiveNameRE = regexp.MustCompile(`(?i)\.(zip|7z|rar)$`)

var archiveMimes = map[string]struct{}{
	"application/zip":              {},
	"application/x-zip-compressed": {},
	"application/x-7z-compressed":  {},
	"application/vnd.rar":          {},
	"application/x-rar-compressed": {},
}

func isArchiveFileName(fileName string) bool {
	return archiveNameRE.MatchString(fileName)
}

func isHTMLContentType(header http.Header) bool {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(header.Get("Content-Type"), ";")[0]))
	return contentType == "text/html" || contentType == "application/xhtml+xml"
}

func isArchiveByResponseOrContent(ctx context.Context, header http.Header, originalFileName, filePath string, archive *infra.Archive) bool {
	if originalFileName != "" && isArchiveFileName(originalFileName) {
		return true
	}
	if isArchiveFileName(header.Get("Content-Disposition")) {
		return true
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(header.Get("Content-Type"), ";")[0]))
	if _, ok := archiveMimes[contentType]; ok {
		return true
	}
	if archive != nil {
		return archive.IsArchiveOf(ctx, filePath, "zip", "7z", "rar")
	}
	return false
}

func isHTMLResponseOrContent(header http.Header, filePath string) (bool, error) {
	if isHTMLContentType(header) {
		return true, nil
	}
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	buf := make([]byte, 4096)
	n, _ := file.Read(buf)
	snippet := strings.TrimLeft(string(buf[:n]), " \t\r\n")
	return regexp.MustCompile(`(?i)^(<!doctype\s+html\b|<html\b)`).MatchString(snippet), nil
}
