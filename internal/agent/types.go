package agent

import (
	"context"
	"encoding/json"
)

type AgentScope struct {
	Type    string `json:"type"`
	ModPath string `json:"modPath,omitempty"`
	ModName string `json:"modName,omitempty"`
}

type AgentSessionSummary struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Scope     AgentScope `json:"scope"`
	UpdatedAt string     `json:"updatedAt"`
	Running   bool       `json:"running"`
}

type AgentChatEntry struct {
	Sequence     int64           `json:"sequence"`
	TurnID       string          `json:"turnId"`
	Type         string          `json:"type"`
	Role         string          `json:"role,omitempty"`
	Text         string          `json:"text,omitempty"`
	Reasoning    string          `json:"reasoning,omitempty"`
	Images       []AgentImage    `json:"images,omitempty"`
	ToolName     string          `json:"toolName,omitempty"`
	ToolCallID   string          `json:"toolCallId,omitempty"`
	Arguments    json.RawMessage `json:"arguments,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
	ChangedFiles []string        `json:"changedFiles,omitempty"`
	Error        string          `json:"error,omitempty"`
	Approval     *AgentApproval  `json:"approval,omitempty"`
	Reverted     bool            `json:"reverted,omitempty"`
	CreatedAt    string          `json:"createdAt"`
}

// AgentSessionRevert is a staged revert: every event at or after BoundarySequence is hidden until the
// next send commits the deletion.
type AgentSessionRevert struct {
	BoundarySequence int64  `json:"boundarySequence"`
	BoundaryTurnID   string `json:"boundaryTurnId"`
	RevertedCount    int    `json:"revertedCount"`
	CreatedAt        string `json:"createdAt"`
}

type AgentSessionSnapshot struct {
	Summary           AgentSessionSummary `json:"summary"`
	Entries           []AgentChatEntry    `json:"entries"`
	Roots             []SandboxRoot       `json:"roots"`
	Approvals         []AgentApproval     `json:"approvals"`
	Revert            *AgentSessionRevert `json:"revert,omitempty"`
	ContextUsage      *AgentContextUsage  `json:"contextUsage,omitempty"`
	SupportsImages    bool                `json:"supportsImages"`
	UnavailableReason string              `json:"unavailableReason,omitempty"`
}

// AgentContextUsage is the context occupancy of one session. ProjectedTokens is what the next
// request's prompt would cost; PressureTokens is the provider-reported prompt size and is present
// only while a matching usage anchor exists. The breakdown fields are the heuristic composition of
// the current surface, so their sum need not equal ProjectedTokens.
type AgentContextUsage struct {
	PressureTokens  int64 `json:"pressureTokens,omitempty"`
	ProjectedTokens int64 `json:"projectedTokens"`
	ContextWindow   int   `json:"contextWindow"`
	SystemTokens    int   `json:"systemTokens"`
	ToolsTokens     int   `json:"toolsTokens"`
	MessageTokens   int   `json:"messageTokens"`
}

// AgentImage references one stored image. Chat attachments and images returned by tools are
// written below the app data directory; Path addresses the stored file for the agent runtime and
// Src is the absolute path the renderer loads with the local-file protocol.
type AgentImage struct {
	Name     string `json:"name,omitempty"`
	MIMEType string `json:"mimeType"`
	Bytes    int    `json:"bytes"`
	Path     string `json:"path"`
	Src      string `json:"src"`
}

// AgentImageInput is one renderer-supplied attachment; Data is base64-encoded image bytes.
type AgentImageInput struct {
	Name     string `json:"name,omitempty"`
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

// MessageImage is one image sent to a model provider; Data is base64-encoded image bytes. Bytes
// carries the decoded size for estimation replays that must not read payloads from disk.
type MessageImage struct {
	MIMEType string `json:"mimeType"`
	Bytes    int    `json:"bytes,omitempty"`
	Data     string `json:"data,omitempty"`
}

type AgentApproval struct {
	ID          string          `json:"id"`
	SessionID   string          `json:"sessionId"`
	TurnID      string          `json:"turnId"`
	ToolCallID  string          `json:"toolCallId"`
	ActionID    string          `json:"actionId"`
	Arguments   json.RawMessage `json:"arguments"`
	Summary     string          `json:"summary"`
	Target      string          `json:"target,omitempty"`
	Impact      string          `json:"impact"`
	Status      string          `json:"status"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   string          `json:"createdAt"`
	DecidedAt   string          `json:"decidedAt,omitempty"`
	CompletedAt string          `json:"completedAt,omitempty"`
}

type AgentHeaderInput struct {
	Name         string `json:"name"`
	Value        string `json:"value,omitempty"`
	Secret       bool   `json:"secret"`
	SecretAction string `json:"secretAction,omitempty"`
}

type AgentHeaderView struct {
	Name       string `json:"name"`
	Value      string `json:"value,omitempty"`
	Secret     bool   `json:"secret"`
	Configured bool   `json:"configured"`
}

type AgentSettingsView struct {
	Provider          string              `json:"provider"`
	Protocol          string              `json:"protocol"`
	Endpoint          string              `json:"endpoint"`
	Model             string              `json:"model"`
	ContextWindowSize int                 `json:"contextWindowSize"`
	MaxOutputTokens   int                 `json:"maxOutputTokens"`
	Reasoning         string              `json:"reasoning"`
	SupportsImages    bool                `json:"supportsImages"`
	Headers           []AgentHeaderView   `json:"headers,omitempty"`
	Credential        AgentCredentialView `json:"credential"`
}

// AgentCredentialView summarizes the stored credential of one provider without exposing it.
type AgentCredentialView struct {
	Kind         string `json:"kind"`
	AccountLabel string `json:"accountLabel,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
}

// AgentProviderView is one provider entry of the agent settings screen. Models are the shipped
// catalog entries the provider's current credential can use.
type AgentProviderView struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Endpoint        string              `json:"endpoint"`
	DefaultProtocol string              `json:"defaultProtocol"`
	SupportsAPIKey  bool                `json:"supportsApiKey"`
	SupportsOAuth   bool                `json:"supportsOAuth"`
	Custom          bool                `json:"custom"`
	Credential      AgentCredentialView `json:"credential"`
	Models          []CatalogModel      `json:"models,omitempty"`
}

// AgentCatalogStatus reports where the model catalog came from and when it was published.
type AgentCatalogStatus struct {
	Source    string `json:"source"`
	UpdatedAt string `json:"updatedAt"`
}

// AgentProviderCatalogView is the provider list together with the catalog it was built from.
type AgentProviderCatalogView struct {
	Catalog   AgentCatalogStatus  `json:"catalog"`
	Providers []AgentProviderView `json:"providers"`
}

// AgentProviderTestView reports the model that answered a connection test.
type AgentProviderTestView struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Endpoint string `json:"endpoint"`
	Latency  int64  `json:"latencyMs"`
}

// AgentLoginView carries the URL of a pending provider login; the provider opens it in the
// browser and the renderer shows it as a fallback link.
type AgentLoginView struct {
	Provider string `json:"provider"`
	URL      string `json:"url"`
}

type UpdateAgentSettingsInput struct {
	Provider          string             `json:"provider"`
	Protocol          string             `json:"protocol"`
	Endpoint          string             `json:"endpoint"`
	Model             string             `json:"model"`
	ContextWindowSize int                `json:"contextWindowSize"`
	MaxOutputTokens   int                `json:"maxOutputTokens"`
	Reasoning         string             `json:"reasoning"`
	SupportsImages    bool               `json:"supportsImages"`
	Headers           []AgentHeaderInput `json:"headers,omitempty"`
	// APIKey is the key typed into the settings form; TestProvider tries it without saving it.
	APIKey string `json:"apiKey,omitempty"`
}

// UpdateAgentCredentialInput sets or removes the API key of one provider.
type UpdateAgentCredentialInput struct {
	ProviderID string `json:"providerId"`
	Action     string `json:"action"`
	APIKey     string `json:"apiKey,omitempty"`
}

type AgentStreamEvent struct {
	SessionID string `json:"sessionId"`
	RunID     string `json:"runId"`
	Sequence  int64  `json:"sequence"`
	Type      string `json:"type"`
	Payload   any    `json:"payload,omitempty"`
}

type SendResult struct {
	RunID string `json:"runId"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type Message struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Reasoning  string         `json:"reasoning,omitempty"`
	Images     []MessageImage `json:"images,omitempty"`
	ToolCalls  []ToolCall     `json:"toolCalls,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
}

type ModelRequest struct {
	System          string
	Messages        []Message
	Tools           []ToolDefinition
	MaxOutputTokens int
	Reasoning       string
}

type ModelResponse struct {
	Text         string
	Reasoning    string
	ToolCalls    []ToolCall
	InputTokens  int64
	OutputTokens int64
}

type ModelAdapter interface {
	Complete(ctx context.Context, request ModelRequest, emit func(kind, delta string)) (ModelResponse, error)
}
