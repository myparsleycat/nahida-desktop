package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestLoginFailureDiagnosticsPreserveMessage(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		body, evidence string
	}{
		{"forbidden", 403, "", "403"}, {"rate", 429, "", "429"}, {"server", 503, "", "503"},
		{"html", 200, "<html>", "invalid character"}, {"empty", 200, "", "EOF"},
		{"truncated", 200, `{"state":`, "unexpected EOF"},
		{"field", 200, `{"state":123}`, "expected string, got float64"},
		{"missing", 200, `{}`, "missing login field"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
			a := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{BackendURL: "https://example.com"}), Do: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			}})
			err := a.StartLogin(context.Background())
			if !errors.Is(err, errIWantToLogin) || err.Error() != errIWantToLogin.Error() {
				t.Fatalf("contract changed: %v", err)
			}
			log.ServiceErrorMarshaler("Auth")(err)
			for _, want := range []string{test.evidence, "start-login", "contentType", "status"} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("missing %q: %s", want, output.String())
				}
			}
		})
	}
}

type diagnosticCrypto struct{ passCrypto }

func (diagnosticCrypto) DecryptString(string) (string, error) {
	return "", errors.New("ciphertext authentication failed")
}

type diagnosticStore struct{ memStore }

func (*diagnosticStore) UpdateValue(context.Context, string, *string) error {
	return errors.New("token deletion database locked")
}

func TestTokenRecoveryKeepsEmptyResultAndBothCauses(t *testing.T) {
	var output bytes.Buffer
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
	secret := "private-encrypted-token"
	store := &diagnosticStore{memStore: memStore{value: &secret}}
	a := NewWithOptions(Options{Store: store, Crypto: diagnosticCrypto{}, Log: log})
	token, err := a.getToken(context.Background())
	if token != "" || err != nil {
		t.Fatalf("recovery contract changed: %q, %v", token, err)
	}
	for _, want := range []string{"ciphertext authentication failed", "token deletion database locked", `"cleanupFailed":true`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q: %s", want, output.String())
		}
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("encrypted token leaked")
	}
}
