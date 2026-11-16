package iostore

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/go-faster/city"
)

// Zen package header layout as cooked by UE 5.5 and later: a 60-byte summary whose last nine fields are
// absolute section offsets, followed by the name batch, then a padded bulk data map.
const (
	summarySize           = 60
	summaryOffsetsStart   = 24
	nameBatchHashVersion  = 0xC1640000
	nameBatchFixedSize    = 16
	mappedNameIndexMask   = 1<<30 - 1
	maxNameLength         = 1<<15 - 1
	wideNameFlag          = 0x8000
	scriptImportTypeShift = 62
)

type Package struct {
	data    []byte
	names   []string
	nameEnd int
}

func ParsePackage(data []byte) (*Package, error) {
	names, nameEnd, err := parseNames(data)
	if err != nil {
		return nil, err
	}
	headerSize := int(binary.LittleEndian.Uint32(data[4:]))
	if headerSize > len(data) || headerSize < nameEnd {
		return nil, fmt.Errorf("%w: header size %d", ErrUnsupported, headerSize)
	}
	for i := range 9 {
		offset := int(binary.LittleEndian.Uint32(data[summaryOffsetsStart+i*4:]))
		if offset < nameEnd || offset > headerSize {
			return nil, fmt.Errorf("%w: section offset %d", ErrUnsupported, offset)
		}
	}
	p := &Package{data: data, names: names, nameEnd: nameEnd}
	if p.offset(1) > p.offset(2) {
		return nil, fmt.Errorf("%w: import map after export map", ErrUnsupported)
	}
	return p, nil
}

// PackageNames reads the name map from the start of a package chunk, which may be truncated after it.
func PackageNames(head []byte) ([]string, error) {
	names, _, err := parseNames(head)
	return names, err
}

// PackagePrefixSize is how much of a package NameBatchEnd needs.
const PackagePrefixSize = summarySize + nameBatchFixedSize

// NameBatchEnd returns the offset where the name map of a package ends, read from its first PackagePrefixSize
// bytes.
func NameBatchEnd(prefix []byte) (int, error) {
	le := binary.LittleEndian
	if len(prefix) < PackagePrefixSize {
		return 0, fmt.Errorf("%w: package too short", ErrUnsupported)
	}
	if le.Uint32(prefix) != 0 {
		return 0, fmt.Errorf("%w: versioned package", ErrUnsupported)
	}
	count := int(le.Uint32(prefix[summarySize:]))
	stringBytes := int(le.Uint32(prefix[summarySize+4:]))
	if le.Uint64(prefix[summarySize+8:]) != nameBatchHashVersion {
		return 0, fmt.Errorf("%w: name batch hash version", ErrUnsupported)
	}
	if count <= 0 {
		return 0, fmt.Errorf("%w: empty name batch", ErrUnsupported)
	}
	return PackagePrefixSize + count*10 + stringBytes, nil
}

func parseNames(data []byte) ([]string, int, error) {
	le := binary.LittleEndian
	end, err := NameBatchEnd(data)
	if err != nil {
		return nil, 0, err
	}
	if end > len(data) {
		return nil, 0, fmt.Errorf("%w: name batch out of range", ErrUnsupported)
	}
	count := int(le.Uint32(data[summarySize:]))
	headers := PackagePrefixSize + count*8
	position := headers + count*2

	names := make([]string, count)
	for i := range count {
		header := binary.BigEndian.Uint16(data[headers+i*2:])
		length := int(header &^ wideNameFlag)
		if header&wideNameFlag != 0 {
			units := make([]uint16, length)
			for j := range units {
				units[j] = le.Uint16(data[position+j*2:])
			}
			names[i] = string(utf16.Decode(units))
			length *= 2
		} else {
			names[i] = latin1(data[position : position+length])
		}
		position += length
	}
	if position != end {
		return nil, 0, fmt.Errorf("%w: name batch length", ErrUnsupported)
	}
	return names, end, nil
}

func latin1(b []byte) string {
	runes := make([]rune, len(b))
	for i, c := range b {
		runes[i] = rune(c)
	}
	return string(runes)
}

func (p *Package) Name() string {
	index := int(binary.LittleEndian.Uint32(p.data[8:]) & mappedNameIndexMask)
	if index < len(p.names) {
		return p.names[index]
	}
	return ""
}

func (p *Package) headerSize() int { return int(binary.LittleEndian.Uint32(p.data[4:])) }

func (p *Package) offset(field int) int {
	return int(binary.LittleEndian.Uint32(p.data[summaryOffsetsStart+field*4:]))
}

func (p *Package) imports() []uint64 {
	start, end := p.offset(1), p.offset(2)
	imports := make([]uint64, 0, (end-start)/8)
	for at := start; at+8 <= end; at += 8 {
		imports = append(imports, binary.LittleEndian.Uint64(p.data[at:]))
	}
	return imports
}

// withName returns the package with name appended to its name map and every section after the map moved to
// keep its 8-byte alignment.
func (p *Package) withName(name string) (*Package, error) {
	if len(name) == 0 || len(name) > maxNameLength || !isASCII(name) {
		return nil, fmt.Errorf("invalid name %q", name)
	}
	le := binary.LittleEndian
	data := p.data
	count := len(p.names)
	stringBytes := int(le.Uint32(data[summarySize+4:]))
	hashesEnd := summarySize + nameBatchFixedSize + count*8
	headersEnd := hashesEnd + count*2

	if p.nameEnd+8 > len(data) {
		return nil, fmt.Errorf("%w: bulk data map padding", ErrUnsupported)
	}
	oldPad := int(le.Uint64(data[p.nameEnd:]))
	bulkStart := p.nameEnd + 8 + oldPad
	firstSection := p.offset(0)
	if oldPad > 7 || bulkStart%8 != 0 || bulkStart > firstSection {
		return nil, fmt.Errorf("%w: bulk data map padding", ErrUnsupported)
	}

	var batch bytes.Buffer
	batch.Write(data[:summarySize])
	batch.Write(le.AppendUint32(nil, uint32(count+1)))
	batch.Write(le.AppendUint32(nil, uint32(stringBytes+len(name))))
	batch.Write(data[summarySize+8 : hashesEnd])
	batch.Write(le.AppendUint64(nil, NameHash(name)))
	batch.Write(data[hashesEnd:headersEnd])
	batch.Write(binary.BigEndian.AppendUint16(nil, uint16(len(name))))
	batch.Write(data[headersEnd:p.nameEnd])
	batch.WriteString(name)
	nameEnd := batch.Len()
	pad := (8 - (nameEnd+8)%8) % 8
	batch.Write(le.AppendUint64(nil, uint64(pad)))
	batch.Write(make([]byte, pad))
	batch.Write(data[bulkStart:])

	out := batch.Bytes()
	delta := (nameEnd + 8 + pad) - bulkStart
	le.PutUint32(out[4:], uint32(p.headerSize()+delta))
	for i := range 9 {
		le.PutUint32(out[summaryOffsetsStart+i*4:], uint32(p.offset(i)+delta))
	}
	return ParsePackage(out)
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// NameHash is the hash a name batch stores for an ASCII name.
func NameHash(name string) uint64 {
	return city.Hash64([]byte(strings.ToLower(name)))
}

// scriptImport is the FPackageObjectIndex a zen import map holds for a native object such as
// "/Script/InputCore.Key".
func scriptImport(path string) uint64 {
	path = strings.ToLower(strings.NewReplacer(".", "/", ":", "/").Replace(path))
	units := utf16.Encode([]rune(path))
	raw := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(raw[i*2:], unit)
	}
	hash := city.Hash64(raw) &^ (3 << scriptImportTypeShift)
	return hash | 1<<scriptImportTypeShift
}
