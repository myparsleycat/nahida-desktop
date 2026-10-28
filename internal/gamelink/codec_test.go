package gamelink

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

// The vectors are the contract the forked DLL is tested against as well, so a
// change to the encoding shows up here as a changed .bin file.
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
		Type      string
		RequestID uint32
		Message   json.RawMessage
	}
	if err := json.Unmarshal(data, &vector); err != nil {
		t.Fatal(err)
	}

	targets := map[string]Message{
		"Hello": &Hello{}, "HelloReply": &HelloReply{}, "Ping": &Ping{}, "Pong": &Pong{}, "ErrorReply": &ErrorReply{},
		"GetVars": &GetVars{}, "VarsReply": &VarsReply{}, "SetVars": &SetVars{}, "VarsApplied": &VarsApplied{},
		"Event": &Event{}, "PickArm": &PickArm{}, "PickArmed": &PickArmed{}, "PickResult": &PickResult{},
		"PickFailed": &PickFailed{},
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
	return vector.RequestID, reflect.ValueOf(target).Elem().Interface().(Message)
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
		"reserved flags":      {append(header(16, 1), make([]byte, 8)...), ErrMalformed},
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
		"trailing bytes":       {Type: TypePing, Payload: make([]byte, 9)},
		"short payload":        {Type: TypePickArmed, Payload: make([]byte, 7)},
		"bool out of range":    {Type: TypeVarsReply, Payload: []byte{1, 0, 0, 0, 2, 0, 0, 0, 0}},
		"invalid UTF-8":        {Type: TypeError, Payload: []byte{1, 0, 0xff, 0, 0}},
		"count past remaining": {Type: TypeGetVars, Payload: []byte{0xff, 0xff, 0, 0}},
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
