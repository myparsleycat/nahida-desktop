package elevated

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	protocolVersion = 1
	maxMessageSize  = 64 * 1024
	operationHello  = "session.hello"
	operationPing   = "session.ping"
	operationStop   = "session.stop"
	operationKeys   = "input.send_keys"
)

type message struct {
	Version   int             `json:"version"`
	ID        uint64          `json:"id"`
	Operation string          `json:"operation"`
	Secret    string          `json:"secret,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	OK        bool            `json:"ok,omitempty"`
	ErrorCode string          `json:"errorCode,omitempty"`
	Error     string          `json:"error,omitempty"`
}

func writeMessage(writer io.Writer, value message) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(payload) > maxMessageSize {
		return fmt.Errorf("elevated helper message is too large: %d bytes", len(payload))
	}
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32(len(payload)))
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}

func readMessage(reader io.Reader) (message, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return message{}, err
	}
	size := binary.LittleEndian.Uint32(header)
	if size == 0 || size > maxMessageSize {
		return message{}, fmt.Errorf("invalid elevated helper message size: %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return message{}, err
	}
	var value message
	decoderErr := json.Unmarshal(payload, &value)
	if decoderErr != nil {
		return message{}, errors.New("invalid elevated helper message")
	}
	return value, nil
}
