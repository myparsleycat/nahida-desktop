// Package gamelink is the desktop end of the channel to the forked 3DMigoto DLL
// running in a game. PROTOCOL.md is the wire contract.
package gamelink

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	ProtocolVersion = 1
	// MaxFrameSize bounds a whole frame, length field included.
	MaxFrameSize = 4 << 20

	headerSize = 12
)

var (
	ErrMalformed   = errors.New("malformed desktop channel frame")
	ErrUnknownType = errors.New("unknown desktop channel message type")
)

type Frame struct {
	Type      MessageType
	RequestID uint32
	Payload   []byte
}

func ReadFrame(r io.Reader) (Frame, error) {
	var header [headerSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Frame{}, err
	}
	length := binary.LittleEndian.Uint32(header[0:4])
	if length < headerSize-4 || length > MaxFrameSize-4 {
		return Frame{}, fmt.Errorf("%w: length %d", ErrMalformed, length)
	}
	if flags := binary.LittleEndian.Uint16(header[6:8]); flags != 0 {
		return Frame{}, fmt.Errorf("%w: flags %#x", ErrMalformed, flags)
	}

	frame := Frame{
		Type:      MessageType(binary.LittleEndian.Uint16(header[4:6])),
		RequestID: binary.LittleEndian.Uint32(header[8:12]),
		Payload:   make([]byte, length-(headerSize-4)),
	}
	if _, err := io.ReadFull(r, frame.Payload); err != nil {
		return Frame{}, err
	}
	return frame, nil
}

// AppendFrame encodes a frame, which goes to the pipe in one write.
func AppendFrame(dst []byte, frame Frame) ([]byte, error) {
	if len(frame.Payload) > MaxFrameSize-headerSize {
		return dst, fmt.Errorf("%w: payload of %d bytes", ErrMalformed, len(frame.Payload))
	}
	dst = binary.LittleEndian.AppendUint32(dst, uint32(headerSize-4+len(frame.Payload)))
	dst = binary.LittleEndian.AppendUint16(dst, uint16(frame.Type))
	dst = binary.LittleEndian.AppendUint16(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, frame.RequestID)
	return append(dst, frame.Payload...), nil
}
