package menumaker

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// chromiumValue encodes props the way Chromium's IndexedDB stores an object: value version, Blink's envelope
// with its trailer position, V8's header, then the properties.
func chromiumValue(props ...[2]any) []byte {
	b := []byte{0x02, 0xFF, 0x15, 0xFE}
	b = append(b, make([]byte, 12)...)
	b = append(b, 0xFF, 0x10, 'o')
	str := func(s string) {
		if units := utf16.Encode([]rune(s)); hasWide(s) {
			b = append(b, 0, 'c')
			b = binary.AppendUvarint(b, uint64(len(units)*2))
			for _, u := range units {
				b = binary.LittleEndian.AppendUint16(b, u)
			}
			return
		}
		runes := []rune(s)
		b = append(b, '"')
		b = binary.AppendUvarint(b, uint64(len(runes)))
		for _, r := range runes {
			b = append(b, byte(r))
		}
	}
	for _, p := range props {
		str(p[0].(string))
		if v, ok := p[1].(string); ok {
			str(v)
		} else {
			b = append(b, '0')
		}
	}
	b = append(b, '{')
	return binary.AppendUvarint(b, uint64(len(props)))
}

func hasWide(s string) bool {
	for _, r := range s {
		if r > 0xFF {
			return true
		}
	}
	return false
}

func TestReadLegacyDraftMediaReadsLatestRecords(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	put := func(key string, value []byte) {
		t.Helper()
		if err := db.Put([]byte(key), value, nil); err != nil {
			t.Fatal(err)
		}
	}
	put("a", chromiumValue([2]any{"id", "draft-a"}, [2]any{"originalText", "old"}))
	put("b", chromiumValue([2]any{"id", "draft-b"}, [2]any{"originalText", "gone"}))
	// The compaction moves these into a table file; the writes after it stay in the log.
	if err := db.CompactRange(util.Range{}); err != nil {
		t.Fatal(err)
	}
	put("a", chromiumValue(
		[2]any{"id", "draft-a"}, [2]any{"originalText", "; 한국어 텍스트"},
		[2]any{"panelImageDataUrl", nil}, [2]any{"slotImages", `{"s1":"data:image/png;base64,AA=="}`},
	))
	if err := db.Delete([]byte("b"), nil); err != nil {
		t.Fatal(err)
	}
	put("c", chromiumValue([2]any{"id", "draft-c"}, [2]any{"originalText", "café"}))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	drafts, err := ReadLegacyDraftMedia(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 {
		t.Fatalf("drafts = %v, want draft-a and draft-c", drafts)
	}
	if a := drafts["draft-a"]; a.OriginalText != "; 한국어 텍스트" || a.PanelImageDataURL != "" ||
		a.SlotImages != `{"s1":"data:image/png;base64,AA=="}` {
		t.Errorf("draft-a = %+v, want the newer record", a)
	}
	if c := drafts["draft-c"]; c.OriginalText != "café" {
		t.Errorf("draft-c text = %q, want café from a Latin-1 string", c.OriginalText)
	}
}
