package drive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
)

const (
	preferredDirectUploadThreshold = 80 * 1024 * 1024
	preferredUploadPartSize        = 25 * 1024 * 1024
	// uploadCompressionAlgorithm is the only algorithm this client encodes;
	// rules naming another one describe payloads this client cannot produce.
	uploadCompressionAlgorithm = "zstd"
	// The zstd levels the encoder accepts; a rules answer outside them is not
	// one this client can read.
	minUploadCompressionLevel = 1
	maxUploadCompressionLevel = 22
)

type UploadExtensionRule struct {
	Ext     string `json:"ext"`
	MaxSize int64  `json:"maxSize"`
}

type UploadPackRules struct {
	PayloadBudget int64 `json:"payloadBudget"`
	MemberMax     int64 `json:"memberMax"`
	MaxFiles      int   `json:"maxFiles"`
}

type UploadPartRules struct {
	MaxBytes int64 `json:"maxBytes"`
	MaxParts int   `json:"maxParts"`
}

// UploadCompressionRules is the algorithm, level and skip rules the server
// publishes; the client compresses every upload with them instead of its own
// hardcoded rules.
type UploadCompressionRules struct {
	Algorithm        string   `json:"algorithm"`
	Level            int      `json:"level"`
	SkipMaxBytes     int64    `json:"skipMaxBytes"`
	SkipMimePrefixes []string `json:"skipMimePrefixes"`
	SkipMimeTypes    []string `json:"skipMimeTypes"`
}

type UploadRules struct {
	MaxFileSize                 int64                  `json:"maxFileSize"`
	MaxPlanFiles                int                    `json:"maxPlanFiles"`
	MaxUploadBodyBytes          int64                  `json:"maxUploadBodyBytes"`
	DirectUploadMaxLogicalBytes int64                  `json:"directUploadMaxLogicalBytes"`
	Extensions                  []UploadExtensionRule  `json:"extensions"`
	Pack                        UploadPackRules        `json:"pack"`
	Parts                       UploadPartRules        `json:"parts"`
	Compression                 UploadCompressionRules `json:"compression"`
}

func (r UploadRules) PartSize() int64 {
	if r.Parts.MaxBytes > 0 && r.Parts.MaxBytes < preferredUploadPartSize {
		return r.Parts.MaxBytes
	}
	return preferredUploadPartSize
}

func (r UploadRules) partSizeForFile(fileSize int64) (int64, bool) {
	if r.Parts.MaxBytes <= 0 || r.Parts.MaxParts <= 0 {
		return 0, false
	}
	partSize := r.PartSize()
	required := requiredUploadPartSize(fileSize, r.Parts.MaxParts)
	if required > partSize {
		partSize = required
	}
	if partSize > r.Parts.MaxBytes {
		return 0, false
	}
	return partSize, true
}

func requiredUploadPartSize(fileSize int64, maxParts int) int64 {
	if fileSize <= 0 || maxParts <= 0 {
		return 0
	}
	n := int64(maxParts)
	size := fileSize / n
	if fileSize%n != 0 {
		size++
	}
	return size
}

func (r UploadRules) MaxSizeFor(name string) (int64, bool) {
	ext := strings.ToLower(filepath.Ext(name))
	for _, item := range r.Extensions {
		if strings.ToLower(item.Ext) == ext {
			return item.MaxSize, true
		}
	}
	return 0, false
}

//wails:ignore
func (d *Drive) UploadRules(ctx context.Context) (UploadRules, error) {
	if d == nil || d.http == nil {
		return UploadRules{}, errDriveHTTPUnconfigured
	}
	d.uploadRulesMu.Lock()
	if d.uploadRules != nil {
		rules := *d.uploadRules
		d.uploadRulesMu.Unlock()
		return rules, nil
	}
	d.uploadRulesMu.Unlock()

	decoded, edenErr, err := d.doJSON(ctx, http.MethodGet, "/akasha/v2/upload-rules", nil, nil)
	if err != nil {
		return UploadRules{}, err
	}
	if edenErr != nil {
		return UploadRules{}, CreateDriveAPIError(edenErr.asAny(), "get:upload-rules", edenErr.Status)
	}
	rules, err := parseUploadRules(decoded)
	if err != nil {
		return UploadRules{}, err
	}
	d.uploadRulesMu.Lock()
	d.uploadRules = &rules
	d.uploadRulesMu.Unlock()
	return rules, nil
}

func (d *Drive) setUploadRules(rules UploadRules) {
	if d == nil {
		return
	}
	cloned := rules
	d.uploadRulesMu.Lock()
	d.uploadRules = &cloned
	d.uploadRulesMu.Unlock()
}

func parseUploadRules(decoded any) (UploadRules, error) {
	raw, err := json.Marshal(decoded)
	if err != nil {
		return UploadRules{}, err
	}
	var rules UploadRules
	if err := json.Unmarshal(raw, &rules); err != nil {
		return UploadRules{}, err
	}
	if rules.DirectUploadMaxLogicalBytes <= 0 {
		rules.DirectUploadMaxLogicalBytes = preferredDirectUploadThreshold
	}
	if rules.MaxFileSize <= 0 || rules.MaxPlanFiles <= 0 || rules.MaxUploadBodyBytes <= 0 ||
		rules.DirectUploadMaxLogicalBytes <= 0 ||
		len(rules.Extensions) == 0 {
		return UploadRules{}, errors.New("upload_rules_unavailable")
	}
	if rules.Pack.PayloadBudget <= 0 || rules.Pack.MemberMax <= 0 || rules.Pack.MaxFiles <= 0 {
		return UploadRules{}, errors.New("upload_rules_unavailable")
	}
	if rules.Parts.MaxBytes <= 0 || rules.Parts.MaxParts <= 0 {
		return UploadRules{}, errors.New("upload_rules_unavailable")
	}
	if rules.Compression.Algorithm != uploadCompressionAlgorithm ||
		rules.Compression.Level < minUploadCompressionLevel ||
		rules.Compression.Level > maxUploadCompressionLevel ||
		rules.Compression.SkipMaxBytes < 0 ||
		!usableUploadMimePatterns(rules.Compression.SkipMimePrefixes) ||
		!usableUploadMimePatterns(rules.Compression.SkipMimeTypes) {
		return UploadRules{}, errors.New("upload_rules_unavailable")
	}
	return rules, nil
}

// usableUploadMimePatterns reports whether the list holds at least one pattern
// that names a type, so a blank entry cannot match every detection and turn
// the skip checks into "skip everything".
func usableUploadMimePatterns(patterns []string) bool {
	return len(patterns) > 0 && !slices.ContainsFunc(patterns, func(pattern string) bool {
		return strings.TrimSpace(pattern) == ""
	})
}

func normalizeUploadExt(extension string) string {
	extension = strings.ToLower(strings.TrimSpace(extension))
	if extension == "" {
		return ""
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	return extension
}

func extensionMaxSizes(rules UploadRules, additional []string) map[string]int64 {
	allowed := make(map[string]int64, len(rules.Extensions)+len(additional))
	for _, item := range rules.Extensions {
		ext := normalizeUploadExt(item.Ext)
		if ext == "" {
			continue
		}
		allowed[ext] = item.MaxSize
	}
	for _, extension := range additional {
		ext := normalizeUploadExt(extension)
		if ext == "" {
			continue
		}
		if _, exists := allowed[ext]; !exists {
			allowed[ext] = rules.MaxFileSize
		}
	}
	return allowed
}

type uploadFileDenial string

const (
	uploadFileDenialNone      uploadFileDenial = ""
	uploadFileDenialExtension uploadFileDenial = "denied_file_type"
	uploadFileDenialSize      uploadFileDenial = "file_too_large"
)

func classifyUploadFile(
	name string,
	size int64,
	allowed map[string]int64,
	allowAll bool,
	maxFileSize int64,
) uploadFileDenial {
	ext := strings.ToLower(filepath.Ext(name))
	maxSize, ok := allowed[ext]
	if !ok {
		if !allowAll {
			return uploadFileDenialExtension
		}
		maxSize = maxFileSize
	}
	if maxSize <= 0 {
		maxSize = maxFileSize
	}
	if maxFileSize > 0 {
		maxSize = min(maxSize, maxFileSize)
	}
	if size > maxSize {
		return uploadFileDenialSize
	}
	return uploadFileDenialNone
}

func uploadFilePermitted(name string, size int64, allowed map[string]int64, allowAll bool, maxFileSize int64) bool {
	return classifyUploadFile(name, size, allowed, allowAll, maxFileSize) == uploadFileDenialNone
}
