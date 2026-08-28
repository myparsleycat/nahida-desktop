//go:build windows

package platform

import (
	"reflect"
	"testing"
)

func TestPowerBlockerDeduplicatesAndRestoresExecutionState(t *testing.T) {
	t.Parallel()
	var calls []bool
	p := newPowerBlocker(func(block bool) error {
		calls = append(calls, block)
		return nil
	})
	if err := p.Set(true); err != nil {
		t.Fatal(err)
	}
	if err := p.Set(true); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if want := []bool{true, false}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("execution-state calls = %v, want %v", calls, want)
	}
	if err := p.Set(false); err == nil {
		t.Fatal("Set after Close should fail")
	}
}
