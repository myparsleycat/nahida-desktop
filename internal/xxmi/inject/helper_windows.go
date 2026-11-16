//go:build windows

package inject

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"nahida.live/desktop/internal/elevated"
)

const HelperOperation = "xxmi.launch"

// helperGrace is how long a helper launch may outlast the spec's process and window waits for injection
// and cleanup.
const helperGrace = 60 * time.Second

var errInvalidHelperLaunch = errors.New("invalid XXMI launch request")

func HelperHandler() elevated.Handler {
	return elevated.Handler{
		Timeout: helperTimeout(LaunchSpec{TimeoutSeconds: maxTimeoutSeconds, WindowTimeoutSeconds: maxTimeoutSeconds}) +
			legacyLoaderReadyTimeout + legacyLoaderMaxDelay + legacyLoaderExitMargin,
		Run: func(ctx context.Context, _ elevated.Peer, payload json.RawMessage) (json.RawMessage, error) {
			var spec LaunchSpec
			if err := json.Unmarshal(payload, &spec); err != nil {
				return nil, errInvalidHelperLaunch
			}
			ctx, cancel := context.WithTimeout(ctx, helperTimeout(spec))
			defer cancel()
			result, err := Launch(ctx, spec)
			if err != nil {
				return nil, err
			}
			response, err := json.Marshal(result)
			if err != nil {
				return nil, errors.New("encode XXMI launch result")
			}
			return response, nil
		},
		Classify: func(err error) (string, string) {
			if errors.Is(err, errInvalidHelperLaunch) {
				return "XXMI_INVALID_LAUNCH", err.Error()
			}
			return ClassifyError(err), err.Error()
		},
	}
}

func LaunchThroughHelper(ctx context.Context, caller elevated.Caller, spec LaunchSpec) (LaunchResult, error) {
	if err := ctx.Err(); err != nil {
		return LaunchResult{}, err
	}
	payload, err := json.Marshal(spec)
	if err != nil {
		return LaunchResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, helperTimeout(spec))
	defer cancel()
	response, err := caller.Call(ctx, HelperOperation, payload)
	if err != nil {
		return LaunchResult{}, err
	}
	var result LaunchResult
	if err := json.Unmarshal(response, &result); err != nil {
		return LaunchResult{}, fmt.Errorf("decode XXMI launch result: %w", err)
	}
	return result, nil
}

// helperTimeout covers the process wait and the window wait, which each take their own full timeout, and the
// legacy loader's waits around them.
func helperTimeout(spec LaunchSpec) time.Duration {
	budget := spec.timeout() + spec.windowTimeout() + helperGrace
	if spec.usesLegacyLoader() {
		budget += legacyLoaderReadyTimeout + legacyLoaderExitTimeout(spec.ModuleDLL)
	}
	return budget
}
