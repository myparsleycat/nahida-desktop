//go:build windows

package inject

import "testing"

func TestInjectorFailureReasonsMatchReferenceCodes(t *testing.T) {
	t.Parallel()
	for code, want := range map[uintptr]string{
		100: "another 3DMigoto loader instance is running",
		200: "failed to load the module DLL",
		300: "module DLL is missing the expected entry point",
		400: "failed to set up the Windows hook",
	} {
		if got := hookFailureReason(code); got != want {
			t.Errorf("HookLibrary code %d: got %q, want %q", code, got, want)
		}
	}
	for code, want := range map[uintptr]string{
		100: "target process was not found",
		110: "invalid DLL path",
		120: "failed to resolve kernel32.dll",
		130: "failed to resolve LoadLibraryW",
		200: "failed to allocate remote memory",
		300: "failed to write the DLL path to process memory",
		400: "failed to create a remote thread",
		500: "injection thread timed out",
		510: "waiting for the injection thread failed",
		600: "DLL injection failed",
		700: "unknown low-level error",
	} {
		if got := injectFailureReason(code); got != want {
			t.Errorf("Inject code %d: got %q, want %q", code, got, want)
		}
	}
	if got := injectFailureReason(999); got != "unknown injection failure" {
		t.Fatalf("unknown Inject code: %q", got)
	}
}
