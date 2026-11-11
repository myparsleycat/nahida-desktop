package setting

import (
	"context"
	"errors"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const githubTokenStorageKey = "network_github_token"

var (
	// ErrGitHubTokenInvalid is returned by the VerifyGitHubToken hook for a
	// token GitHub does not accept. Its text is a renderer contract.
	ErrGitHubTokenInvalid = errors.New("githubToken.invalid")

	errGitHubTokenEmpty        = errors.New("githubToken.empty")
	errGitHubTokenAction       = errors.New("githubToken.action")
	errGitHubTokenStorage      = errors.New("githubToken.storageUnavailable")
	errGitHubTokenUnverifiable = errors.New("githubToken.verificationUnavailable")
)

type GitHubTokenStatus struct {
	Configured bool `json:"configured"`
	// Rejected means GitHub refused the saved token and requests are anonymous.
	Rejected bool `json:"rejected"`
	// Unreadable means the saved token cannot be decrypted for this Windows
	// user, so requests are anonymous until it is entered again.
	Unreadable bool `json:"unreadable"`
}

type GitHubTokenInput struct {
	// Action is "replace" or "clear".
	Action string `json:"action"`
	Token  string `json:"token"`
}

func (s *Setting) GetGitHubTokenStatus(ctx context.Context) (GitHubTokenStatus, error) {
	s.githubTokenMu.Lock()
	defer s.githubTokenMu.Unlock()
	stored, err := s.storedGitHubToken(ctx)
	if err != nil {
		return GitHubTokenStatus{}, settingError(err, "github-token", "read", githubTokenStorageKey,
			infra.DiagnosticError)
	}
	if stored == "" {
		return GitHubTokenStatus{}, nil
	}
	if _, err := platform.NewCrypto().DecryptString(stored); err != nil {
		return GitHubTokenStatus{Configured: true, Unreadable: true}, nil
	}
	return GitHubTokenStatus{Configured: true, Rejected: s.opts.Hooks.githubTokenRejected()}, nil
}

// SetGitHubToken verifies and saves the token, or clears it, and returns the
// hourly request limit GitHub reports for the saved token (0 after clearing).
func (s *Setting) SetGitHubToken(ctx context.Context, input GitHubTokenInput) (int64, error) {
	switch input.Action {
	case "clear":
		s.githubTokenMu.Lock()
		defer s.githubTokenMu.Unlock()
		if s.client == nil {
			return 0, errGitHubTokenStorage
		}
		if err := s.client.Settings.Delete(ctx, githubTokenStorageKey); err != nil {
			return 0, settingError(err, "github-token", "clear", githubTokenStorageKey, infra.DiagnosticError)
		}
		s.opts.Hooks.githubTokenChanged("")
		return 0, nil
	case "replace":
	default:
		return 0, settingError(errGitHubTokenAction, "github-token", "validate", githubTokenStorageKey,
			infra.DiagnosticWarn)
	}

	token := strings.TrimSpace(input.Token)
	if token == "" {
		return 0, errGitHubTokenEmpty
	}
	// Verification is a network call; it runs unlocked so status reads are not
	// held behind it. Store and apply stay paired under the lock.
	limit, err := s.opts.Hooks.verifyGitHubToken(ctx, token)
	if errors.Is(err, ErrGitHubTokenInvalid) {
		return 0, ErrGitHubTokenInvalid
	}
	if err != nil {
		return 0, settingError(err, "github-token", "verify", githubTokenStorageKey, infra.DiagnosticWarn)
	}

	s.githubTokenMu.Lock()
	defer s.githubTokenMu.Unlock()
	if s.client == nil {
		return 0, errGitHubTokenStorage
	}
	encrypted, err := platform.NewCrypto().EncryptString(token)
	if err != nil {
		return 0, settingError(err, "github-token", "encrypt", githubTokenStorageKey, infra.DiagnosticError)
	}
	if err := s.client.Settings.Upsert(ctx, githubTokenStorageKey, &encrypted); err != nil {
		return 0, settingError(err, "github-token", "save", githubTokenStorageKey, infra.DiagnosticError)
	}
	s.opts.Hooks.githubTokenChanged(token)
	return limit, nil
}

// LoadGitHubToken returns the saved token for runtime startup.
func (s *Setting) LoadGitHubToken(ctx context.Context) (string, error) {
	s.githubTokenMu.Lock()
	defer s.githubTokenMu.Unlock()
	stored, err := s.storedGitHubToken(ctx)
	if err != nil || stored == "" {
		return "", settingError(err, "github-token", "load", githubTokenStorageKey, infra.DiagnosticWarn)
	}
	token, err := platform.NewCrypto().DecryptString(stored)
	if err != nil {
		return "", settingError(err, "github-token", "decrypt", githubTokenStorageKey, infra.DiagnosticWarn)
	}
	return token, nil
}

func (s *Setting) storedGitHubToken(ctx context.Context) (string, error) {
	if s.client == nil {
		return "", errGitHubTokenStorage
	}
	value, err := s.client.Settings.GetValue(ctx, githubTokenStorageKey)
	if err != nil || value == nil {
		return "", err
	}
	return *value, nil
}
