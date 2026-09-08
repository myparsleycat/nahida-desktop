package platform

import "testing"

func TestUserAgentAddsDevSuffixWhenUnpackaged(t *testing.T) {
	previous := AppVersion
	t.Cleanup(func() { AppVersion = previous })
	AppVersion = "3.7.0"

	t.Setenv("NAHIDA_DEV", "1")
	if got := UserAgent(); got != "Nahida Desktop/3.7.0-dev" {
		t.Fatalf("unpackaged UserAgent() = %q", got)
	}

	t.Setenv("NAHIDA_DEV", "")
	if got := UserAgent(); got != "Nahida Desktop/3.7.0" {
		t.Fatalf("packaged UserAgent() = %q", got)
	}
}

func TestUserAgentDoesNotDoubleDevSuffix(t *testing.T) {
	previous := AppVersion
	t.Cleanup(func() { AppVersion = previous })
	AppVersion = "3.7.0-dev"
	t.Setenv("NAHIDA_DEV", "1")
	if got := UserAgent(); got != "Nahida Desktop/3.7.0-dev" {
		t.Fatalf("UserAgent() = %q", got)
	}
}
