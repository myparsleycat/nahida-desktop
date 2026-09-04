package setting

import (
	"encoding/json"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"
)

const (
	defaultStartPage   = "/mod"
	defaultLogLevel    = "warn"
	defaultLanguage    = "en"
	defaultToneMapping = "neutral"
	defaultEnvironment = "studio"
	defaultExposure    = 0.7
	exposureMin        = 0.0
	exposureMax        = 4.0

	transferDownloadConcurrencyDefault = 32
	transferDownloadConcurrencyMin     = 16
	transferDownloadConcurrencyMax     = 64
	transferBandwidthDefault           = 0
	transferBandwidthMin               = 0
	transferBandwidthMax               = 1024
	transferUploadConcurrencyDefault   = 8
	transferUploadConcurrencyMin       = 4
	transferUploadConcurrencyMax       = 16

	modGridWidthMin                   = 240
	modGridWidthMax                   = 640
	modGridColumnMin                  = 1
	modGridColumnMax                  = 8
	modGridResponsiveBaseWidthDefault = 400
	modGridFixedCardWidthDefault      = 360
	modGridFixedColumnCountDefault    = 4
	modCharacterSidebarWidthMin       = 220
	modCharacterSidebarWidthMax       = 480
	modCharacterSidebarWidthDefault   = 256

	drivePasswordListMax = 10

	defaultTouchProfileLlmProtocol  = "openai-response"
	defaultTouchProfileLlmEndpoint  = "https://api.openai.com/v1"
	defaultTouchProfileLlmModel     = "openai/gpt-5.6-luna"
	defaultTouchProfileLlmReasoning = "auto"

	defaultDriveNameSortPolicy = "natural_ignore_spacing"
	defaultArchiveExtractPath  = "flatten_single_root"
	defaultSidebarLayout       = "row"
	defaultModGridLayout       = "responsive"
	defaultDisabledPrefix      = "space"
	defaultAutoUpdateMode      = "auto"
)

var (
	archiveExtractPathModes   = []string{"flatten_single_root", "keep_archive_root", "ask_every_time"}
	modGridLayoutModes        = []string{"responsive", "fixed_card_width", "fixed_column_count"}
	sidebarLayoutModes        = []string{"row", "grid"}
	disabledPrefixStyles      = []string{"space", "underscore"}
	modCompressionMethods     = []string{"zstd", "xpress4k"}
	downloadSources           = []string{"gamebanana", "nahidaLive", "hui", "drive"}
	driveNameSortPolicies     = []string{"natural_ignore_spacing", "natural"}
	touchProfileLlmProtocols  = []string{"openai-response", "openai-compatible", "anthropic"}
	touchProfileLlmReasonings = []string{"auto", "low", "medium", "high"}
	modelViewerToneMappings   = []string{"neutral", "aces", "none"}
	modelViewerEnvironments   = []string{"studio", "soft", "none"}
	logLevels                 = []string{"trace", "debug", "info", "warn", "error", "fatal"}
	defaultDownloadSources    = []string{"gamebanana"}
	sensitiveKeyParts         = []string{"password", "token", "secret", "credentials", "api_key"}
)

func parseBooleanSetting(value *string, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value == "true"
}

func formatBool(value bool) string {
	return strconv.FormatBool(value)
}

func defaultLanguageFromLocale(locale string) string {
	if strings.EqualFold(strings.TrimSpace(locale), "ko") {
		return "ko"
	}
	lang := localeLanguage(locale)
	if lang == "en" || lang == "zh" {
		return lang
	}
	return defaultLanguage
}

func localeLanguage(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return ""
	}
	locale = strings.ReplaceAll(locale, "_", "-")
	locale, _, _ = strings.Cut(locale, ".")
	locale, _, _ = strings.Cut(locale, "@")
	lang, _, _ := strings.Cut(locale, "-")
	return strings.ToLower(lang)
}

func sanitizeDefaultStartPage(page string) string {
	if page == "" {
		return defaultStartPage
	}
	return page
}

func normalizeAutoUpdateMode(value string) string {
	if value == "notify" {
		return "notify"
	}
	if value == "off" || value == "false" {
		return "off"
	}
	return defaultAutoUpdateMode
}

func clampIntegerSetting(value float64, min, max, fallback int) int {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fallback
	}
	truncated := int(math.Trunc(value))
	if truncated < min {
		return min
	}
	if truncated > max {
		return max
	}
	return truncated
}

func clampModelViewerExposure(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return defaultExposure
	}
	rounded := math.Round(value*100) / 100
	if rounded < exposureMin {
		return exposureMin
	}
	if rounded > exposureMax {
		return exposureMax
	}
	return rounded
}

func normalizeEnum(value string, allowed []string, fallback string) string {
	if containsString(allowed, value) {
		return value
	}
	return fallback
}

func normalizeDownloadSources(value any) []string {
	items, ok := asStringSlice(value)
	if !ok {
		return append([]string(nil), defaultDownloadSources...)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if containsString(downloadSources, item) {
			out = append(out, item)
		}
	}
	return out
}

func parseDownloadSources(value *string) []string {
	if value == nil || *value == "" {
		return append([]string(nil), defaultDownloadSources...)
	}
	var parsed any
	if err := json.Unmarshal([]byte(*value), &parsed); err != nil {
		return append([]string(nil), defaultDownloadSources...)
	}
	return normalizeDownloadSources(parsed)
}

func normalizePasswordList(value any) []string {
	items, ok := asStringSlice(value)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
		if len(out) == drivePasswordListMax {
			break
		}
	}
	return out
}

func parsePasswordList(value *string) []string {
	if value == nil || *value == "" {
		return []string{}
	}
	var parsed any
	if err := json.Unmarshal([]byte(*value), &parsed); err != nil {
		return []string{}
	}
	return normalizePasswordList(parsed)
}

func normalizeDriveNameSortPolicy(value string) string {
	return normalizeEnum(value, driveNameSortPolicies, defaultDriveNameSortPolicy)
}

func isTouchProfileLlmProtocol(value string) bool {
	return containsString(touchProfileLlmProtocols, value)
}

func isTouchProfileLlmReasoning(value string) bool {
	return containsString(touchProfileLlmReasonings, value)
}

func normalizeTouchProfileLlmEndpoint(value string) string {
	endpoint := strings.TrimRight(strings.TrimSpace(value), "/")
	if endpoint == "" {
		return defaultTouchProfileLlmEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return defaultTouchProfileLlmEndpoint
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return defaultTouchProfileLlmEndpoint
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return defaultTouchProfileLlmEndpoint
	}
	return strings.TrimRight(parsed.String(), "/")
}

func defaultLLMEndpoint(override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(os.Getenv("NAHIDA_LLM_BASE_URL")); trimmed != "" {
		return trimmed
	}
	return defaultTouchProfileLlmEndpoint
}

func maskSensitiveValue(key string, value *string) *string {
	if !isSensitiveKey(key) {
		return value
	}
	masked := "********"
	return &masked
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

func encodeJSON(value any) string {
	var builder strings.Builder
	encoder := json.NewEncoder(&builder)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "[]"
	}
	return strings.TrimSuffix(builder.String(), "\n")
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// parseJSInt matches Number.parseInt(value, 10): leading space, optional sign,
// then base-10 digits; anything else is ignored. No digits → not ok.
func parseJSInt(value string) (int, bool) {
	value = trimJSSpace(value)
	if value == "" {
		return 0, false
	}
	sign := 1
	switch value[0] {
	case '+':
		value = value[1:]
	case '-':
		sign = -1
		value = value[1:]
	}
	if value == "" || value[0] < '0' || value[0] > '9' {
		return 0, false
	}
	n := 0
	for i := 0; i < len(value) && value[i] >= '0' && value[i] <= '9'; i++ {
		n = n*10 + int(value[i]-'0')
	}
	return sign * n, true
}

// parseJSFloat matches Number.parseFloat for the prefixes we persist.
func parseJSFloat(value string) (float64, bool) {
	value = trimJSSpace(value)
	if value == "" {
		return 0, false
	}
	end := 0
	if value[0] == '+' || value[0] == '-' {
		end = 1
	}
	seenDigit := false
	seenDot := false
	for end < len(value) {
		ch := value[end]
		if ch >= '0' && ch <= '9' {
			seenDigit = true
			end++
			continue
		}
		if ch == '.' && !seenDot {
			seenDot = true
			end++
			continue
		}
		break
	}
	if !seenDigit {
		return 0, false
	}
	n, err := strconv.ParseFloat(value[:end], 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func trimJSSpace(value string) string {
	return strings.TrimLeftFunc(value, unicode.IsSpace)
}

func asString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

func asBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true"
	default:
		return false
	}
}

func asFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case string:
		return parseJSFloat(v)
	default:
		return 0, false
	}
}

func asStringSlice(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return v, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func formatInt(value int) string {
	return strconv.Itoa(value)
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func storedString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return formatBool(v)
	case int:
		return formatInt(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return formatFloat(v)
	case []string:
		return encodeJSON(v)
	default:
		return encodeJSON(v)
	}
}
