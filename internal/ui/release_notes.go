package ui

import (
	"regexp"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"
)

var (
	releaseNoteLink   = regexp.MustCompile(`\[(https?://[^\]\s]+)\]`)
	releaseNoteBullet = regexp.MustCompile(`^(\s*)([*-])\s+(.*)$`)
	// releaseNoteLinkBoundaries end the plain text before a link's label, as in "* Fixed crash by user [url]".
	releaseNoteLinkBoundaries = []string{"* ", "- ", "• ", ": ", "의 ", "by ", "from "}
)

// releaseNoteSegment is a run of release note text; url is set for a link whose label is text.
type releaseNoteSegment struct {
	text, url string
}

// releaseNoteSegments turns the "[https://…]" markers of a line into links labelled with the words before them,
// back to the nearest boundary, as the React update dialog did.
func releaseNoteSegments(line string) []releaseNoteSegment {
	var out []releaseNoteSegment
	cursor := 0
	for _, match := range releaseNoteLink.FindAllStringSubmatchIndex(line, -1) {
		url := line[match[2]:match[3]]
		prefix := line[cursor:match[0]]
		split := -1
		for _, boundary := range releaseNoteLinkBoundaries {
			if i := strings.LastIndex(
				prefix,
				boundary,
			); i >= 0 && i+len(boundary) < len(prefix) &&
				i+len(boundary) > split {
				split = i + len(boundary)
			}
		}
		leading, label := "", prefix
		if split >= 0 {
			leading, label = prefix[:split], prefix[split:]
		}
		label = strings.TrimRight(label, " \t")
		switch {
		case label != "":
			if leading != "" {
				out = append(out, releaseNoteSegment{text: leading})
			}
			out = append(out, releaseNoteSegment{text: label, url: url})
		case prefix != "" && leading == "":
			out = append(out, releaseNoteSegment{text: prefix})
		default:
			if leading != "" {
				out = append(out, releaseNoteSegment{text: leading})
			}
			out = append(out, releaseNoteSegment{text: url, url: url})
		}
		cursor = match[1]
	}
	if cursor < len(line) {
		out = append(out, releaseNoteSegment{text: line[cursor:]})
	}
	return out
}

func (s *Shell) releaseNoteLine(line string) *fairy.El {
	t := kit.T()
	segments := releaseNoteSegments(line)
	if len(segments) == 0 {
		return fairy.Text(" ")
	}
	spans := make([]fairy.Span, 0, len(segments))
	for _, segment := range segments {
		if segment.url == "" {
			spans = append(spans, fairy.Span{Text: segment.text})
			continue
		}
		url := segment.url
		spans = append(spans, fairy.Span{
			Text: segment.text, URL: url, Color: t.Primary, Underline: true, OnClick: func() { s.openExternal(url) },
		})
	}
	return fairy.RichText(spans...).LineHeight(20)
}

// releaseNotesContent lays out release notes line by line, with "*" and "-" lines as indented bullets.
func (s *Shell) releaseNotesContent(text string) *fairy.El {
	content := fairy.Col()
	for line := range strings.SplitSeq(text, "\n") {
		bullet := releaseNoteBullet.FindStringSubmatch(line)
		if bullet == nil {
			content.Child(s.releaseNoteLine(line))
			continue
		}
		indent, marker, rest := bullet[1], bullet[2], bullet[3]
		content.Child(fairy.Row(
			fairy.Text(marker).Shrink(0).LineHeight(20),
			s.releaseNoteLine(rest).Grow(1).MinW(0),
		).Gap(8).Align(fairy.Start).PadEdges(0, 0, 0, float32(len(indent))*7))
	}
	return content
}
