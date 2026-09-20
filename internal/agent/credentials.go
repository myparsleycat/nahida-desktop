package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/platform"
)

const credentialsKey = "agent_llm_credentials"

const (
	credentialKindAPI   = "api"
	credentialKindOAuth = "oauth"
)

// oauthRefreshMargin refreshes an access token shortly before it expires so a run does not start
// with credentials that die mid-turn.
const oauthRefreshMargin = 5 * time.Minute

// providerCredential is the stored secret of one provider: an API key, or the tokens of a ChatGPT
// Pro/Plus login. Values never leave this package unencrypted.
type providerCredential struct {
	Type      string `json:"type"`
	Key       string `json:"key,omitempty"`
	Access    string `json:"access,omitempty"`
	Refresh   string `json:"refresh,omitempty"`
	Expires   int64  `json:"expires,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	Email     string `json:"email,omitempty"`
}

// needsRefresh reports whether the credential holds an OAuth token that is missing or about to
// expire.
func (c providerCredential) needsRefresh(now time.Time) bool {
	if c.Type != credentialKindOAuth {
		return false
	}
	return c.Access == "" || c.Expires-now.UnixMilli() < oauthRefreshMargin.Milliseconds()
}

func readCredentials(
	ctx context.Context,
	client *db.Client,
	crypto *platform.Crypto,
) (map[string]providerCredential, error) {
	value, err := client.Settings.GetValue(ctx, credentialsKey)
	if err != nil || value == nil || *value == "" {
		return map[string]providerCredential{}, err
	}
	plain, err := crypto.DecryptString(*value)
	if err != nil {
		return nil, fmt.Errorf("decrypt agent provider credentials: %w", err)
	}
	credentials := map[string]providerCredential{}
	if err := json.Unmarshal([]byte(plain), &credentials); err != nil {
		return nil, fmt.Errorf("decode agent provider credentials: %w", err)
	}
	return credentials, nil
}

func writeCredentials(
	ctx context.Context,
	client *db.Client,
	crypto *platform.Crypto,
	credentials map[string]providerCredential,
) error {
	if len(credentials) == 0 {
		return client.Settings.ApplyBatch(ctx, nil, []string{credentialsKey})
	}
	encoded, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	encrypted, err := crypto.EncryptString(string(encoded))
	if err != nil {
		return fmt.Errorf("encrypt agent provider credentials: %w", err)
	}
	return client.Settings.ApplyBatch(ctx, map[string]*string{credentialsKey: &encrypted}, nil)
}

// readStoredCredential reads one provider credential under the credential lock.
func (s *Service) readStoredCredential(
	ctx context.Context,
	client *db.Client,
	providerID string,
) (providerCredential, bool, error) {
	s.credentialMu.Lock()
	defer s.credentialMu.Unlock()
	credentials, err := readCredentials(ctx, client, s.crypto)
	if err != nil {
		return providerCredential{}, false, err
	}
	credential, found := credentials[providerID]
	return credential, found, nil
}

// apiKey returns the key of a stored API-key credential; an OAuth login carries no key.
func (c providerCredential) apiKey() string {
	if c.Type != credentialKindAPI {
		return ""
	}
	return c.Key
}

func credentialView(credential providerCredential) AgentCredentialView {
	switch credential.Type {
	case credentialKindAPI:
		if credential.Key == "" {
			return AgentCredentialView{Kind: "none"}
		}
		return AgentCredentialView{Kind: credentialKindAPI}
	case credentialKindOAuth:
		view := AgentCredentialView{Kind: credentialKindOAuth, AccountLabel: credential.Email}
		if view.AccountLabel == "" {
			view.AccountLabel = credential.AccountID
		}
		if credential.Expires > 0 {
			view.ExpiresAt = time.UnixMilli(credential.Expires).UTC().Format(time.RFC3339)
		}
		return view
	default:
		return AgentCredentialView{Kind: "none"}
	}
}

// applyCredential attaches the provider credential summary to a settings view.
func applyCredential(view AgentSettingsView, credential providerCredential) AgentSettingsView {
	view.Credential = credentialView(credential)
	return view
}
