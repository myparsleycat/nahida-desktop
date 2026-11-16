package iostore

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/zeebo/blake3"
)

func isTestKey(name string) bool {
	return slices.Contains([]string{"NumPadZero", "NumPadOne", "NumPadFive", "X"}, name)
}

// buildPackage writes a zen package whose bytecode holds one FKey literal for NumPadZero and one struct
// literal named X that refers to a non-key struct.
func buildPackage(t *testing.T) []byte {
	t.Helper()
	le := binary.LittleEndian
	names := []string{"/Game/Test/BP", "NumPadZero", "NumPadOne", "X"}

	var b bytes.Buffer
	b.Write(make([]byte, summarySize))
	b.Write(le.AppendUint32(nil, uint32(len(names))))
	stringBytes := 0
	for _, name := range names {
		stringBytes += len(name)
	}
	b.Write(le.AppendUint32(nil, uint32(stringBytes)))
	b.Write(le.AppendUint64(nil, nameBatchHashVersion))
	for _, name := range names {
		b.Write(le.AppendUint64(nil, NameHash(name)))
	}
	for _, name := range names {
		b.Write(binary.BigEndian.AppendUint16(nil, uint16(len(name))))
	}
	for _, name := range names {
		b.WriteString(name)
	}
	pad := (8 - (b.Len()+8)%8) % 8
	b.Write(le.AppendUint64(nil, uint64(pad)))
	b.Write(make([]byte, pad))
	b.Write(le.AppendUint64(nil, 0))

	sections := b.Len()
	b.Write(le.AppendUint64(nil, keyStructImport))
	b.Write(le.AppendUint64(nil, scriptImport("/Script/CoreUObject.Vector")))
	headerSize := b.Len()

	site := func(ref int32, name uint32) {
		b.WriteByte(exStructConst)
		b.Write(le.AppendUint32(nil, uint32(ref)))
		b.Write(le.AppendUint32(nil, 0x18))
		b.WriteByte(exNameConst)
		b.Write(le.AppendUint32(nil, name))
		b.Write(le.AppendUint32(nil, 0))
		b.WriteByte(exEndStructConst)
	}
	b.Write([]byte{0x1C, 1, 2, 3})
	site(-1, 1)
	site(-2, 3)
	b.Write([]byte{0x53, 0x53})

	out := b.Bytes()
	le.PutUint32(out[4:], uint32(headerSize))
	offsets := []int{
		sections,
		sections,
		headerSize,
		headerSize,
		headerSize,
		headerSize,
		headerSize,
		headerSize,
		headerSize,
	}
	for i, offset := range offsets {
		le.PutUint32(out[summaryOffsetsStart+i*4:], uint32(offset))
	}
	return out
}

func TestReplaceInputKey(t *testing.T) {
	original := buildPackage(t)
	p, err := ParsePackage(original)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.InputKeys(isTestKey); !slices.Equal(got, []string{"NumPadZero"}) {
		t.Fatalf("keys = %v, want [NumPadZero]; a struct literal of another type must not count", got)
	}

	t.Run("existing name keeps the header", func(t *testing.T) {
		out, err := p.ReplaceInputKey("NumPadZero", "NumPadOne", isTestKey)
		if err != nil {
			t.Fatal(err)
		}
		q, err := ParsePackage(out)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != len(original) || !slices.Equal(q.InputKeys(isTestKey), []string{"NumPadOne"}) {
			t.Fatalf("keys = %v, size %d -> %d", q.InputKeys(isTestKey), len(original), len(out))
		}
	})

	t.Run("new name moves the sections after the name map", func(t *testing.T) {
		out, err := p.ReplaceInputKey("NumPadZero", "NumPadFive", isTestKey)
		if err != nil {
			t.Fatal(err)
		}
		q, err := ParsePackage(out)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(q.InputKeys(isTestKey), []string{"NumPadFive"}) {
			t.Fatalf("keys = %v", q.InputKeys(isTestKey))
		}
		last := len(q.names) - 1
		if hash := binary.LittleEndian.Uint64(
			out[summarySize+nameBatchFixedSize+last*8:],
		); hash != NameHash(
			"NumPadFive",
		) {
			t.Fatalf("stored hash %x", hash)
		}
		for i := range 9 {
			if q.offset(i)%8 != 0 {
				t.Fatalf("section %d at unaligned offset %d", i, q.offset(i))
			}
		}
		if !slices.Equal(q.imports(), p.imports()) || q.Name() != p.Name() {
			t.Fatal("import map or package name moved incorrectly")
		}
		oldTail, newTail := original[p.headerSize():], out[q.headerSize():]
		if len(oldTail) != len(newTail) || !bytes.Equal(oldTail[:4], newTail[:4]) {
			t.Fatal("export data changed outside the key sites")
		}
	})

	if _, err := p.ReplaceInputKey("NumPadOne", "NumPadFive", isTestKey); err == nil {
		t.Fatal("replacing a key the package does not use succeeded")
	}
	if _, err := p.ReplaceInputKey("NumPadZero", "NumPadZero", isTestKey); !errors.Is(err, ErrKeyInUse) {
		t.Fatalf("replacing with a key in use: err = %v, want ErrKeyInUse", err)
	}
}

func writeContainer(t *testing.T, dir string, blockSize uint64, chunks [][]byte) *Container {
	t.Helper()
	c := &Container{
		TocPath: filepath.Join(dir, "test.utoc"), CasPath: filepath.Join(dir, "test.ucas"),
		header: make([]byte, tocHeaderSize), blockSize: blockSize,
	}
	copy(c.header, tocMagic)
	c.header[16] = tocVersionIoHash
	le := binary.LittleEndian
	le.PutUint32(c.header[20:], tocHeaderSize)
	le.PutUint32(c.header[24:], uint32(len(chunks)))
	le.PutUint32(c.header[32:], blockEntrySize)
	le.PutUint32(c.header[44:], uint32(blockSize))
	le.PutUint32(c.header[52:], 1)
	le.PutUint64(c.header[88:], math.MaxUint64)

	var cas bytes.Buffer
	for i, chunk := range chunks {
		c.ids = append(c.ids, ChunkID{byte(i), 11: ChunkTypeExportBundleData})
		c.ranges = append(c.ranges, chunkRange{offset: uint64(len(c.blocks)) * blockSize, length: uint64(len(chunk))})
		blocks, err := appendBlocks(&sliceWriter{&cas}, int64(cas.Len()), chunk, blockSize)
		if err != nil {
			t.Fatal(err)
		}
		c.blocks = append(c.blocks, blocks...)
		hash := blake3.Sum256(chunk)
		meta := make([]byte, chunkMetaSize)
		copy(meta, hash[:chunkHashSize])
		c.metas = append(c.metas, meta...)
	}
	le.PutUint32(c.header[28:], uint32(len(c.blocks)))
	if err := os.WriteFile(c.TocPath, c.encode(), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.CasPath, cas.Bytes(), 0o666); err != nil {
		t.Fatal(err)
	}
	return c
}

type sliceWriter struct{ b *bytes.Buffer }

func (w *sliceWriter) WriteAt(p []byte, _ int64) (int, error) { return w.b.Write(p) }

func TestReplaceChunkGrowsAcrossBlocks(t *testing.T) {
	dir := t.TempDir()
	chunks := [][]byte{
		bytes.Repeat([]byte{'a'}, 5), bytes.Repeat([]byte{'b'}, 20), bytes.Repeat([]byte{'c'}, 9),
	}
	writeContainer(t, dir, 16, chunks)
	c, err := Open(filepath.Join(dir, "test.utoc"), filepath.Join(dir, "test.ucas"))
	if err != nil {
		t.Fatal(err)
	}
	grown := bytes.Repeat([]byte{'B'}, 40)
	if err := c.ReplaceChunk(1, grown); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(c.TocPath, c.CasPath)
	if err != nil {
		t.Fatal(err)
	}
	cas, err := os.Open(c.CasPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cas.Close() }()
	want := [][]byte{chunks[0], grown, chunks[2]}
	for i := range want {
		got, err := reopened.ReadChunk(cas, i, -1)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want[i]) {
			t.Fatalf("chunk %d = %q, want %q", i, got, want[i])
		}
		hash := blake3.Sum256(got)
		if !bytes.Equal(reopened.metas[i*chunkMetaSize:][:chunkHashSize], hash[:chunkHashSize]) {
			t.Fatalf("chunk %d hash not updated", i)
		}
	}
}
