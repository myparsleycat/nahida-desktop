package platform

import (
	"os"
	"testing"
)

func TestPackagedDevSignal(t *testing.T) {
	t.Setenv("NAHIDA_DEV", "1")
	if Packaged() {
		t.Fatal("NAHIDA_DEV nonempty must be unpackaged")
	}
	t.Setenv("NAHIDA_DEV", "")
	if !Packaged() {
		t.Fatal("empty NAHIDA_DEV must be packaged")
	}
}

func TestPackagedDefaultsToBuildMode(t *testing.T) {
	previous, existed := os.LookupEnv("NAHIDA_DEV")
	if err := os.Unsetenv("NAHIDA_DEV"); err != nil {
		t.Fatalf("Unsetenv: %v", err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv("NAHIDA_DEV", previous)
			return
		}
		_ = os.Unsetenv("NAHIDA_DEV")
	})

	if got := Packaged(); got != packagedBuild {
		t.Fatalf("Packaged() = %v, want build default %v", got, packagedBuild)
	}
}
