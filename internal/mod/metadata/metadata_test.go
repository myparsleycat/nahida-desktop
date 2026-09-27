package metadata

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestInitialize(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"id":"first","feature":{"enabled":true}}`)
	if err := Initialize(dir, original); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(dir, []byte(`{"id":"second"}`)); !errors.Is(err, os.ErrExist) {
		t.Fatalf("initialize existing metadata = %v, want os.ErrExist", err)
	}
	raw, err := Read(dir)
	if err != nil || string(raw) != string(original) {
		t.Fatalf("read initialized metadata = %q, %v", raw, err)
	}
}

func TestReadMissing(t *testing.T) {
	if _, err := Read(t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read missing metadata = %v, want os.ErrNotExist", err)
	}
}

func TestWriteReplacesContents(t *testing.T) {
	dir := t.TempDir()
	if err := Initialize(dir, []byte(`{"id":"first","source":"mod"}`)); err != nil {
		t.Fatal(err)
	}
	updated := []byte(`{"id":"first","feature":{"count":1234567890123456789}}` + "\n")
	if err := Write(dir, updated); err != nil {
		t.Fatal(err)
	}
	raw, err := Read(dir)
	if err != nil || string(raw) != string(updated) {
		t.Fatalf("read written metadata = %q, %v", raw, err)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "nhd.json.backup-*")); err != nil || len(matches) != 0 {
		t.Fatalf("metadata backups = %v, %v", matches, err)
	}
}

func TestWriteRejectsInvalidJSONWithoutChangingFile(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"id":"first"}`)
	if err := Initialize(dir, original); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, []byte("{")); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
	raw, err := Read(dir)
	if err != nil || string(raw) != string(original) {
		t.Fatalf("metadata after failed write = %q, %v", raw, err)
	}
}

func TestUpdateSerializesReadModifyWrite(t *testing.T) {
	dir := t.TempDir()
	if err := Initialize(dir, []byte(`{"count":0}`)); err != nil {
		t.Fatal(err)
	}

	var workers sync.WaitGroup
	results := make(chan error, 20)
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- Update(dir, func(raw []byte) ([]byte, error) {
				var value struct {
					Count int `json:"count"`
				}
				if err := json.Unmarshal(raw, &value); err != nil {
					return nil, err
				}
				value.Count++
				return json.Marshal(value)
			})
		}()
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := Read(dir)
	if err != nil || string(raw) != `{"count":20}` {
		t.Fatalf("updated metadata = %q, %v", raw, err)
	}
}

func TestUpdateLeavesFileUnchangedOnCallbackError(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"id":"first"}`)
	if err := Initialize(dir, original); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("update failed")
	if err := Update(dir, func([]byte) ([]byte, error) { return nil, failure }); !errors.Is(err, failure) {
		t.Fatalf("update error = %v", err)
	}
	if raw, err := Read(dir); err != nil || string(raw) != string(original) {
		t.Fatalf("metadata after failed update = %q, %v", raw, err)
	}
}

func TestInitializeRemovesFileWhenHideFails(t *testing.T) {
	dir := t.TempDir()
	realHide := hideFile
	t.Cleanup(func() { hideFile = realHide })
	hideFile = func(string) error { return errors.New("hide failed") }

	if err := Initialize(dir, []byte(`{}`)); err == nil {
		t.Fatal("expected hide failure")
	}
	if _, err := Read(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("metadata after failed initialization = %v", err)
	}
}

func TestWriteBatchRestoresEveryDirectoryWhenHideFails(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	firstMetadata := filepath.Join(first, fileName)
	if err := os.WriteFile(firstMetadata, []byte("original metadata\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	realHide := hideFile
	t.Cleanup(func() { hideFile = realHide })
	hideFile = func(path string) error {
		if filepath.Dir(path) == second {
			return errors.New("simulated hidden-attribute failure")
		}
		return nil
	}

	err := WriteBatch([]WriteEntry{
		{Dir: first, Data: []byte(`{"source":"mod"}`)},
		{Dir: second, Data: []byte(`{"source":"mod"}`)},
	})
	if err == nil || !strings.Contains(err.Error(), "simulated hidden-attribute failure") {
		t.Fatalf("write error = %v", err)
	}
	if raw, err := os.ReadFile(firstMetadata); err != nil || string(raw) != "original metadata\n" {
		t.Fatalf("restored first metadata = %q, %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(second, fileName)); !os.IsNotExist(err) {
		t.Fatalf("new second metadata should be removed, stat = %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(root, "*", "nhd.json.backup-*")); err != nil || len(matches) != 0 {
		t.Fatalf("metadata backups = %v, %v", matches, err)
	}
}
