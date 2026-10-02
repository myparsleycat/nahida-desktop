//go:build windows && integration

package fixer4001

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckoutD3DSourcePreservesSelectedTagMetadata(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "remote source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	runSourceTestGit(t, source, "init")
	solution := filepath.Join(source, "StereovisionHacks.sln")
	if err := os.WriteFile(solution, []byte("selected release"), 0o600); err != nil {
		t.Fatal(err)
	}
	runSourceTestGit(t, source, "add", ".")
	runSourceTestGit(t, source, "commit", "-m", "release fixture")
	commit := runSourceTestGit(t, source, "rev-parse", "HEAD")
	runSourceTestGit(t, source, "tag", "-a", "v1.2.0", "-m", "annotated release")
	runSourceTestGit(t, source, "tag", "v1.2.1")

	if err := os.WriteFile(solution, []byte("newer branch"), 0o600); err != nil {
		t.Fatal(err)
	}
	runSourceTestGit(t, source, "add", ".")
	runSourceTestGit(t, source, "commit", "-m", "newer fixture")
	runSourceTestGit(t, source, "branch", "v1.2.0")
	runSourceTestGit(t, source, "branch", "v9.9.9")

	for _, tc := range []struct {
		name string
		tag  string
	}{
		{name: "annotated tag with same named branch", tag: "v1.2.0"},
		{name: "lightweight tag", tag: "v1.2.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := filepath.Join(t.TempDir(), "XXMI source with spaces")
			if err := checkoutD3DSource(context.Background(), source, tc.tag, project); err != nil {
				t.Fatal(err)
			}
			if got := runSourceTestGit(t, project, "config", "--get", "remote.origin.url"); got != source {
				t.Fatalf("origin = %q, want %q", got, source)
			}
			if got := runSourceTestGit(
				t,
				project,
				"describe",
				"--tags",
				"--match",
				"v[0-9]*",
				"--abbrev=0",
			); got != tc.tag {
				t.Fatalf("version tag = %q, want %q", got, tc.tag)
			}
			if got := runSourceTestGit(t, project, "rev-parse", "HEAD"); got != commit {
				t.Fatalf("HEAD = %q, want release commit %q", got, commit)
			}
			if got := runSourceTestGit(t, project, "rev-list", "-n", "1", tc.tag); got != commit {
				t.Fatalf("tag commit = %q, want %q", got, commit)
			}
			if got := runSourceTestGit(t, project, "rev-list", tc.tag+"..HEAD", "--count"); got != "0" {
				t.Fatalf("commits since tag = %q", got)
			}
			if got := runSourceTestGit(t, project, "status", "--porcelain"); got != "" {
				t.Fatalf("source is dirty: %s", got)
			}
			contents, err := os.ReadFile(filepath.Join(project, "StereovisionHacks.sln"))
			if err != nil || string(contents) != "selected release" {
				t.Fatalf("solution = %q, %v", contents, err)
			}
			if got, err := findStereovisionProject(project); err != nil || got != project {
				t.Fatalf("project = %q, %v", got, err)
			}
		})
	}

	t.Run("branch is not a release tag", func(t *testing.T) {
		t.Parallel()
		project := filepath.Join(t.TempDir(), "source")
		err := checkoutD3DSource(context.Background(), source, "v9.9.9", project)
		if err == nil || !strings.Contains(err.Error(), "fetch-tag") {
			t.Fatalf("missing tag error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(project, "StereovisionHacks.sln")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("branch source was checked out: %v", err)
		}
	})
}

func TestCheckoutD3DSourceRejectsInvalidTagsBeforeFetching(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{"", "../outside", "v1:refs/heads/main", "v1 && echo injected", "v1\n"} {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()
			project := filepath.Join(t.TempDir(), "source")
			err := checkoutD3DSource(context.Background(), "unused-remote", tag, project)
			if err == nil || !strings.Contains(err.Error(), "validate-tag") {
				t.Fatalf("invalid tag error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(project, ".git")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("repository created for invalid tag: %v", err)
			}
		})
	}
}

func TestCheckoutD3DSourceRequiresGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := checkoutD3DSource(context.Background(), "unused-remote", "v1.2.0", filepath.Join(t.TempDir(), "source"))
	if !errors.Is(err, exec.ErrNotFound) || !strings.Contains(err.Error(), "install Git and restart Nahida") {
		t.Fatalf("missing Git error = %v", err)
	}
}

func TestCheckoutD3DSourceHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := checkoutD3DSource(ctx, "unused-remote", "v1.2.0", filepath.Join(t.TempDir(), "source"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled checkout error = %v", err)
	}
}

func runSourceTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := append(
		[]string{"-C", dir, "-c", "user.name=4001Fixer Test", "-c", "user.email=fixer@example.invalid"},
		args...)
	output, err := exec.Command("git", command...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
