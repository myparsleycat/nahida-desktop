package ui

import (
	"strings"
	"testing"
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
