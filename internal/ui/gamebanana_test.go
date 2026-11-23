package ui

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/infra"
)

func TestParseGameBananaModURL(t *testing.T) {
	for _, tc := range []struct {
		input string
		id    int
	}{
		{"https://gamebanana.com/mods/12345", 12345},
		{" http://www.GameBanana.com/mods/7/?tab=files#x ", 7},
		{"https://user:pass@gamebanana.com/mods/1", 0},
		{"https://gamebanana.com.evil.test/mods/1", 0},
		{"https://gamebanana.com/tools/1", 0},
		{"gamebanana.com/mods/1", 0},
		{"javascript:alert(1)", 0},
	} {
		id, ok := parseGameBananaModURL(tc.input)
		if id != tc.id || ok != (tc.id > 0) {
			t.Errorf("parseGameBananaModURL(%q) = %d, %v; want %d", tc.input, id, ok, tc.id)
		}
	}
}

func TestGameBananaDescriptionDropsUnsafeContent(t *testing.T) {
	source := `<h2>Install</h2><p>Copy <b>all</b> files<br>then <a href="javascript:alert(1)">run</a> ` +
		`<a href="https://example.com/x">docs</a></p><script>alert(1)</script><iframe src="https://e.test"></iframe>` +
		`<ul><li>one</li><li>two*</li></ul><img src="https://images.gamebanana.com/a.png" alt="shot">`
	got := gbHTMLToMarkdown(source)
	for _, want := range []string{
		"## Install", "Copy **all** files", "then run [docs](https://example.com/x)", "- one", `- two\*`,
		"![shot](https://images.gamebanana.com/a.png)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	for _, unsafe := range []string{"javascript:", "alert", "e.test"} {
		if strings.Contains(got, unsafe) {
			t.Errorf("kept %q in %q", unsafe, got)
		}
	}
}

func TestGameBananaDecodeAcceptsNumericStrings(t *testing.T) {
	value := map[string]any{"_aMetadata": map[string]any{"_nRecordCount": 3, "_nPerpage": "15", "_bIsComplete": true},
		"_aRecords": []any{map[string]any{"_idRow": "42", "_nStatus": 3, "_sText": "x"}}}
	posts, err := gbDecode[gbPosts](value)
	if err != nil {
		t.Fatal(err)
	}
	if posts.Metadata.PerPage != 15 || posts.Records[0].ID != 42 || posts.Records[0].Status != "3" {
		t.Fatalf("decoded %+v", posts)
	}
}

func TestGameBananaDescriptionKeepsStructure(t *testing.T) {
	source := `<ol><li>first</li><li>second</li></ol><blockquote>quoted</blockquote><hr>` +
		`<table><tr><th>Key</th><th>Action</th></tr><tr><td>F1</td><td>a|b</td></tr></table>` +
		`<pre>line 1
line 2</pre><p><del>old</del> <code>x</code></p><h5>Small</h5>`
	got := gbHTMLToMarkdown(source)
	for _, want := range []string{
		"1. first\n2. second", "> quoted", "---", "| Key | Action |\n| --- | --- |\n| F1 | a\\|b |",
		"```\nline 1\nline 2\n```", "~~old~~ `x`", "##### Small",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func gbTestPNG(t *testing.T, size int) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, size, size))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

// gbImageTestShell serves picture, or 403 while available is false, to a Shell that logs to the returned buffer.
func gbImageTestShell(t *testing.T, picture []byte, available *atomic.Bool) (*Shell, *bytes.Buffer, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !available.Load() {
			http.Error(w, "blocked", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(picture)
	}))
	t.Cleanup(server.Close)
	var output bytes.Buffer
	s := &Shell{deps: Deps{
		HTTP: infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: server.Client()}),
		Log:  infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true}),
	}}
	return s, &output, server.URL
}

func gbWait(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestGameBananaImageRetriesFailureOnRefresh(t *testing.T) {
	t.Parallel()
	var available atomic.Bool
	s, output, origin := gbImageTestShell(t, gbTestPNG(t, 8), &available)
	sources := []string{origin + "/a.png", origin + "/b.png"}
	t.Cleanup(func() {
		for _, src := range sources {
			fairy.Images.Forget(gbImageKey(src))
		}
	})
	load := func(src string) *fairy.Image { return s.gbImage(src, 8, 8, fairy.FitContain) }

	for _, src := range sources {
		gbWait(t, "failure of "+src, func() bool {
			return load(src) == nil && fairy.Images.SizedErr(gbImageKey(src), 8, 8, fairy.FitContain) != nil
		})
		load(src)
	}
	if got := strings.Count(output.String(), "[GameBanana.image]"); got != 1 {
		t.Fatalf("logged %d image failures, want 1 for the host and status:\n%s", got, output.String())
	}
	if !strings.Contains(output.String(), `"status":403`) {
		t.Fatalf("image failure log lacks the HTTP status:\n%s", output.String())
	}

	available.Store(true)
	if load(sources[0]) != nil {
		t.Fatal("failed image loaded before a refresh")
	}
	s.gbRefresh()
	for _, src := range sources {
		gbWait(t, "image after refresh: "+src, func() bool { return load(src) != nil })
	}
	if !strings.Contains(output.String(), `"failureCount":1`) {
		t.Fatalf("refresh did not log the failure that was not logged on its own:\n%s", output.String())
	}
}

func TestGameBananaImageRetriesFailureBehindLoadedSize(t *testing.T) {
	t.Parallel()
	var available atomic.Bool
	available.Store(true)
	s, output, origin := gbImageTestShell(t, gbTestPNG(t, 256), &available)
	src := origin + "/c.png"
	t.Cleanup(func() { fairy.Images.Forget(gbImageKey(src)) })
	load := func(size float32) *fairy.Image { return s.gbImage(src, size, size, fairy.FitContain) }

	gbWait(t, "small image", func() bool { return load(16) != nil })
	available.Store(false)
	gbWait(t, "large image failure", func() bool {
		return load(128) != nil && fairy.Images.SizedErr(gbImageKey(src), 128, 128, fairy.FitContain) != nil
	})
	load(128)
	if got := strings.Count(output.String(), "[GameBanana.image]"); got != 1 {
		t.Fatalf("logged %d image failures behind the small image, want 1:\n%s", got, output.String())
	}

	available.Store(true)
	s.gbRefresh()
	gbWait(t, "large image after refresh", func() bool {
		img := load(128)
		if img == nil {
			return false
		}
		width, _ := img.Size()
		return width >= 128
	})
}

func TestGameBananaImageBodyRejectsNonImageResponses(t *testing.T) {
	t.Parallel()
	picture := gbTestPNG(t, 8)
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		body        []byte
		ok          bool
	}{
		{"forbidden", http.StatusForbidden, "text/html", []byte("<html>blocked</html>"), false},
		{"block page with OK status", http.StatusOK, "image/jpeg", []byte("<!DOCTYPE html><html>blocked</html>"), false},
		{"JSON error with OK status", http.StatusOK, "application/json", []byte(`{"error":"denied"}`), false},
		{"PNG", http.StatusOK, "image/png", picture, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body, err := gbImageBody(&http.Response{
				StatusCode: tc.status,
				Header:     http.Header{"Content-Type": {tc.contentType}},
				Body:       io.NopCloser(bytes.NewReader(tc.body)),
			})
			if (err == nil) != tc.ok {
				t.Fatalf("gbImageBody() error = %v, want success %v", err, tc.ok)
			}
			if tc.ok && !bytes.Equal(body, tc.body) {
				t.Fatalf("gbImageBody() returned %d bytes, want the %d-byte body", len(body), len(tc.body))
			}
		})
	}
}
