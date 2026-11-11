package menumaker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb/journal"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/syndtr/goleveldb/leveldb/table"
)

// LegacyDraftMedia is what the React menu maker kept in IndexedDB for a draft: the source text and the images.
type LegacyDraftMedia struct {
	OriginalText      string
	PanelImageDataURL string
	SlotImages        string
}

// LegacyIndexedDBDir is the IndexedDB of the WebView2 profile that the Wails build used, where the React menu
// maker stored its draft media.
func LegacyIndexedDBDir() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "nahida-desktop.exe", "EBWebView", "Default", "IndexedDB",
		"http_wails.localhost_0.indexeddb.leveldb"), nil
}

// ReadLegacyDraftMedia reads the draft media records of the React menu maker from a Chromium IndexedDB LevelDB
// directory, by draft ID. It reads the table and log files itself instead of opening the database, because
// Chromium orders keys with its own comparator and a reader must not write to the directory. A missing
// directory gives no drafts.
func ReadLegacyDraftMedia(dir string) (map[string]LegacyDraftMedia, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	latest := map[string]leveldbEntry{}
	keep := func(key []byte, seq uint64, value []byte, deleted bool) {
		if old, ok := latest[string(key)]; ok && old.seq > seq {
			return
		}
		latest[string(key)] = leveldbEntry{seq: seq, value: value, deleted: deleted}
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".log":
			err = readLevelDBLog(path, keep)
		case ".ldb", ".sst":
			err = readLevelDBTable(path, keep)
		}
		if err != nil {
			return nil, err
		}
	}

	drafts := map[string]LegacyDraftMedia{}
	for _, entry := range latest {
		if entry.deleted {
			continue
		}
		if fields, ok := decodeIndexedDBObject(entry.value); ok && fields["id"] != "" && fields["originalText"] != "" {
			drafts[fields["id"]] = LegacyDraftMedia{
				OriginalText:      fields["originalText"],
				PanelImageDataURL: fields["panelImageDataUrl"],
				SlotImages:        fields["slotImages"],
			}
		}
	}
	return drafts, nil
}

type leveldbEntry struct {
	seq     uint64
	value   []byte
	deleted bool
}

type leveldbVisit func(key []byte, seq uint64, value []byte, deleted bool)

// readLevelDBLog replays the write batches of a LevelDB log file.
func readLevelDBLog(path string, visit leveldbVisit) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	r := journal.NewReader(f, nil, false, true)
	for {
		chunk, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		batch, err := io.ReadAll(chunk)
		if err != nil {
			// A torn record at the end of the log is what an interrupted write leaves.
			return nil
		}
		decodeLevelDBBatch(batch, visit)
	}
}

// decodeLevelDBBatch reads a write batch: an 8-byte sequence, a 4-byte count, then the records.
func decodeLevelDBBatch(batch []byte, visit leveldbVisit) {
	if len(batch) < 12 {
		return
	}
	seq := binary.LittleEndian.Uint64(batch)
	count := binary.LittleEndian.Uint32(batch[8:])
	rest := batch[12:]
	for i := range uint64(count) {
		if len(rest) == 0 {
			return
		}
		kind := rest[0]
		rest = rest[1:]
		key, n := leveldbSlice(rest)
		if n == 0 {
			return
		}
		rest = rest[n:]
		if kind == 0 {
			visit(key, seq+i, nil, true)
			continue
		}
		value, n := leveldbSlice(rest)
		if n == 0 {
			return
		}
		rest = rest[n:]
		visit(key, seq+i, value, false)
	}
}

func leveldbSlice(b []byte) ([]byte, int) {
	size, n := binary.Uvarint(b)
	if n <= 0 || uint64(len(b)-n) < size {
		return nil, 0
	}
	return b[n : n+int(size)], n + int(size)
}

// readLevelDBTable visits every entry of a table file. Its keys end in 8 bytes of sequence and kind.
func readLevelDBTable(path string, visit leveldbVisit) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}

	r, err := table.NewReader(f, info.Size(), storage.FileDesc{Type: storage.TypeTable}, nil, nil, nil)
	if err != nil {
		return err
	}
	defer r.Release()
	it := r.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() {
		key := it.Key()
		if len(key) < 8 {
			continue
		}
		trailer := binary.LittleEndian.Uint64(key[len(key)-8:])
		visit(bytes.Clone(key[:len(key)-8]), trailer>>8, bytes.Clone(it.Value()), trailer&0xff == 0)
	}
	return it.Error()
}

// decodeIndexedDBObject reads the string and null properties of a flat object stored by Chromium's IndexedDB:
// a value version, Blink's envelope (0xFF version, and 0xFE with a trailer position from version 21), then a
// V8 serialized object.
func decodeIndexedDBObject(value []byte) (map[string]string, bool) {
	d := v8Decoder{b: value}
	if _, ok := d.varint(); !ok {
		return nil, false
	}
	for d.peek() == 0xFF {
		d.pos++
		if _, ok := d.varint(); !ok {
			return nil, false
		}
		if d.peek() == 0xFE {
			d.pos += 13
		}
	}
	if d.tag() != 'o' {
		return nil, false
	}

	fields := map[string]string{}
	for {
		tag := d.tag()
		if tag == '{' {
			return fields, true
		}
		key, ok := d.stringOf(tag)
		if !ok {
			return nil, false
		}
		switch tag := d.tag(); tag {
		case '0', '_':
		default:
			text, ok := d.stringOf(tag)
			if !ok {
				return nil, false
			}
			fields[key] = text
		}
	}
}

type v8Decoder struct {
	b   []byte
	pos int
}

func (d *v8Decoder) peek() byte {
	if d.pos >= len(d.b) {
		return 0
	}
	return d.b[d.pos]
}

// tag reads the next tag, passing over the padding V8 writes to align two-byte strings.
func (d *v8Decoder) tag() byte {
	for d.pos < len(d.b) && d.b[d.pos] == 0 {
		d.pos++
	}
	if d.pos >= len(d.b) {
		return 0
	}
	d.pos++
	return d.b[d.pos-1]
}

func (d *v8Decoder) varint() (uint64, bool) {
	v, n := binary.Uvarint(d.b[min(d.pos, len(d.b)):])
	if n <= 0 {
		return 0, false
	}
	d.pos += n
	return v, true
}

func (d *v8Decoder) bytes() ([]byte, bool) {
	size, ok := d.varint()
	if !ok || size > math.MaxInt32 || uint64(len(d.b)-d.pos) < size {
		return nil, false
	}
	b := d.b[d.pos : d.pos+int(size)]
	d.pos += int(size)
	return b, true
}

// stringOf reads the string a tag starts: '"' Latin-1, 'c' UTF-16LE or 'S' UTF-8.
func (d *v8Decoder) stringOf(tag byte) (string, bool) {
	b, ok := d.bytes()
	if !ok {
		return "", false
	}
	switch tag {
	case '"':
		runes := make([]rune, len(b))
		for i, c := range b {
			runes[i] = rune(c)
		}
		return string(runes), true
	case 'c':
		if len(b)%2 != 0 {
			return "", false
		}
		units := make([]uint16, len(b)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(b[i*2:])
		}
		return string(utf16.Decode(units)), true
	case 'S':
		return string(b), true
	}
	return "", false
}
