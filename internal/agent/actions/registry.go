// Package actions holds the desktop action registry the Nahida Agent drives: the catalog of local
// Nahida Desktop actions the model may call, their input schemas, and their risk and approval
// semantics. The agent runtime keeps one registry and turns confirm-risk plans into approvals.
package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"nahida.live/desktop/internal/menumaker"
	modservice "nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/transfer"
	"nahida.live/desktop/internal/xxmi"
)

// Risk classifies what an action may do to the user's local state.
type Risk string

const (
	RiskRead    Risk = "read"
	RiskWrite   Risk = "write"
	RiskConfirm Risk = "confirm"
)

// Definition is the model-facing description of one action.
type Definition struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	Domain      string         `json:"domain"`
	Risk        Risk           `json:"risk"`
	Scopes      []string       `json:"scopes"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Hint is the compact index entry the system prompt carries.
type Hint struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Risk        Risk   `json:"risk"`
}

// Dependencies are the desktop services the catalog binds to. A nil service keeps its domain out of
// the registry, so a model is never offered an action that cannot run.
type Dependencies struct {
	Mod       *modservice.Mod
	Tools     *tools.Tools
	Settings  *setting.Setting
	Transfer  *transfer.Transfer
	XXMI      *xxmi.XXMI
	MenuMaker *menumaker.MenuMaker
	Input     *platform.Input
	Screen    *platform.Screen
}

// Resolver turns a sandbox root id and relative path into an absolute local path.
type Resolver interface {
	// ResolveExisting resolves a path that must already exist.
	ResolveExisting(rootID, relativePath string) (string, error)
	// ResolveTarget resolves a path that may be missing, such as a generated output file.
	ResolveTarget(rootID, relativePath string) (string, error)
}

type actionContext struct {
	resolver Resolver
	scope    string
}

func (c actionContext) resolve(rootID, relativePath string) (string, error) {
	if c.resolver == nil {
		return "", errors.New("agent sandbox is unavailable")
	}
	return c.resolver.ResolveExisting(rootID, relativePath)
}

func (c actionContext) resolveTarget(rootID, relativePath string) (string, error) {
	if c.resolver == nil {
		return "", errors.New("agent sandbox is unavailable")
	}
	return c.resolver.ResolveTarget(rootID, relativePath)
}

type action struct {
	definition Definition
	execute    func(context.Context, actionContext, json.RawMessage) (any, error)
	validate   func(json.RawMessage) error
	risk       func(json.RawMessage) Risk
	describe   func(actionContext, json.RawMessage) (string, string, error)
}

// Registry holds the actions the agent may run, keyed by action id.
type Registry struct {
	actions map[string]action
}

// Call is one model-supplied action invocation.
type Call struct {
	ActionID  string          `json:"actionId"`
	Arguments json.RawMessage `json:"arguments"`
}

// Proposal is the approval request a confirm-risk action produces.
type Proposal struct {
	ActionID  string
	Arguments json.RawMessage
	Summary   string
	Target    string
	Impact    string
	Kind      string
}

// CapturedImage is an action result whose payload is an image the model should see. The agent
// runtime replaces the bytes with a text placeholder in the persisted tool result and stores them as
// tool images, the same way MCP image content is handled.
type CapturedImage struct {
	Window   platform.WindowInfo `json:"window"`
	Width    int                 `json:"width"`
	Height   int                 `json:"height"`
	Scale    float64             `json:"scale"`
	MIMEType string              `json:"mimeType"`
	PNG      []byte              `json:"-"`
}

// Plan is a prepared action. A non-nil Proposal means the caller must collect user approval before
// running it.
type Plan struct {
	Definition Definition
	Risk       Risk
	Proposal   *Proposal
	run        func(context.Context) (any, error)
}

// Run executes the prepared action.
func (p Plan) Run(ctx context.Context) (any, error) {
	if p.run == nil {
		return nil, errors.New("desktop action plan is empty")
	}
	return p.run(ctx)
}

// NewRegistry builds the catalog for the services that are available.
func NewRegistry(deps Dependencies) *Registry {
	registry := &Registry{actions: make(map[string]action)}
	registerModActions(registry, deps)
	registerSettingsActions(registry, deps)
	registerTransferActions(registry, deps)
	registerXXMIActions(registry, deps)
	registerToolActions(registry, deps)
	registerMenuMakerActions(registry, deps)
	registerInputActions(registry, deps)
	registerScreenActions(registry, deps)
	return registry
}

func (r *Registry) add(action action) {
	if err := r.register(action); err != nil {
		panic(err)
	}
}

func (r *Registry) register(action action) error {
	definition := &action.definition
	if strings.TrimSpace(definition.ID) == "" || strings.TrimSpace(definition.Domain) == "" ||
		strings.TrimSpace(definition.Description) == "" {
		return errors.New("desktop action requires id, domain, and description")
	}
	if definition.Risk != RiskRead && definition.Risk != RiskWrite && definition.Risk != RiskConfirm {
		return fmt.Errorf("desktop action %q has invalid risk %q", definition.ID, definition.Risk)
	}
	if definition.InputSchema["type"] != "object" || definition.InputSchema["additionalProperties"] != false {
		return fmt.Errorf("desktop action %q requires a strict object input schema", definition.ID)
	}
	if action.execute == nil {
		return fmt.Errorf("desktop action %q requires a handler", definition.ID)
	}
	if _, exists := r.actions[definition.ID]; exists {
		return fmt.Errorf("duplicate desktop action %q", definition.ID)
	}
	if definition.Scopes == nil {
		definition.Scopes = []string{"global", "mod"}
	}
	r.actions[definition.ID] = action
	return nil
}

func (r *Registry) Definitions(scope, domain, query string) []Definition {
	domain, query = strings.ToLower(strings.TrimSpace(domain)), strings.ToLower(strings.TrimSpace(query))
	definitions := make([]Definition, 0, len(r.actions))
	for _, action := range r.actions {
		definition := action.definition
		if domain != "" && definition.Domain != domain {
			continue
		}
		if !containsString(definition.Scopes, scope) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(definition.ID+" "+definition.Description), query) {
			continue
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions
}

func (r *Registry) Hints(scope string) []Hint {
	definitions := r.Definitions(scope, "", "")
	hints := make([]Hint, 0, len(definitions))
	for _, definition := range definitions {
		hints = append(hints, Hint{
			ID: definition.ID, Description: definition.Description, Risk: definition.Risk,
		})
	}
	return hints
}

// Prepare validates one call and returns the plan for it, including the approval request when the
// action needs user confirmation.
func (r *Registry) Prepare(scope string, resolver Resolver, call Call) (Plan, error) {
	action, ok := r.actions[call.ActionID]
	if !ok {
		return Plan{}, fmt.Errorf("unregistered desktop action %q", call.ActionID)
	}
	if !containsString(action.definition.Scopes, scope) {
		return Plan{}, fmt.Errorf("desktop action %q is unavailable in %s scope", call.ActionID, scope)
	}
	arguments := call.Arguments
	if len(arguments) == 0 || bytes.Equal(bytes.TrimSpace(arguments), []byte("null")) {
		arguments = json.RawMessage(`{}`)
	}
	var objectValue map[string]any
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.UseNumber()
	if err := decoder.Decode(&objectValue); err != nil {
		return Plan{}, fmt.Errorf("decode desktop action arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Plan{}, errors.New("decode desktop action arguments: multiple values")
	}
	canonical, err := json.Marshal(objectValue)
	if err != nil {
		return Plan{}, err
	}
	if err := validateActionSchema(action.definition.InputSchema, objectValue, "arguments"); err != nil {
		return Plan{}, err
	}
	if action.validate != nil {
		if err := action.validate(canonical); err != nil {
			return Plan{}, err
		}
	}
	actionCtx := actionContext{resolver: resolver, scope: scope}
	plan := Plan{
		Definition: action.definition,
		Risk:       action.definition.Risk,
		run: func(ctx context.Context) (any, error) {
			return action.execute(ctx, actionCtx, canonical)
		},
	}
	if action.risk != nil {
		plan.Risk = action.risk(canonical)
	}
	if plan.Risk != RiskConfirm {
		return plan, nil
	}
	summary, target := action.definition.Description, ""
	if action.describe != nil {
		summary, target, err = action.describe(actionCtx, canonical)
		if err != nil {
			return Plan{}, err
		}
	}
	plan.Proposal = &Proposal{
		ActionID:  call.ActionID,
		Arguments: canonical,
		Summary:   summary,
		Target:    target,
		Impact:    "This action can make broad or difficult-to-reverse local changes.",
		Kind:      "desktop",
	}
	return plan, nil
}

// Execute prepares and runs one action. Callers must have approval for confirm-risk actions.
func (r *Registry) Execute(
	ctx context.Context,
	scope string,
	resolver Resolver,
	actionID string,
	arguments json.RawMessage,
) (any, error) {
	plan, err := r.Prepare(scope, resolver, Call{ActionID: actionID, Arguments: arguments})
	if err != nil {
		return nil, err
	}
	return plan.Run(ctx)
}
