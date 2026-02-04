package ipc

import (
	"encoding/json"
	"fmt"
	"os"
)

// MessageType defines the type of IPC message
type MessageType string

const (
	TypeLog      MessageType = "log"
	TypeProgress MessageType = "progress"
	TypeSuccess  MessageType = "success"
	TypeError    MessageType = "error"
)

// Message represents the standard structure for IPC communication
type Message struct {
	Type    MessageType `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

// ProgressPayload represents payload for progress updates
type ProgressPayload struct {
	Percent float64 `json:"percent"`
	Message string  `json:"message,omitempty"`
}

// ErrorPayload represents payload for errors
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Send sends a message to stdout in JSON Line format
func Send(msgType MessageType, payload interface{}) {
	msg := Message{
		Type:    msgType,
		Payload: payload,
	}
	bytes, err := json.Marshal(msg)
	if err != nil {
		// Fallback error logging to stderr
		fmt.Fprintf(os.Stderr, "Failed to marshal IPC message: %v\n", err)
		return
	}
	fmt.Println(string(bytes))
}

// SendProgress gives a shorthand for sending progress
func SendProgress(percent float64, message string) {
	Send(TypeProgress, ProgressPayload{
		Percent: percent,
		Message: message,
	})
}

// SendSuccess sends a success message with data
func SendSuccess(data interface{}) {
	Send(TypeSuccess, data)
}

// SendError sends an error message
func SendError(code string, message string) {
	Send(TypeError, ErrorPayload{
		Code:    code,
		Message: message,
	})
}

// SendLog sends a log message
func SendLog(message string) {
	Send(TypeLog, message)
}
