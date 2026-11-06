package setting

import (
	"context"
	"strings"
	"testing"
)

func TestGitHubTokenIsEncryptedVerifiedAndHidden(t *testing.T) {
	var applied []string
	valid := "github_pat_valid"
	s, _ := openTemp(t, Options{Hooks: Hooks{
		VerifyGitHubToken: func(_ context.Context, token string) (int64, error) {
			if token != valid {
				return 0, ErrGitHubTokenInvalid
			}
			return 5000, nil
		},
		AfterGitHubTokenChanged: func(token string) { applied = append(applied, token) },
	}})
	ctx := context.Background()

	if _, err := s.SetGitHubToken(ctx, GitHubTokenInput{Action: "replace", Token: "rejected"}); err == nil ||
		!strings.Contains(err.Error(), "githubToken.invalid") {
		t.Fatalf("invalid token err = %v", err)
	}
	if status, _ := s.GetGitHubTokenStatus(ctx); status.Configured || len(applied) != 0 {
		t.Fatalf("rejected token was saved: status=%+v applied=%q", status, applied)
	}

	limit, err := s.SetGitHubToken(ctx, GitHubTokenInput{Action: "replace", Token: " " + valid + "\n"})
	if err != nil || limit != 5000 {
		t.Fatalf("limit=%d err=%v", limit, err)
	}
	if raw := rawValue(t, s, githubTokenStorageKey); strings.Contains(raw, valid) {
		t.Fatal("plaintext token in database")
	}
	if token, err := s.LoadGitHubToken(ctx); err != nil || token != valid {
		t.Fatalf("loaded token=%q err=%v", token, err)
	}
	if status, err := s.GetGitHubTokenStatus(ctx); err != nil || !status.Configured || status.Unreadable {
		t.Fatalf("saved status=%+v err=%v", status, err)
	}
	rows, err := s.AdvancedGetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Key == githubTokenStorageKey {
			t.Fatal("GitHub token listed in advanced settings")
		}
	}
	if err := s.AdvancedSet(ctx, githubTokenStorageKey, "x"); err == nil {
		t.Fatal("advanced settings changed the GitHub token")
	}

	if _, err := s.SetGitHubToken(ctx, GitHubTokenInput{Action: "clear"}); err != nil {
		t.Fatal(err)
	}
	if status, _ := s.GetGitHubTokenStatus(ctx); status.Configured {
		t.Fatal("cleared token still configured")
	}
	if len(applied) != 2 || applied[0] != valid || applied[1] != "" {
		t.Fatalf("applied tokens = %q", applied)
	}
}

func TestUndecryptableGitHubTokenIsReportedUnreadable(t *testing.T) {
	s, _ := openTemp(t, Options{})
	ctx := context.Background()
	value := "not-dpapi"
	if err := s.client.Settings.Upsert(ctx, githubTokenStorageKey, &value); err != nil {
		t.Fatal(err)
	}

	status, err := s.GetGitHubTokenStatus(ctx)
	if err != nil || !status.Configured || !status.Unreadable {
		t.Fatalf("status=%+v err=%v, want unreadable", status, err)
	}
	if _, err := s.LoadGitHubToken(ctx); err == nil {
		t.Fatal("undecryptable token loaded")
	}
}
