package app

import (
	"os"
	"path/filepath"
	gostdruntime "runtime"
	"strings"
	"testing"
)

func TestRunAcquiresSingleInstanceBeforeBackendBoot(t *testing.T) {
	t.Parallel()
	_, file, _, ok := gostdruntime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	lockIndex := strings.Index(source, "app := newLockedApplication(")
	bootIndex := strings.Index(source, "bootRuntime(context.Background(), rt, in,")
	if lockIndex < 0 || bootIndex < 0 || lockIndex >= bootIndex {
		t.Fatalf("single-instance newLockedApplication must precede bootRuntime (indices %d, %d)", lockIndex, bootIndex)
	}
}
