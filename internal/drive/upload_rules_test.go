package drive

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func testUploadRules() UploadRules {
	return UploadRules{
		MaxFileSize:        1024 * 1024 * 1024,
		MaxPlanFiles:       500,
		MaxUploadBodyBytes: 100 * 1024 * 1024,
		Extensions: []UploadExtensionRule{
			{Ext: ".ini", MaxSize: 10*1024*1024 - 1},
			{Ext: ".dds", MaxSize: 1024 * 1024 * 1024},
			{Ext: ".png", MaxSize: 100*1024*1024 - 1},
			{Ext: ".bin", MaxSize: 1024 * 1024 * 1024},
			{Ext: ".pak", MaxSize: 1024 * 1024 * 1024},
			{Ext: ".utoc", MaxSize: 1024 * 1024 * 1024},
			{Ext: ".ucas", MaxSize: 1024 * 1024 * 1024},
		},
		Pack: UploadPackRules{
			PayloadBudget: 90 * 1024 * 1024,
			MemberMax:     4 * 1024 * 1024,
			MaxFiles:      100,
		},
		Parts: UploadPartRules{MaxBytes: 32 * 1024 * 1024, MaxParts: 64},
		Compression: UploadCompressionRules{
			Algorithm:        "zstd",
			Level:            6,
			SkipMaxBytes:     100,
			SkipMimePrefixes: []string{"image/", "video/", "audio/"},
			SkipMimeTypes:    []string{"application/zip", "application/gzip"},
		},
	}
}

func TestUploadRulesFetchesAndCaches(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/akasha/v2/upload-rules" || request.Method != http.MethodGet {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(testUploadRules()); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: server.URL,
		HTTPClient: server.Client(),
		Status:     infra.BackendOnline,
	})})
	first, err := drive.UploadRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := drive.UploadRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("fetches = %d, want 1", calls)
	}
	if first.MaxPlanFiles != 500 || second.MaxFileSize != first.MaxFileSize || len(first.Extensions) == 0 {
		t.Fatalf("rules = %#v", first)
	}
	if first.Compression.Algorithm != "zstd" || first.Compression.Level != 6 ||
		first.Compression.SkipMaxBytes != 100 || len(first.Compression.SkipMimeTypes) == 0 {
		t.Fatalf("compression rules = %#v", first.Compression)
	}
}

func TestParseUploadRulesRejectsIncompletePayload(t *testing.T) {
	if _, err := parseUploadRules(map[string]any{"maxFileSize": 1}); err == nil {
		t.Fatal("expected unavailable rules")
	}
}

func TestParseUploadRulesRequiresTheCompressionBlock(t *testing.T) {
	payload := func(mutate func(rules map[string]any)) map[string]any {
		raw, err := json.Marshal(testUploadRules())
		if err != nil {
			t.Fatal(err)
		}
		rules := map[string]any{}
		if err := json.Unmarshal(raw, &rules); err != nil {
			t.Fatal(err)
		}
		mutate(rules)
		return rules
	}
	tests := []struct {
		name   string
		mutate func(rules map[string]any)
	}{
		{name: "missing block", mutate: func(rules map[string]any) { delete(rules, "compression") }},
		{name: "no algorithm", mutate: func(rules map[string]any) {
			rules["compression"].(map[string]any)["algorithm"] = ""
		}},
		{name: "algorithm this client cannot encode", mutate: func(rules map[string]any) {
			rules["compression"].(map[string]any)["algorithm"] = "gzip"
		}},
		{name: "level below the encoder range", mutate: func(rules map[string]any) {
			rules["compression"].(map[string]any)["level"] = 0
		}},
		{name: "level above the encoder range", mutate: func(rules map[string]any) {
			rules["compression"].(map[string]any)["level"] = 23
		}},
		{name: "negative skip size", mutate: func(rules map[string]any) {
			rules["compression"].(map[string]any)["skipMaxBytes"] = -1
		}},
		{name: "empty prefix list", mutate: func(rules map[string]any) {
			rules["compression"].(map[string]any)["skipMimePrefixes"] = []string{}
		}},
		{name: "empty type list", mutate: func(rules map[string]any) {
			rules["compression"].(map[string]any)["skipMimeTypes"] = []string{}
		}},
		{name: "blank prefix entry", mutate: func(rules map[string]any) {
			rules["compression"].(map[string]any)["skipMimePrefixes"] = []string{" "}
		}},
		{name: "blank type entry", mutate: func(rules map[string]any) {
			rules["compression"].(map[string]any)["skipMimeTypes"] = []string{""}
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseUploadRules(payload(tc.mutate)); err == nil {
				t.Fatal("expected unavailable rules")
			}
		})
	}
}

func TestClassifyUploadFileSeparatesExtensionAndSize(t *testing.T) {
	allowed := map[string]int64{".bin": 100}
	if got := classifyUploadFile("tool.exe", 10, allowed, false, 1000); got != uploadFileDenialExtension {
		t.Fatalf("exe denial = %q, want %q", got, uploadFileDenialExtension)
	}
	if got := classifyUploadFile("mod.bin", 150, allowed, false, 1000); got != uploadFileDenialSize {
		t.Fatalf("oversized denial = %q, want %q", got, uploadFileDenialSize)
	}
	if got := classifyUploadFile("mod.bin", 50, allowed, false, 1000); got != uploadFileDenialNone {
		t.Fatalf("permitted denial = %q, want empty", got)
	}
	if got := classifyUploadFile("tool.exe", 10, allowed, true, 1000); got != uploadFileDenialNone {
		t.Fatalf("allow-all denial = %q, want empty", got)
	}
}

func TestUploadFilePermittedCapsExtensionLimitAtMaxFileSize(t *testing.T) {
	allowed := map[string]int64{".bin": 200}
	if uploadFilePermitted("mod.bin", 150, allowed, false, 100) {
		t.Fatal("matching extension should not exceed maxFileSize")
	}
	if !uploadFilePermitted("mod.bin", 100, allowed, false, 100) {
		t.Fatal("size equal to maxFileSize should remain permitted")
	}
	if !uploadFilePermitted("mod.bin", 50, allowed, false, 100) {
		t.Fatal("size under the capped limit should remain permitted")
	}
}

func TestPartSizeForFileRaisesOnlyWhenFixedSizeExceedsMaxParts(t *testing.T) {
	rules := testUploadRules()
	fixed, ok := rules.partSizeForFile(preferredUploadPartSize)
	if !ok || fixed != preferredUploadPartSize {
		t.Fatalf("fixed part size = %d ok=%v, want %d", fixed, ok, preferredUploadPartSize)
	}
	fileSize := preferredUploadPartSize*int64(rules.Parts.MaxParts) + 1
	got, ok := rules.partSizeForFile(fileSize)
	want := requiredUploadPartSize(fileSize, rules.Parts.MaxParts)
	if !ok || got != want || got <= preferredUploadPartSize {
		t.Fatalf("raised part size = %d ok=%v, want %d", got, ok, want)
	}
	tooLarge := rules.Parts.MaxBytes*int64(rules.Parts.MaxParts) + 1
	if _, ok := rules.partSizeForFile(tooLarge); ok {
		t.Fatal("required part size over MaxBytes should be rejected")
	}
	if _, ok := (UploadRules{}).partSizeForFile(1); ok {
		t.Fatal("invalid part limits should be rejected")
	}
}

func TestRequiredUploadPartSizeIsOverflowSafe(t *testing.T) {
	got := requiredUploadPartSize(math.MaxInt64, 2)
	want := int64(math.MaxInt64/2 + 1)
	if got != want {
		t.Fatalf("required part size = %d, want %d", got, want)
	}
}
