package ui

import (
	"slices"
	"testing"
)

func TestReleaseNoteSegments(t *testing.T) {
	tests := []struct {
		line string
		want []releaseNoteSegment
	}{
		{"plain text", []releaseNoteSegment{{text: "plain text"}}},
		{
			"Fixed crash by alice [https://x.test/1]",
			[]releaseNoteSegment{{text: "Fixed crash by "}, {text: "alice", url: "https://x.test/1"}},
		},
		{"Notes: [https://x.test/2] end", []releaseNoteSegment{
			{text: "Notes:", url: "https://x.test/2"}, {text: " end"},
		}},
		{"[https://x.test/3]", []releaseNoteSegment{{text: "https://x.test/3", url: "https://x.test/3"}}},
	}
	for _, tt := range tests {
		if got := releaseNoteSegments(tt.line); !slices.Equal(got, tt.want) {
			t.Errorf("releaseNoteSegments(%q) = %+v, want %+v", tt.line, got, tt.want)
		}
	}
}
