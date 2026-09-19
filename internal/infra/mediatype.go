package infra

import (
	"os"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// sniffBytes bounds content-based detection to the leading bytes of a file.
// It matches the read limit mimetype uses by default.
const sniffBytes = 3072

// DetectMediaType reports the MIME type the renderer should be served for
// content, based on its leading bytes rather than its name. It reports false
// when the content does not identify a binary media format.
//
// Text-based detections count as unrecognized: mimetype classifies short files
// as text/plain, and scriptable image/svg+xml is text-based too, so those keep
// the extension mapping the protocol applied before content detection existed.
func DetectMediaType(content []byte) (string, bool) {
	if len(content) == 0 {
		return "", false
	}
	return detectServedType(mimetype.Detect(content))
}

// DetectFileMediaType reports the MIME type of an open file from its leading
// bytes. It leaves the file offset untouched so callers can still serve or read
// the whole file afterwards.
func DetectFileMediaType(file *os.File) (string, bool) {
	if file == nil {
		return "", false
	}
	buffer := make([]byte, sniffBytes)
	read, _ := file.ReadAt(buffer, 0)
	return DetectMediaType(buffer[:read])
}

// DetectFormatExtension reports the extension of the format identified from the
// leading bytes of a file, for callers that classify a file by the container it
// holds rather than by the type they serve. Unknown formats report an empty
// extension; the error only describes a failure to open or read the file.
func DetectFormatExtension(path string) (string, error) {
	detected, err := mimetype.DetectFile(path)
	if detected == nil {
		return "", err
	}
	return strings.Trim(strings.ToLower(detected.Extension()), "."), err
}

func detectServedType(detected *mimetype.MIME) (string, bool) {
	if detected == nil || detected.Is("application/octet-stream") || isTextual(detected) {
		return "", false
	}
	return servedContentType(detected), true
}

func isTextual(detected *mimetype.MIME) bool {
	for current := detected; current != nil; current = current.Parent() {
		if current.Is("text/plain") {
			return true
		}
	}
	return false
}

// servedContentType keeps the MIME strings the protocol served before content
// detection was added. mimetype reports video/matroska where WebView2 and the
// existing mapping for that container expect video/x-matroska.
func servedContentType(detected *mimetype.MIME) string {
	if detected.Is("video/matroska") {
		return "video/x-matroska"
	}
	return detected.String()
}
