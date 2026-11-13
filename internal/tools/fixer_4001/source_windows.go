//go:build windows

package fixer4001

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"nahida.live/desktop/internal/infra"
)

// checkoutD3DSource fetches tag from sourceURL into projectPath. proxyURL is the app's local proxy relay, which
// applies the proxy settings of the app to git; "" leaves git to its own configuration.
func checkoutD3DSource(ctx context.Context, gitPath, proxyURL, sourceURL, tag, projectPath string) (err error) {
	stage := "create-source-directory"
	defer func() {
		err = infra.AnnotateError(err, infra.Diagnostic{
			Operation: "4001Fixer", Stage: stage,
			Fields: map[string]any{
				"sourceURL": sourceURL, "version": tag, "projectPath": projectPath,
				"executablePath": gitPath, "proxyRelay": proxyURL != "", "cleanupPending": true,
			},
		})
	}()
	if err := os.MkdirAll(projectPath, 0o700); err != nil {
		return fmt.Errorf("create XXMI source directory: %w", err)
	}

	// The version generator needs origin, HEAD and the selected tag. A source ZIP has none of them.
	// Fetch an explicit tag ref so a branch with the same name cannot be built by mistake.
	ref := "refs/tags/" + tag
	steps := []struct {
		stage string
		args  []string
	}{
		{stage: "validate-tag", args: []string{"check-ref-format", ref}},
		{stage: "init-source", args: []string{"init"}},
		{stage: "set-origin", args: []string{"remote", "add", "origin", sourceURL}},
		{stage: "fetch-tag", args: []string{"fetch", "--depth=1", "--no-tags", "origin", ref + ":" + ref}},
		{stage: "checkout-tag", args: []string{"checkout", "--detach", ref}},
	}
	base := []string{"-C", projectPath}
	env := os.Environ()
	if proxyURL != "" {
		base = append([]string{"-c", "http.proxy=" + proxyURL}, base...)
		env = withoutProxyVariables(env)
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	for _, step := range steps {
		stage = step.stage
		cmd := exec.CommandContext(ctx, gitPath, append(append([]string{}, base...), step.args...)...)
		cmd.Env = env
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("prepare XXMI source (%s): %w\n%s", stage, err, tailBuildOutput(string(output), 40))
		}
	}
	return nil
}

// withoutProxyVariables drops the proxy variables curl reads, including NO_PROXY, which would otherwise send
// matching hosts past the relay.
func withoutProxyVariables(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
