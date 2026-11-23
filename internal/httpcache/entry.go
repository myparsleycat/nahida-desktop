package httpcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/url"
	"time"
)

// entryMagic starts every entry file; the last byte is the format version.
var entryMagic = []byte("NHC\x01")

const maxMetadataBytes = 64 << 10

// entry is one cached response. An entry file holds entryMagic, the length of the JSON metadata as a
// little-endian uint32, the metadata, then the body.
type entry struct {
	URL          string      `json:"url"`
	Status       int         `json:"status"`
	Header       http.Header `json:"header"`
	RequestTime  time.Time   `json:"requestTime"`
	ResponseTime time.Time   `json:"responseTime"`
	Size         int         `json:"size"`
	CRC32        uint32      `json:"crc32"`
	body         []byte
}

// cacheKey is rawURL without its fragment, which never reaches the server.
func cacheKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.Fragment, u.RawFragment = "", ""
	return u.String()
}

func entryName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func isEntryName(name string) bool {
	if len(name) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil
}

func (e *entry) encode() ([]byte, error) {
	e.Size, e.CRC32 = len(e.body), crc32.ChecksumIEEE(e.body)
	metadata, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.Grow(len(entryMagic) + 4 + len(metadata) + len(e.body))
	out.Write(entryMagic)
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(metadata)))
	out.Write(metadata)
	out.Write(e.body)
	return out.Bytes(), nil
}

var errCorruptEntry = errors.New("corrupt cache entry")

// decodeEntry reads an entry file and checks that it belongs to key and that its body is whole.
func decodeEntry(data []byte, key string) (*entry, error) {
	header := len(entryMagic) + 4
	if len(data) < header || !bytes.Equal(data[:len(entryMagic)], entryMagic) {
		return nil, errCorruptEntry
	}
	length := int(binary.LittleEndian.Uint32(data[len(entryMagic):header]))
	if length > maxMetadataBytes || len(data)-header < length {
		return nil, errCorruptEntry
	}
	var e entry
	if err := json.Unmarshal(data[header:header+length], &e); err != nil {
		return nil, fmt.Errorf("%w: %w", errCorruptEntry, err)
	}
	e.body = data[header+length:]
	if e.URL != key || len(e.body) != e.Size || crc32.ChecksumIEEE(e.body) != e.CRC32 || e.Header == nil {
		return nil, errCorruptEntry
	}
	return &e, nil
}
