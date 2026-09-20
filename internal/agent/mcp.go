package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

type MCPEntryInput struct {
	Name         string `json:"name"`
	Value        string `json:"value,omitempty"`
	Secret       bool   `json:"secret"`
	SecretAction string `json:"secretAction,omitempty"`
}

type MCPServerInput struct {
	ID               string          `json:"id,omitempty"`
	Name             string          `json:"name"`
	Transport        string          `json:"transport"`
	Executable       string          `json:"executable,omitempty"`
	Arguments        []string        `json:"arguments,omitempty"`
	WorkingDirectory string          `json:"workingDirectory,omitempty"`
	Endpoint         string          `json:"endpoint,omitempty"`
	Entries          []MCPEntryInput `json:"entries,omitempty"`
	Enabled          bool            `json:"enabled"`
}

type MCPEntryView struct {
	Name       string `json:"name"`
	Secret     bool   `json:"secret"`
	Configured bool   `json:"configured"`
	Value      string `json:"value,omitempty"`
}

type MCPServerView struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Transport        string         `json:"transport"`
	Executable       string         `json:"executable,omitempty"`
	Arguments        []string       `json:"arguments,omitempty"`
	WorkingDirectory string         `json:"workingDirectory,omitempty"`
	Endpoint         string         `json:"endpoint,omitempty"`
	Entries          []MCPEntryView `json:"entries,omitempty"`
	Enabled          bool           `json:"enabled"`
	Status           string         `json:"status"`
	Error            string         `json:"error,omitempty"`
	ToolCount        int            `json:"toolCount,omitempty"`
}

type mcpPublicConfig struct {
	Executable       string          `json:"executable,omitempty"`
	Arguments        []string        `json:"arguments,omitempty"`
	WorkingDirectory string          `json:"workingDirectory,omitempty"`
	Endpoint         string          `json:"endpoint,omitempty"`
	Entries          []MCPEntryInput `json:"entries,omitempty"`
}

type mcpRuntime struct {
	sessions []*mcp.ClientSession
	tools    map[string]mcpRuntimeTool
	servers  map[string]*mcp.ClientSession
}

type mcpRuntimeTool struct {
	session *mcp.ClientSession
	name    string
}

func (s *Service) openMCPRuntime(ctx context.Context, roots []SandboxRoot) (*mcpRuntime, []ToolDefinition) {
	runtime := &mcpRuntime{tools: make(map[string]mcpRuntimeTool), servers: make(map[string]*mcp.ClientSession)}
	client, err := s.dbClient()
	if err != nil {
		s.reportMCPError(err, "database", db.AgentMCPServerRow{})
		return runtime, nil
	}
	rows, err := client.AgentMCPServers.List(ctx)
	if err != nil {
		s.reportMCPError(err, "list-servers", db.AgentMCPServerRow{})
		return runtime, nil
	}
	definitions := make([]ToolDefinition, 0)
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		session, connectErr := s.connectMCP(ctx, row, roots)
		if connectErr != nil {
			s.reportMCPError(connectErr, "connect", row)
			continue
		}
		serverName := mcpName(row.Name)
		if serverName == "" {
			_ = session.Close()
			s.reportMCPError(errors.New("server name normalizes to an empty identifier"), "register-server", row)
			continue
		}
		if _, collision := runtime.servers[serverName]; collision {
			_ = session.Close()
			s.reportMCPError(
				fmt.Errorf("normalized server name %q is already registered", serverName),
				"register-server",
				row,
			)
			continue
		}
		runtime.sessions = append(runtime.sessions, session)
		runtime.servers[serverName] = session
		cursor := ""
		for {
			result, listErr := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
			if listErr != nil {
				s.reportMCPError(listErr, "list-tools", row)
				break
			}
			for _, tool := range result.Tools {
				publicName := publicMCPToolName(row.Name, tool.Name)
				if _, collision := runtime.tools[publicName]; collision {
					s.reportMCPError(
						fmt.Errorf("public tool name %q is already registered", publicName),
						"register-tool",
						row,
					)
					continue
				}
				schema := map[string]any{"type": "object"}
				encoded, _ := json.Marshal(tool.InputSchema)
				_ = json.Unmarshal(encoded, &schema)
				runtime.tools[publicName] = mcpRuntimeTool{session: session, name: tool.Name}
				definitions = append(definitions, ToolDefinition{
					Name: publicName, Description: tool.Description, InputSchema: schema,
				})
			}
			if result.NextCursor == "" {
				break
			}
			cursor = result.NextCursor
		}
	}
	if len(runtime.sessions) > 0 {
		definitions = append(definitions, mcpBuiltInToolDefinitions()...)
	}
	return runtime, definitions
}

func (s *Service) reportMCPError(err error, stage string, row db.AgentMCPServerRow) {
	fields := map[string]any{"serverId": row.ID, "serverName": row.Name, "transport": row.Transport}
	_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "mcp-runtime", Stage: stage, Fields: fields,
	})
}

func mcpBuiltInToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{Name: "list_mcp_resources", Description: "List resources exposed by enabled MCP servers.",
			InputSchema: objectSchema(map[string]any{"server": map[string]any{"type": "string"}})},
		{Name: "read_mcp_resource", Description: "Read one MCP resource by server and URI.",
			InputSchema: objectSchema(map[string]any{"server": map[string]any{"type": "string"},
				"uri": map[string]any{"type": "string"}}, "server", "uri")},
		{Name: "list_mcp_prompts", Description: "List prompts exposed by enabled MCP servers.",
			InputSchema: objectSchema(map[string]any{"server": map[string]any{"type": "string"}})},
		{Name: "get_mcp_prompt", Description: "Render one MCP prompt with string arguments.",
			InputSchema: objectSchema(map[string]any{"server": map[string]any{"type": "string"},
				"name": map[string]any{"type": "string"}, "arguments": map[string]any{"type": "object"}},
				"server", "name")},
	}
}

func (r *mcpRuntime) Close() error {
	var result error
	for _, session := range r.sessions {
		result = errors.Join(result, session.Close())
	}
	return result
}

func (r *mcpRuntime) Call(ctx context.Context, call ToolCall) (*mcp.CallToolResult, error) {
	tool, ok := r.tools[call.Name]
	if !ok {
		return nil, fmt.Errorf("unknown MCP tool %q", call.Name)
	}
	var arguments map[string]any
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return nil, err
	}
	result, err := tool.session.CallTool(ctx, &mcp.CallToolParams{Name: tool.name, Arguments: arguments})
	if err != nil {
		return nil, err
	}
	if result.NeedsInput() {
		return result, errors.New("MCP elicitation is not supported by Nahida Agent")
	}
	return result, nil
}

func (r *mcpRuntime) ListResources(ctx context.Context, serverName string) (map[string]any, error) {
	sessions, err := r.selectedServers(serverName)
	if err != nil {
		return nil, err
	}
	output := make(map[string]any, len(sessions))
	for name, session := range sessions {
		resources := make([]*mcp.Resource, 0)
		cursor := ""
		for {
			result, err := session.ListResources(ctx, &mcp.ListResourcesParams{Cursor: cursor})
			if err != nil {
				return nil, err
			}
			resources = append(resources, result.Resources...)
			if result.NextCursor == "" {
				break
			}
			cursor = result.NextCursor
		}
		output[name] = resources
	}
	return output, nil
}

func (r *mcpRuntime) ReadResource(ctx context.Context, serverName, uri string) (any, error) {
	session := r.servers[mcpName(serverName)]
	if session == nil {
		return nil, fmt.Errorf("unknown MCP server %q", serverName)
	}
	return session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
}

func (r *mcpRuntime) ListPrompts(ctx context.Context, serverName string) (map[string]any, error) {
	sessions, err := r.selectedServers(serverName)
	if err != nil {
		return nil, err
	}
	output := make(map[string]any, len(sessions))
	for name, session := range sessions {
		prompts := make([]*mcp.Prompt, 0)
		cursor := ""
		for {
			result, err := session.ListPrompts(ctx, &mcp.ListPromptsParams{Cursor: cursor})
			if err != nil {
				return nil, err
			}
			prompts = append(prompts, result.Prompts...)
			if result.NextCursor == "" {
				break
			}
			cursor = result.NextCursor
		}
		output[name] = prompts
	}
	return output, nil
}

func (r *mcpRuntime) GetPrompt(
	ctx context.Context,
	serverName, name string,
	arguments map[string]string,
) (any, error) {
	session := r.servers[mcpName(serverName)]
	if session == nil {
		return nil, fmt.Errorf("unknown MCP server %q", serverName)
	}
	return session.GetPrompt(ctx, &mcp.GetPromptParams{Name: name, Arguments: arguments})
}

func (r *mcpRuntime) selectedServers(name string) (map[string]*mcp.ClientSession, error) {
	if strings.TrimSpace(name) == "" {
		return r.servers, nil
	}
	key := mcpName(name)
	session := r.servers[key]
	if session == nil {
		return nil, fmt.Errorf("unknown MCP server %q", name)
	}
	return map[string]*mcp.ClientSession{key: session}, nil
}

func mcpName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}

// mcpToolNameLimit is the function-name limit OpenAI-compatible providers enforce on a model
// request. One built-in Blender tool needs a longer name than that, so an over-long public name is
// shortened instead of being dropped from the tool list.
const mcpToolNameLimit = 64

// publicMCPToolName namespaces one MCP tool for the model. A name over the provider limit keeps a
// readable prefix and ends in a hash of the full name, so a shortened name stays unique and stable
// across turns, restarts, and provider switches.
func publicMCPToolName(server, tool string) string {
	name := "mcp__" + mcpName(server) + "__" + mcpName(tool)
	if len(name) <= mcpToolNameLimit {
		return name
	}
	hash := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(hash[:4])
	return name[:mcpToolNameLimit-len(suffix)-1] + "_" + suffix
}

func (s *Service) ListMCPServers(ctx context.Context) ([]MCPServerView, error) {
	client, err := s.dbClient()
	if err != nil {
		return nil, err
	}
	rows, err := client.AgentMCPServers.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MCPServerView, 0, len(rows))
	for _, row := range rows {
		view, err := s.mcpView(row)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *Service) UpsertMCPServer(ctx context.Context, input MCPServerInput) (MCPServerView, error) {
	if err := validateMCPInput(input); err != nil {
		return MCPServerView{}, err
	}
	client, err := s.dbClient()
	if err != nil {
		return MCPServerView{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	row := db.AgentMCPServerRow{ID: input.ID, Name: strings.TrimSpace(input.Name), Transport: input.Transport,
		Enabled: input.Enabled, CreatedAt: now, UpdatedAt: now}
	if row.ID == "" {
		row.ID = uuid.NewString()
	} else if existing, getErr := client.AgentMCPServers.Get(ctx, row.ID); getErr != nil {
		return MCPServerView{}, getErr
	} else if existing != nil {
		row.CreatedAt = existing.CreatedAt
		row.SecretBlob = existing.SecretBlob
	}
	public := mcpPublicConfig{Executable: input.Executable, Arguments: input.Arguments,
		WorkingDirectory: input.WorkingDirectory, Endpoint: input.Endpoint}
	secrets := map[string]string{}
	if row.SecretBlob != "" {
		plain, decryptErr := s.crypto.DecryptString(row.SecretBlob)
		if decryptErr != nil {
			return MCPServerView{}, fmt.Errorf("decrypt MCP secrets: %w", decryptErr)
		}
		_ = json.Unmarshal([]byte(plain), &secrets)
	}
	for _, entry := range input.Entries {
		entry.Name = strings.TrimSpace(entry.Name)
		if entry.Secret {
			switch entry.SecretAction {
			case "", "keep":
			case "replace":
				if entry.Value == "" {
					return MCPServerView{}, fmt.Errorf("secret value is required for %s", entry.Name)
				}
				secrets[entry.Name] = entry.Value
			case "clear":
				delete(secrets, entry.Name)
			default:
				return MCPServerView{}, fmt.Errorf("invalid secret action for %s", entry.Name)
			}
			public.Entries = append(public.Entries, MCPEntryInput{Name: entry.Name, Secret: true})
			continue
		}
		public.Entries = append(public.Entries, MCPEntryInput{Name: entry.Name, Value: entry.Value})
	}
	publicData, _ := json.Marshal(public)
	row.PublicConfig = string(publicData)
	if len(secrets) > 0 {
		secretData, _ := json.Marshal(secrets)
		row.SecretBlob, err = s.crypto.EncryptString(string(secretData))
		if err != nil {
			return MCPServerView{}, err
		}
	} else {
		row.SecretBlob = ""
	}
	if err := client.AgentMCPServers.Upsert(ctx, row); err != nil {
		return MCPServerView{}, err
	}
	return s.mcpView(row)
}

func (s *Service) DeleteMCPServer(ctx context.Context, id string) error {
	client, err := s.dbClient()
	if err != nil {
		return err
	}
	return client.AgentMCPServers.Delete(ctx, id)
}

func (s *Service) TestMCPServer(ctx context.Context, id string) (MCPServerView, error) {
	client, err := s.dbClient()
	if err != nil {
		return MCPServerView{}, err
	}
	row, err := client.AgentMCPServers.Get(ctx, id)
	if err != nil || row == nil {
		return MCPServerView{}, errors.New("MCP server not found")
	}
	view, err := s.mcpView(*row)
	if err != nil {
		return MCPServerView{}, err
	}
	testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	session, err := s.connectMCP(testCtx, *row, nil)
	if err != nil {
		view.Status, view.Error = "error", err.Error()
		return view, nil
	}
	defer func() { _ = session.Close() }()
	cursor := ""
	for {
		result, listErr := session.ListTools(testCtx, &mcp.ListToolsParams{Cursor: cursor})
		if listErr != nil {
			view.Status, view.Error = "error", listErr.Error()
			return view, nil
		}
		view.ToolCount += len(result.Tools)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	view.Status = "connected"
	return view, nil
}

func (s *Service) mcpView(row db.AgentMCPServerRow) (MCPServerView, error) {
	var config mcpPublicConfig
	if err := json.Unmarshal([]byte(row.PublicConfig), &config); err != nil {
		return MCPServerView{}, err
	}
	configured := map[string]bool{}
	if row.SecretBlob != "" {
		plain, err := s.crypto.DecryptString(row.SecretBlob)
		if err != nil {
			return MCPServerView{}, err
		}
		var secrets map[string]string
		_ = json.Unmarshal([]byte(plain), &secrets)
		for name, value := range secrets {
			configured[name] = value != ""
		}
	}
	view := MCPServerView{ID: row.ID, Name: row.Name, Transport: row.Transport, Executable: config.Executable,
		Arguments: config.Arguments, WorkingDirectory: config.WorkingDirectory, Endpoint: config.Endpoint,
		Enabled: row.Enabled, Status: "disconnected"}
	for _, entry := range config.Entries {
		view.Entries = append(view.Entries, MCPEntryView{Name: entry.Name, Secret: entry.Secret,
			Configured: !entry.Secret || configured[entry.Name], Value: entry.Value})
	}
	return view, nil
}

func (s *Service) connectMCP(
	ctx context.Context,
	row db.AgentMCPServerRow,
	roots []SandboxRoot,
) (*mcp.ClientSession, error) {
	var config mcpPublicConfig
	if err := json.Unmarshal([]byte(row.PublicConfig), &config); err != nil {
		return nil, err
	}
	secrets := map[string]string{}
	if row.SecretBlob != "" {
		plain, err := s.crypto.DecryptString(row.SecretBlob)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(plain), &secrets)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "nahida-agent", Title: "Nahida Agent", Version: "1"},
		&mcp.ClientOptions{})
	for _, root := range roots {
		//nolint:staticcheck // Product contract requires MCP roots while the SDK still supports them.
		client.AddRoots(
			//nolint:staticcheck // See AddRoots rationale above.
			&mcp.Root{Name: root.Name, URI: (&url.URL{Scheme: "file", Path: filepathSlash(root.Path)}).String()},
		)
	}
	var transport mcp.Transport
	switch row.Transport {
	case "stdio":
		command := exec.CommandContext(ctx, config.Executable, config.Arguments...)
		command.Dir = config.WorkingDirectory
		command.Env = minimalEnvironment(config.Entries, secrets)
		transport = &mcp.CommandTransport{Command: command, TerminateDuration: 3 * time.Second}
	case "streamable-http":
		httpClient := *s.http
		httpClient.Transport = &mcpHeaderTransport{base: s.http.Transport, entries: config.Entries, secrets: secrets}
		transport = &mcp.StreamableClientTransport{Endpoint: config.Endpoint, HTTPClient: &httpClient, MaxRetries: 3}
	case mcpTransportBlender:
		// Built-in compatibility layer: the Blender add-on speaks a raw TCP socket protocol, not
		// MCP, so Nahida hosts the MCP server itself and translates each tool call into a
		// Blender-side Python program. No external bridge executable is required.
		session, sessionErr := newBlenderToolSession(ctx)
		if sessionErr != nil {
			return nil, sessionErr
		}
		return session.client, nil
	default:
		return nil, fmt.Errorf("unsupported MCP transport %q", row.Transport)
	}
	return client.Connect(ctx, transport, nil)
}

type mcpHeaderTransport struct {
	base    http.RoundTripper
	entries []MCPEntryInput
	secrets map[string]string
}

func (t *mcpHeaderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	for _, entry := range t.entries {
		value := entry.Value
		if entry.Secret {
			value = t.secrets[entry.Name]
		}
		if value != "" {
			clone.Header.Set(entry.Name, value)
		}
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func validateMCPInput(input MCPServerInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("MCP server name is required")
	}
	switch input.Transport {
	case "stdio":
		if strings.TrimSpace(input.Executable) == "" {
			return errors.New("MCP executable is required")
		}
	case "streamable-http":
		endpoint, err := url.Parse(input.Endpoint)
		if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
			return errors.New("MCP endpoint must be an HTTP(S) URL")
		}
	case mcpTransportBlender:
		// The built-in Blender transport is configured entirely by Nahida; the optional host and
		// port entries only retarget which Blender instance it talks to.
	default:
		return fmt.Errorf("unsupported MCP transport %q", input.Transport)
	}
	seen := map[string]bool{}
	for _, entry := range input.Entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" || seen[strings.ToLower(name)] {
			return errors.New("MCP entry names must be non-empty and unique")
		}
		seen[strings.ToLower(name)] = true
	}
	return nil
}

func minimalEnvironment(entries []MCPEntryInput, secrets map[string]string) []string {
	environment := []string{"PATH=" + os.Getenv("PATH"), "SYSTEMROOT=" + os.Getenv("SYSTEMROOT"),
		"TEMP=" + os.Getenv("TEMP"), "TMP=" + os.Getenv("TMP")}
	for _, entry := range entries {
		value := entry.Value
		if entry.Secret {
			value = secrets[entry.Name]
		}
		if value != "" {
			environment = append(environment, entry.Name+"="+value)
		}
	}
	return environment
}

func filepathSlash(path string) string {
	return "/" + strings.TrimPrefix(strings.ReplaceAll(path, `\`, "/"), "/")
}
