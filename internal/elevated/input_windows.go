//go:build windows

package elevated

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"nahida.live/desktop/internal/platform"
)

// inputTimeout covers the longest valid key request plus the focus wait and restore around it.
const inputTimeout = platform.MaxKeySendDuration + 5*time.Second

var errInvalidInputRequest = errors.New("invalid input request")

func InputHandler(sendKeys func(context.Context, platform.KeyRequest) (platform.KeyResult, error)) Handler {
	return Handler{
		Timeout: inputTimeout,
		Run: func(ctx context.Context, _ Peer, payload json.RawMessage) (json.RawMessage, error) {
			var request platform.KeyRequest
			if err := json.Unmarshal(payload, &request); err != nil {
				return nil, errInvalidInputRequest
			}
			result, err := sendKeys(ctx, request)
			if err != nil {
				return nil, err
			}
			response, err := json.Marshal(result)
			if err != nil {
				return nil, errors.New("encode input result")
			}
			return response, nil
		},
		Classify: func(err error) (string, string) {
			if errors.Is(err, errInvalidInputRequest) {
				return "", err.Error()
			}
			return platform.ClassifyInputError(err)
		},
	}
}

// SendKeys rebuilds the input sentinel from the code the helper sent, so errors.Is resolves as it
// does for a local input failure.
func (c *Client) SendKeys(ctx context.Context, request platform.KeyRequest) (platform.KeyResult, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return platform.KeyResult{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, inputTimeout+10*time.Second)
	defer cancel()
	response, err := c.Call(callCtx, OperationInputKeys, payload)
	if remote, ok := errors.AsType[*remoteError](err); ok {
		if sentinel, ok := platform.InputErrorFromCode(remote.code); ok {
			return platform.KeyResult{}, fmt.Errorf("%w: elevated helper: %s", sentinel, remote.message)
		}
	}
	if err != nil {
		return platform.KeyResult{}, err
	}
	var result platform.KeyResult
	if err := json.Unmarshal(response, &result); err != nil {
		return platform.KeyResult{}, fmt.Errorf("decode elevated input result: %w", err)
	}
	return result, nil
}
