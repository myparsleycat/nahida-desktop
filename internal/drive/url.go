package drive

import (
	"encoding/base64"
	"net/url"
	"regexp"
	"strings"
)

var whitespace = regexp.MustCompile(`\s+`)

// DriveSource is a parsed Nahida shared-link or collection URL.
type DriveSource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

var nahidaSourceHostnames = []string{"nahida.live", "www.nahida.live"}

// Bound nested decoding so malformed input cannot trigger unbounded work.
const maxSourceURLDecodingDepth = 10

// EncodeNahidaPassword matches the web client's URL-safe Base64 password encoding.
func EncodeNahidaPassword(value string) string {
	escaped := strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
	var binary strings.Builder
	for i := 0; i < len(escaped); i++ {
		if escaped[i] == '%' && i+2 < len(escaped) {
			n, bad := parseHexByte(escaped[i+1], escaped[i+2])
			if bad {
				_ = binary.WriteByte(escaped[i])
				continue
			}
			_ = binary.WriteByte(n)
			i += 2
			continue
		}
		_ = binary.WriteByte(escaped[i])
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(binary.String()))
	encoded = strings.ReplaceAll(encoded, "+", "-")
	encoded = strings.ReplaceAll(encoded, "/", "_")
	return strings.TrimRight(encoded, "=")
}

func parseHexByte(a, b byte) (byte, bool) {
	hi, ok := fromHex(a)
	if !ok {
		return 0, true
	}
	lo, ok := fromHex(b)
	if !ok {
		return 0, true
	}
	return hi<<4 | lo, false
}

func fromHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	default:
		return 0, false
	}
}

// ParseDriveSourceUrl is the Go port of Electron parseDriveSourceUrl.
// Non-string values (null, numbers) are rejected the same way.
func ParseDriveSourceUrl(value any) (DriveSource, error) {
	sourceURL := resolveSourceURL(value, 0)
	if sourceURL != "" {
		if source, ok := parseNahidaSourceURL(sourceURL); ok {
			return source, nil
		}
	}
	return DriveSource{}, newDriveAPIError(codeInvalidSourceURL, msgInvalidSourceURL, 0, nil)
}

func resolveSourceURL(value any, depth int) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	normalized := strings.TrimSpace(s)
	if len(normalized) >= 4 && strings.EqualFold(normalized[:4], "http") {
		return normalized
	}
	lowercase := strings.ToLower(normalized)
	for _, hostname := range nahidaSourceHostnames {
		if lowercase == hostname || strings.HasPrefix(lowercase, hostname+"/") {
			return "https://" + normalized
		}
	}
	if depth >= maxSourceURLDecodingDepth {
		return ""
	}
	decoded := decodeBase64(normalized)
	if decoded == "" || decoded == normalized {
		return ""
	}
	return resolveSourceURL(decoded, depth+1)
}

func parseNahidaSourceURL(value string) (DriveSource, bool) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return DriveSource{}, false
	}
	if u.Scheme != "https" {
		return DriveSource{}, false
	}
	host := strings.ToLower(u.Hostname())
	ok := false
	for _, hostname := range nahidaSourceHostnames {
		if hostname == host {
			ok = true
			break
		}
	}
	if !ok {
		return DriveSource{}, false
	}

	path := u.Path
	if m := matchSourcePath(path, "/akasha/link/"); m != "" {
		return DriveSource{Type: "link", ID: m}, true
	}
	if m := matchSourcePath(path, "/akasha/mod/"); m != "" {
		return DriveSource{Type: "mod", ID: m}, true
	}
	return DriveSource{}, false
}

func matchSourcePath(path, prefix string) string {
	lower := strings.ToLower(path)
	if !strings.HasPrefix(lower, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	for _, r := range rest {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return ""
		}
	}
	return rest
}

func decodeBase64(value string) string {
	normalized := whitespace.ReplaceAllString(strings.TrimSpace(value), "")
	normalized = strings.ReplaceAll(normalized, "-", "+")
	normalized = strings.ReplaceAll(normalized, "_", "/")
	if normalized == "" || len(normalized)%4 == 1 || !isStdBase64(normalized) {
		return ""
	}
	padded := normalized
	if rem := len(padded) % 4; rem != 0 {
		padded += strings.Repeat("=", 4-rem)
	}
	raw, err := base64.StdEncoding.DecodeString(padded)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func isStdBase64(s string) bool {
	eq := 0
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '+', c == '/':
			if eq > 0 {
				return false
			}
		case c == '=':
			eq++
			if eq > 2 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
