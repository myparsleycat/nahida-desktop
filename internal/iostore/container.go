// Package iostore reads Unreal Engine 5 IoStore containers (.utoc/.ucas) and rewrites single package chunks
// in place, for editing the input keys that NTE mods compile into their Blueprints.
package iostore

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/zeebo/blake3"

	"nahida.live/desktop/internal/platform"
)

var ErrUnsupported = errors.New("unsupported IoStore container")

var tocMagic = []byte("-==--==--==--==-")

const (
	tocVersionIoHash = 8
	tocHeaderSize    = 0x90
	blockEntrySize   = 12
	chunkMetaSize    = 24
	chunkHashSize    = 20

	flagEncrypted = 1 << 1
	flagSigned    = 1 << 2
	flagIndexed   = 1 << 3

	ChunkTypeExportBundleData = 1

	maxBlockSize = 1<<24 - 1
)

type ChunkID [12]byte

func (id ChunkID) Type() uint8 { return id[11] }

type chunkRange struct{ offset, length uint64 }

type block struct {
	offset                   uint64
	compressed, uncompressed uint32
	method                   uint8
}

// Container is a parsed .utoc together with the path of its .ucas.
type Container struct {
	TocPath, CasPath string

	header        []byte
	blockSize     uint64
	partitionSize uint64
	ids           []ChunkID
	ranges        []chunkRange
	hashTables    []byte
	blocks        []block
	trailer       []byte // compression method names, directory index
	metas         []byte
}

func Open(tocPath, casPath string) (*Container, error) {
	data, err := os.ReadFile(tocPath)
	if err != nil {
		return nil, err
	}
	c, err := parseToc(data)
	if err != nil {
		return nil, err
	}
	c.TocPath, c.CasPath = tocPath, casPath
	return c, nil
}

func parseToc(data []byte) (*Container, error) {
	if len(data) < tocHeaderSize || !bytes.Equal(data[:16], tocMagic) {
		return nil, fmt.Errorf("%w: not a utoc file", ErrUnsupported)
	}
	le := binary.LittleEndian
	version := data[16]
	headerSize := int(le.Uint32(data[20:]))
	entries := int(le.Uint32(data[24:]))
	blockCount := int(le.Uint32(data[28:]))
	entrySize := le.Uint32(data[32:])
	methodCount := int(le.Uint32(data[36:]))
	methodLength := int(le.Uint32(data[40:]))
	blockSize := uint64(le.Uint32(data[44:]))
	directorySize := int(le.Uint32(data[48:]))
	partitions := le.Uint32(data[52:])
	flags := data[80]
	seeds := int(le.Uint32(data[84:]))
	overflow := int(le.Uint32(data[96:]))
	switch {
	case version != tocVersionIoHash:
		return nil, fmt.Errorf("%w: utoc version %d", ErrUnsupported, version)
	case headerSize != tocHeaderSize || entrySize != blockEntrySize || blockSize == 0 || blockSize > maxBlockSize:
		return nil, fmt.Errorf("%w: unexpected utoc layout", ErrUnsupported)
	case flags&(flagEncrypted|flagSigned) != 0:
		return nil, fmt.Errorf("%w: encrypted or signed container", ErrUnsupported)
	case partitions != 1:
		return nil, fmt.Errorf("%w: %d partitions", ErrUnsupported, partitions)
	}
	if flags&flagIndexed == 0 {
		directorySize = 0
	}

	sizes := []int{
		headerSize, entries * 12, entries * 10, (seeds + overflow) * 4, blockCount * blockEntrySize,
		methodCount*methodLength + directorySize, entries * chunkMetaSize,
	}
	total := 0
	for _, size := range sizes {
		if size < 0 {
			return nil, fmt.Errorf("%w: corrupt utoc", ErrUnsupported)
		}
		total += size
	}
	if total != len(data) {
		return nil, fmt.Errorf("%w: utoc size %d, expected %d", ErrUnsupported, len(data), total)
	}

	c := &Container{blockSize: blockSize, partitionSize: le.Uint64(data[88:])}
	if c.partitionSize == 0 {
		c.partitionSize = math.MaxUint64
	}
	sections := make([][]byte, len(sizes))
	position := 0
	for i, size := range sizes {
		sections[i] = data[position : position+size]
		position += size
	}
	c.header = bytes.Clone(sections[0])
	c.ids = make([]ChunkID, entries)
	c.ranges = make([]chunkRange, entries)
	for i := range entries {
		copy(c.ids[i][:], sections[1][i*12:])
		raw := sections[2][i*10:]
		c.ranges[i] = chunkRange{offset: uint40BE(raw), length: uint40BE(raw[5:])}
	}
	c.hashTables = bytes.Clone(sections[3])
	c.blocks = make([]block, blockCount)
	for i := range blockCount {
		raw := sections[4][i*blockEntrySize:]
		c.blocks[i] = block{
			offset:       uint40LE(raw),
			compressed:   uint32(raw[5]) | uint32(raw[6])<<8 | uint32(raw[7])<<16,
			uncompressed: uint32(raw[8]) | uint32(raw[9])<<8 | uint32(raw[10])<<16,
			method:       raw[11],
		}
	}
	c.trailer = bytes.Clone(sections[5])
	c.metas = bytes.Clone(sections[6])

	for i, r := range c.ranges {
		first, count := c.blockSpan(r)
		if r.offset%blockSize != 0 || first+count > len(c.blocks) {
			return nil, fmt.Errorf("%w: chunk %d outside the block table", ErrUnsupported, i)
		}
	}
	return c, nil
}

func (c *Container) blockSpan(r chunkRange) (first, count int) {
	return int(r.offset / c.blockSize), int((r.length + c.blockSize - 1) / c.blockSize)
}

func (c *Container) ChunkCount() int { return len(c.ids) }

func (c *Container) ChunkID(index int) ChunkID { return c.ids[index] }

func (c *Container) ChunkLength(index int) int { return int(c.ranges[index].length) }

func (c *Container) FindChunk(id ChunkID) int {
	for i, candidate := range c.ids {
		if candidate == id {
			return i
		}
	}
	return -1
}

// ReadChunk reads at most limit bytes from the start of chunk index; a negative limit reads all of it.
func (c *Container) ReadChunk(cas io.ReaderAt, index, limit int) ([]byte, error) {
	r := c.ranges[index]
	length := r.length
	if limit >= 0 && uint64(limit) < length {
		length = uint64(limit)
	}
	first, _ := c.blockSpan(r)
	out := make([]byte, 0, length)
	for i := first; uint64(len(out)) < length; i++ {
		if i >= len(c.blocks) {
			return nil, fmt.Errorf("%w: chunk %d outside the block table", ErrUnsupported, index)
		}
		b := c.blocks[i]
		if b.method != 0 || b.compressed != b.uncompressed {
			return nil, fmt.Errorf("%w: compressed chunk", ErrUnsupported)
		}
		take := min(uint64(b.uncompressed), length-uint64(len(out)))
		part := make([]byte, take)
		if _, err := cas.ReadAt(part, int64(b.offset)); err != nil {
			return nil, fmt.Errorf("read chunk %d block %d: %w", index, i, err)
		}
		out = append(out, part...)
	}
	return out, nil
}

// ReplaceChunk appends data as new blocks at the end of the .ucas and rewrites the .utoc to point chunk index
// at them.
//
// The .ucas is appended to in place instead of being replaced through a temporary file: it is often hundreds of
// megabytes, and the bytes the current .utoc references are never touched, so a reader of the old .utoc and an
// interruption before the .utoc is replaced both see the container as it was. A failure truncates only the
// appended bytes. The replaced blocks stay in the .ucas unreferenced.
func (c *Container) ReplaceChunk(index int, data []byte) error {
	if len(data) == 0 {
		return errors.New("empty chunk")
	}
	cas, err := os.OpenFile(c.CasPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	end, err := cas.Seek(0, io.SeekEnd)
	if err != nil {
		return errors.Join(err, cas.Close())
	}
	if uint64(end)+uint64(len(data)) > c.partitionSize {
		return errors.Join(fmt.Errorf("%w: chunk would exceed the partition size", ErrUnsupported), cas.Close())
	}
	newBlocks, err := appendBlocks(cas, end, data, c.blockSize)
	if err == nil {
		err = cas.Sync()
	}
	if err == nil {
		next := c.withChunk(index, data, newBlocks)
		if err = platform.WriteFileAtomic(c.TocPath, next.encode(), 0o666); err == nil {
			*c = *next
			return cas.Close()
		}
	}
	return errors.Join(err, cas.Truncate(end), cas.Close())
}

func appendBlocks(w io.WriterAt, at int64, data []byte, blockSize uint64) ([]block, error) {
	var blocks []block
	for start := uint64(0); start < uint64(len(data)); start += blockSize {
		part := data[start:min(start+blockSize, uint64(len(data)))]
		if _, err := w.WriteAt(part, at); err != nil {
			return nil, err
		}
		blocks = append(
			blocks,
			block{offset: uint64(at), compressed: uint32(len(part)), uncompressed: uint32(len(part))},
		)
		at += int64(len(part))
	}
	return blocks, nil
}

func (c *Container) withChunk(index int, data []byte, newBlocks []block) *Container {
	next := *c
	old := c.ranges[index]
	first, count := c.blockSpan(old)
	shift := int64(len(newBlocks)-count) * int64(c.blockSize)

	next.blocks = append(append(append([]block(nil), c.blocks[:first]...), newBlocks...), c.blocks[first+count:]...)
	next.ranges = append([]chunkRange(nil), c.ranges...)
	for i, r := range next.ranges {
		if r.offset > old.offset {
			next.ranges[i].offset = uint64(int64(r.offset) + shift)
		}
	}
	next.ranges[index].length = uint64(len(data))

	next.metas = bytes.Clone(c.metas)
	hash := blake3.Sum256(data)
	copy(next.metas[index*chunkMetaSize:], hash[:chunkHashSize])

	next.header = bytes.Clone(c.header)
	binary.LittleEndian.PutUint32(next.header[28:], uint32(len(next.blocks)))
	return &next
}

func (c *Container) encode() []byte {
	var out bytes.Buffer
	out.Write(c.header)
	for _, id := range c.ids {
		out.Write(id[:])
	}
	for _, r := range c.ranges {
		var raw [10]byte
		putUint40BE(raw[:], r.offset)
		putUint40BE(raw[5:], r.length)
		out.Write(raw[:])
	}
	out.Write(c.hashTables)
	for _, b := range c.blocks {
		var raw [blockEntrySize]byte
		putUint40LE(raw[:], b.offset)
		raw[5], raw[6], raw[7] = byte(b.compressed), byte(b.compressed>>8), byte(b.compressed>>16)
		raw[8], raw[9], raw[10] = byte(b.uncompressed), byte(b.uncompressed>>8), byte(b.uncompressed>>16)
		raw[11] = b.method
		out.Write(raw[:])
	}
	out.Write(c.trailer)
	out.Write(c.metas)
	return out.Bytes()
}

func uint40BE(b []byte) uint64 {
	return uint64(b[0])<<32 | uint64(b[1])<<24 | uint64(b[2])<<16 | uint64(b[3])<<8 | uint64(b[4])
}

func putUint40BE(b []byte, v uint64) {
	b[0], b[1], b[2], b[3], b[4] = byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
}

func uint40LE(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 | uint64(b[4])<<32
}

func putUint40LE(b []byte, v uint64) {
	b[0], b[1], b[2], b[3], b[4] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24), byte(v>>32)
}
