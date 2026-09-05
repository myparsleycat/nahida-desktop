package mod

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestParseINIPreservesPartialResultAndReportsReadLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.ini")
	if err := os.WriteFile(path, []byte("[KeyTest]\nkey = k\n$test = 0, 1\n"+strings.Repeat("x", 2<<20)), 0o600); err != nil {
		t.Fatal(err)
	}
	var batch infra.DiagnosticBatch
	result := parseINI(path, batch.Add)
	if result.Path != path || !result.HasToggleKey {
		t.Fatalf("lost partial result: %+v", result)
	}
	var output bytes.Buffer
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
	batch.Report(log, "Mod", "scan")
	for _, want := range []string{"token too long", "read-ini", "test.ini"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %s: %s", want, output.String())
		}
	}
	output.Reset()
	var absent infra.DiagnosticBatch
	parseINI(filepath.Join(t.TempDir(), "absent.ini"), absent.Add)
	absent.Report(log, "Mod", "scan")
	if output.Len() != 0 {
		t.Fatal("normal absence was logged")
	}
}
