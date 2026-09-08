package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"nahida.live/desktop/internal/infra"
)

type proxyTestTransport struct{ requests int }

func (p *proxyTestTransport) RoundTrip(*http.Request) (*http.Response, error) {
	p.requests++
	return nil, errors.New("proxy test blocked")
}

func TestAuthRawRequestsUseConfiguredTransport(t *testing.T) {
	transport := &proxyTestTransport{}
	client := infra.NewClient()
	auth := NewWithOptions(Options{HTTP: client})
	// Runtime applies the transport after the service is constructed.
	client.UseTransport(transport)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://auth.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := auth.do(request); err == nil {
		_ = response.Body.Close()
		t.Fatal("request escaped proxy policy")
	}
	if transport.requests != 1 {
		t.Fatal("raw auth request bypassed configured client")
	}
}
