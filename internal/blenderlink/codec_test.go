package blenderlink

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var updateVectors = flag.Bool("update", false, "rewrite testdata/v1/*.bin from the JSON descriptions")

// The add-on's protocol test encodes and decodes the same vectors, so a change to the encoding on
// either side shows up as a changed .bin file.
func TestProtocolVectors(t *testing.T) {
	descriptions, err := filepath.Glob(filepath.Join("testdata", "v1", "*.json"))
	if err != nil || len(descriptions) == 0 {
		t.Fatalf("no vectors: %v", err)
	}
	for _, description := range descriptions {
		name := strings.TrimSuffix(filepath.Base(description), ".json")
		t.Run(name, func(t *testing.T) {
			requestID, want := readVector(t, description)
			frame, err := Encode(requestID, want)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := AppendFrame(nil, frame)
			if err != nil {
				t.Fatal(err)
			}

			binPath := strings.TrimSuffix(description, ".json") + ".bin"
			if *updateVectors {
				if err := os.WriteFile(binPath, encoded, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			golden, err := os.ReadFile(binPath)
			if err != nil {
				t.Fatalf("read %s (run with -update to create it): %v", binPath, err)
			}
			if !bytes.Equal(encoded, golden) {
				t.Fatalf("encoding differs from %s", binPath)
			}

			read, err := ReadFrame(bytes.NewReader(golden))
			if err != nil {
				t.Fatal(err)
			}
			got, err := Decode(read)
			if err != nil {
				t.Fatal(err)
			}
			if read.RequestID != requestID || !reflect.DeepEqual(got, want) {
				t.Fatalf("decoded %d %#v, want %d %#v", read.RequestID, got, requestID, want)
			}
		})
	}
}

func readVector(t *testing.T, path string) (uint32, Message) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Type      string          `json:"type"`
		RequestID uint32          `json:"request_id"`
		Message   json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(data, &vector); err != nil {
		t.Fatal(err)
	}

	targets := map[string]Message{
		"Hello": &Hello{}, "HelloAck": &HelloAck{}, "Scene": &Scene{}, "MeshBegin": &MeshBegin{},
		"MeshChunk": &MeshChunk{}, "MeshCommit": &MeshCommit{}, "Ack": &Ack{}, "Ping": &Ping{}, "Pong": &Pong{},
		"ClientError": &ClientError{},
	}
	target, ok := targets[vector.Type]
	if !ok {
		t.Fatalf("unknown vector type %q", vector.Type)
	}
	decoder := json.NewDecoder(bytes.NewReader(vector.Message))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatal(err)
	}
	m, ok := reflect.ValueOf(target).Elem().Interface().(Message)
	if !ok {
		t.Fatalf("%T is not a message", target)
	}
	return vector.RequestID, m
}

func TestReadFrameRejectsInvalidHeaders(t *testing.T) {
	header := func(length uint32, flags uint16) []byte {
		b := binary.LittleEndian.AppendUint32(nil, length)
		b = binary.LittleEndian.AppendUint16(b, uint16(TypePing))
		b = binary.LittleEndian.AppendUint16(b, flags)
		return binary.LittleEndian.AppendUint32(b, 1)
	}
	cases := map[string]struct {
		data []byte
		want error
	}{
		"length below header": {header(7, 0), ErrMalformed},
		"length past limit":   {header(MaxFrameSize-3, 0), ErrMalformed},
		"reserved flags":      {header(8, 1), ErrMalformed},
		"truncated payload":   {append(header(16, 0), make([]byte, 4)...), io.ErrUnexpectedEOF},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadFrame(bytes.NewReader(tc.data)); !errors.Is(err, tc.want) {
				t.Fatalf("ReadFrame error %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDecodeRejectsMalformedPayloads(t *testing.T) {
	cases := map[string]Frame{
		"trailing bytes":      {Type: TypePing, Payload: []byte{0}},
		"short payload":       {Type: TypePong, Payload: []byte{1}},
		"invalid UTF-8":       {Type: TypeClientError, Payload: []byte{1, 0, 0xff, 0, 0, 0, 0}},
		"count past payload":  {Type: TypeAck, Payload: []byte{0, 0, 0, 0xff, 0xff, 0, 0}},
		"bytes past payload":  {Type: TypeMeshChunk, Payload: []byte{1, 0, 0, 0, 0, 0, 9, 0, 0, 0, 1}},
		"attr header cut off": {Type: TypeMeshBegin, Payload: append(make([]byte, 20), 1, 0, 1, 0, 1)},
	}
	for name, frame := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(frame); !errors.Is(err, ErrMalformed) {
				t.Fatalf("Decode error %v, want ErrMalformed", err)
			}
		})
	}

	if _, err := Decode(Frame{Type: 0x7777}); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("Decode of an unknown type: %v, want ErrUnknownType", err)
	}
}
