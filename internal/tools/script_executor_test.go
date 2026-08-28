package tools

import (
	"bytes"
	"io"
	"testing"
)

type scriptChunkReader struct {
	chunks [][]byte
}

func (r *scriptChunkReader) Read(buffer []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	chunk := r.chunks[0]
	r.chunks = r.chunks[1:]
	return copy(buffer, chunk), nil
}

type scriptInputBuffer struct {
	bytes.Buffer
}

func (*scriptInputBuffer) Close() error { return nil }

func TestScriptExecutorStreamsPartialLinesAndRepliesToPrompt(t *testing.T) {
	type logEntry struct {
		message     string
		replaceLast bool
	}
	var logs []logEntry
	input := &scriptInputBuffer{}
	executor := newScriptExecutor(func(message string, replaceLast bool) {
		logs = append(logs, logEntry{message: message, replaceLast: replaceLast})
	})
	executor.stdin = input
	executor.consume(&scriptChunkReader{chunks: [][]byte{
		[]byte("Working"),
		[]byte(" 50%\nPress any"),
		[]byte(" key to continue..."),
	}})

	want := []logEntry{
		{message: "Working"},
		{message: "Working 50%", replaceLast: true},
		{message: "Press any"},
		{message: "Press any key to continue...", replaceLast: true},
	}
	if len(logs) != len(want) {
		t.Fatalf("logs = %#v", logs)
	}
	for index := range want {
		if logs[index] != want[index] {
			t.Fatalf("logs[%d] = %#v, want %#v", index, logs[index], want[index])
		}
	}
	if input.String() != "\n" {
		t.Fatalf("auto reply = %q", input.String())
	}
}
