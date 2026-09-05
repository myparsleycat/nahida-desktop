package drive

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func TestParseSSEPreservesEventBoundaries(t *testing.T) {
	t.Parallel()
	type message struct{ event, data string }
	for _, tc := range []struct {
		name  string
		input string
		want  []message
	}{
		{"multiline-and-comment", ": heartbeat\r\nevent: progress\r\ndata: first\r\ndata: second\r\n\r\nevent: complete\r\ndata: {}\r\n\r\n", []message{{"progress", "first\nsecond"}, {"complete", "{}"}}},
		{"default-event-and-empty-data", "data:\n\ndata: final", []message{{"message", ""}, {"message", "final"}}},
		{"event-at-eof", "event: complete\ndata: {}", []message{{"complete", "{}"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []message
			err := parseSSE(iotest.OneByteReader(strings.NewReader(tc.input)), func(event, data string) error {
				got = append(got, message{event, data})
				return nil
			})
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseSSE = %#v, %v; want %#v", got, err, tc.want)
			}
		})
	}
}

func TestParseSSEStopsOnConsumerError(t *testing.T) {
	t.Parallel()
	want := errors.New("consumer stopped")
	calls := 0
	err := parseSSE(strings.NewReader("data: first\n\ndata: second\n\n"), func(_, _ string) error {
		calls++
		return want
	})
	if !errors.Is(err, want) || calls != 1 {
		t.Fatalf("parseSSE = %v after %d calls", err, calls)
	}
}

func TestParseSSEReturnsReadFailureAfterPendingEvent(t *testing.T) {
	t.Parallel()
	want := errors.New("stream interrupted")
	reader := io.MultiReader(strings.NewReader("event: progress\ndata: partial\n"), iotest.ErrReader(want))
	var received string
	err := parseSSE(reader, func(event, data string) error {
		received = event + ":" + data
		return nil
	})
	if !errors.Is(err, want) || received != "progress:partial" {
		t.Fatalf("parseSSE = %v, received %q", err, received)
	}
}
