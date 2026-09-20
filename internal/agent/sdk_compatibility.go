package agent

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
)

// Keep the official provider SDKs pinned as protocol compatibility references. The adapters use the shared HTTP client and
// normalized wire model so OpenAI-compatible endpoints can share the same cancellation, proxy, and streaming behavior.
var (
	_ = anthropic.NewClient
	_ = openai.NewClient
)
