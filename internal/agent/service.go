package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	agentactions "nahida.live/desktop/internal/agent/actions"
	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	modservice "nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/transfer"
	"nahida.live/desktop/internal/xxmi"
)

const (
	maxToolRounds                   = 32
	modelTimeout                    = 5 * time.Minute
	persistenceTimeout              = 10 * time.Second
	maxToolOutput                   = 64 << 10
	minimumOutputReserve            = 4096
	interruptedApprovalErrorMessage = "application restarted while action was executing"
	skippedToolErrorMessage         = "tool call skipped while awaiting approval"
	interruptedToolErrorMessage     = "tool call interrupted before a durable result was recorded"
)

type Options struct {
	HTTP      *http.Client
	Remote    *infra.Client
	Crypto    *platform.Crypto
	Tools     *tools.Tools
	Mod       *modservice.Mod
	Setting   *setting.Setting
	Transfer  *transfer.Transfer
	XXMI      *xxmi.XXMI
	Log       *infra.Log
	EventEmit func(string, ...any)
	Shell     *platform.Shell
	Input     *platform.Input
	Screen    *platform.Screen
}

type Service struct {
	mu               sync.Mutex
	client           *db.Client
	appData          *appdata.Store
	http             *http.Client
	remote           *infra.Client
	crypto           *platform.Crypto
	emitEvent        func(string, ...any)
	shell            *platform.Shell
	settings         *setting.Setting
	skills           *skillCatalog
	actions          *agentactions.Registry
	log              *infra.Log
	oauth            providerOAuthConfig
	loginMu          sync.Mutex
	login            *providerLogin
	catalogMu        sync.Mutex
	catalogFetchedAt time.Time
	// credentialMu serializes credential read-modify-write sequences so a token refresh cannot
	// overwrite a credential saved at the same moment.
	credentialMu sync.Mutex
	// refreshMu serializes token refreshes: a rotated refresh token cannot be redeemed twice.
	refreshMu sync.Mutex
	workers   map[string]*sessionWorker
	sequences sync.Map
	// sessionLocks serializes revert staging, run lifecycle transitions, and stable share snapshots.
	// A concurrent send cannot read a stale revert marker or delete a turn it just appended.
	sessionLocks sync.Map
	global       chan struct{}
	stop         chan struct{}
	runCtx       context.Context
	cancelRun    context.CancelFunc
	closing      atomic.Bool
	wg           sync.WaitGroup
}

type queuedRun struct {
	id            string
	text          string
	fallbackTitle string
	resume        bool
	approvalID    string
	result        chan error
}

type sessionWorker struct {
	queue  chan queuedRun
	mu     sync.Mutex
	active string
	cancel context.CancelFunc
}

func New(options Options) *Service {
	httpClient := options.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	crypto := options.Crypto
	if crypto == nil {
		crypto = platform.NewCrypto()
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	return &Service{
		http:      httpClient,
		remote:    options.Remote,
		crypto:    crypto,
		log:       options.Log,
		emitEvent: options.EventEmit,
		shell:     options.Shell,
		settings:  options.Setting,
		actions: agentactions.NewRegistry(agentactions.Dependencies{
			Mod: options.Mod, Tools: options.Tools, Settings: options.Setting, Transfer: options.Transfer,
			XXMI: options.XXMI, Input: options.Input, Screen: options.Screen,
		}),
		workers:   make(map[string]*sessionWorker),
		oauth:     defaultOpenAIOAuth,
		global:    make(chan struct{}, 2),
		stop:      make(chan struct{}),
		runCtx:    runCtx,
		cancelRun: cancelRun,
	}
}

//wails:ignore
func (s *Service) UseClient(ctx context.Context, client *db.Client) error {
	if client == nil {
		return errors.New("agent database is unavailable")
	}
	s.mu.Lock()
	s.client = client
	s.mu.Unlock()
	// An install upgrading from the single-provider build still keeps its key in the legacy row,
	// so move it before any read of the credential store.
	s.credentialMu.Lock()
	err := s.migrateLegacyAPIKey(ctx, client)
	s.credentialMu.Unlock()
	if err != nil {
		return fmt.Errorf("migrate agent API key: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := client.AgentApprovals.SealExecuting(ctx, now); err != nil {
		return fmt.Errorf("seal interrupted agent approvals: %w", err)
	}
	if err := s.repairInterruptedApprovals(ctx, client); err != nil {
		return fmt.Errorf("repair interrupted agent approvals: %w", err)
	}
	return client.AgentEvents.SealInterrupted(ctx, now)
}

func (s *Service) repairInterruptedApprovals(ctx context.Context, client *db.Client) error {
	rows, err := client.AgentApprovals.ListStatus(ctx, "failed")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Error != interruptedApprovalErrorMessage {
			continue
		}
		events, err := client.AgentEvents.List(ctx, row.SessionID)
		if err != nil {
			return err
		}
		hasToolEnd, hasApprovalUpdate := false, false
		for _, event := range events {
			if event.TurnID != row.TurnID {
				continue
			}
			switch event.EventType {
			case "tool/end":
				var payload struct {
					ToolCallID string `json:"toolCallId"`
				}
				if json.Unmarshal([]byte(event.Payload), &payload) == nil && payload.ToolCallID == row.ToolCallID {
					hasToolEnd = true
				}
			case "approval/updated":
				var approval AgentApproval
				if json.Unmarshal([]byte(event.Payload), &approval) == nil && approval.ID == row.ID {
					hasApprovalUpdate = true
				}
			}
		}
		if !hasToolEnd {
			payload := map[string]any{
				"toolName":   approvalToolName(row.ActionID),
				"toolCallId": row.ToolCallID,
				"result":     map[string]any{"completed": false},
				"error":      interruptedApprovalErrorMessage,
			}
			if _, err := s.appendEvent(ctx, db.AgentEventRow{
				SessionID: row.SessionID, TurnID: row.TurnID, EventType: "tool/end",
			}, payload); err != nil {
				return err
			}
		}
		if !hasApprovalUpdate {
			if _, err := s.appendEvent(ctx, db.AgentEventRow{
				SessionID: row.SessionID, TurnID: row.TurnID, EventType: "approval/updated",
			}, agentApproval(row)); err != nil {
				return err
			}
		}
	}
	return nil
}

//wails:ignore
func (s *Service) UseAppData(store *appdata.Store) error {
	if store == nil {
		return errors.New("agent app data is unavailable")
	}
	path, err := store.EnsureDir(filepath.Join("agent", "skills"))
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.appData = store
	s.skills = newSkillCatalog(path)
	s.skills.Reload()
	s.mu.Unlock()
	s.loadCatalogCache()
	return nil
}

func (s *Service) OpenSession(ctx context.Context, scope AgentScope) (AgentSessionSummary, error) {
	validated, _, err := s.resolveScope(ctx, scope)
	if err != nil {
		return AgentSessionSummary{}, err
	}
	client, err := s.dbClient()
	if err != nil {
		return AgentSessionSummary{}, err
	}
	row, err := client.AgentSessions.FindLatestScope(ctx, validated.Type, validated.ModPath)
	if err != nil {
		return AgentSessionSummary{}, err
	}
	if row == nil {
		return s.CreateSession(ctx, validated)
	}
	return s.sessionSummary(*row), nil
}

func (s *Service) CreateSession(ctx context.Context, scope AgentScope) (AgentSessionSummary, error) {
	validated, _, err := s.resolveScope(ctx, scope)
	if err != nil {
		return AgentSessionSummary{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	row := db.AgentSessionRow{
		ID: uuid.NewString(), ScopeType: validated.Type, Title: defaultSessionTitle, CreatedAt: now, UpdatedAt: now,
	}
	if validated.Type == "mod" {
		row.ModPath = &validated.ModPath
		row.ModName = &validated.ModName
	}
	client, err := s.dbClient()
	if err != nil {
		return AgentSessionSummary{}, err
	}
	if err := client.AgentSessions.Insert(ctx, row); err != nil {
		return AgentSessionSummary{}, err
	}
	return s.sessionSummary(row), nil
}

func (s *Service) ListSessions(ctx context.Context) ([]AgentSessionSummary, error) {
	client, err := s.dbClient()
	if err != nil {
		return nil, err
	}
	rows, err := client.AgentSessions.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AgentSessionSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.sessionSummary(row))
	}
	return out, nil
}

func (s *Service) GetSession(ctx context.Context, id string) (AgentSessionSnapshot, error) {
	client, err := s.dbClient()
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	row, err := client.AgentSessions.Get(ctx, id)
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	if row == nil {
		return AgentSessionSnapshot{}, errors.New("agent session not found")
	}
	events, err := client.AgentEvents.List(ctx, id)
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	entries := projectEvents(events)
	approvalRows, err := client.AgentApprovals.ListSession(ctx, id)
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	approvals := make([]AgentApproval, 0, len(approvalRows))
	approvalByID := make(map[string]AgentApproval, len(approvalRows))
	for _, approval := range approvalRows {
		view := agentApproval(approval)
		approvals = append(approvals, view)
		approvalByID[view.ID] = view
	}
	for index := range entries {
		if entries[index].Approval == nil {
			continue
		}
		if current, ok := approvalByID[entries[index].Approval.ID]; ok {
			entries[index].Approval = &current
		}
	}
	scope := rowScope(*row)
	_, roots, scopeErr := s.resolveScope(ctx, scope)
	var staged *AgentSessionRevert
	contextRow := *row
	// A staged revert already defines the next request surface even though Send has not committed
	// the event deletion yet.
	contextEvents := events
	if parsed := parseSessionRevert(row.Revert); parsed != nil {
		reverted := 0
		for index := range entries {
			if entries[index].Sequence >= parsed.BoundarySequence {
				entries[index].Reverted = true
				reverted++
			}
		}
		parsed.RevertedCount = reverted
		staged = parsed
		for index := range events {
			if events[index].Sequence >= parsed.BoundarySequence {
				contextEvents = events[:index]
				break
			}
		}
		contextRow.DurableSummary = latestSummary(contextEvents)
	}
	if scopeErr == nil {
		roots = mergeSandboxRoots(roots, sandboxRootsFromEvents(contextEvents))
	}
	snapshot := AgentSessionSnapshot{
		Summary:   s.sessionSummary(*row),
		Entries:   entries,
		Roots:     roots,
		Approvals: approvals,
		Revert:    staged,
	}
	if settings, settingsErr := readSettings(ctx, client, s.crypto); settingsErr == nil {
		snapshot.SupportsImages = settings.SupportsImages
		if scopeErr == nil {
			messages := s.messagesFromEvents(contextEvents, settings.SupportsImages, false)
			system := s.systemPrompt(ctx, contextRow, roots, settings.SupportsImages)
			// Tools are priced from the built-in set alone, which under-counts the MCP definitions a
			// request may also carry. A matching anchor overrides the figure, so only the no-anchor
			// fallback is approximate; rebuilding the MCP set here would open provider connections on
			// a read path.
			if usage, ok := buildContextUsage(
				settings.ContextWindowSize, contextRouteKey(settings),
				system, messages, builtInToolDefinitions(), parseContextAnchor(contextEvents),
			); ok {
				snapshot.ContextUsage = &usage
			}
		}
	}
	if scopeErr != nil {
		snapshot.UnavailableReason = scopeErr.Error()
	}
	return snapshot, nil
}

func (s *Service) RenameSession(ctx context.Context, id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("session title is required")
	}
	client, err := s.dbClient()
	if err != nil {
		return err
	}
	return client.AgentSessions.Rename(ctx, id, truncateRunes(title, 80), time.Now().UTC().Format(time.RFC3339Nano))
}

func (s *Service) DeleteSession(ctx context.Context, id string) error {
	s.mu.Lock()
	worker := s.workers[id]
	delete(s.workers, id)
	s.mu.Unlock()
	if worker != nil {
		worker.mu.Lock()
		if worker.cancel != nil {
			worker.cancel()
		}
		worker.mu.Unlock()
	}
	client, err := s.dbClient()
	if err != nil {
		return err
	}
	if err := client.AgentSessions.Delete(ctx, id); err != nil {
		return err
	}
	if s.appData != nil {
		for _, relative := range []string{
			filepath.Join("agent", "artifacts", id),
			filepath.Join(imageDirectory, id),
		} {
			target, resolveErr := s.appData.Resolve(relative)
			if resolveErr == nil {
				_ = os.RemoveAll(target)
			}
		}
	}
	return nil
}

func (s *Service) RevertSession(ctx context.Context, id string, sequence int64) (AgentSessionSnapshot, error) {
	lock := s.sessionLock(id)
	lock.Lock()
	defer lock.Unlock()
	client, err := s.dbClient()
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	row, err := client.AgentSessions.Get(ctx, id)
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	if row == nil {
		return AgentSessionSnapshot{}, errors.New("agent session not found")
	}
	if s.sessionSummary(*row).Running {
		return AgentSessionSnapshot{}, errors.New("the agent is still responding to this chat")
	}
	approvals, err := client.AgentApprovals.ListSession(ctx, id)
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	for _, approval := range approvals {
		if approval.Status == "pending" || approval.Status == "executing" {
			return AgentSessionSnapshot{}, errors.New("agent session is awaiting an action decision")
		}
	}
	events, err := client.AgentEvents.List(ctx, id)
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	var boundary *db.AgentEventRow
	for index := range events {
		if events[index].Sequence == sequence && events[index].EventType == "turn/start" {
			boundary = &events[index]
			break
		}
	}
	if boundary == nil {
		return AgentSessionSnapshot{}, errors.New("revert target is not a user message")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	encoded, err := json.Marshal(AgentSessionRevert{
		BoundarySequence: boundary.Sequence, BoundaryTurnID: boundary.TurnID, CreatedAt: now,
	})
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	if err := client.AgentSessions.SetRevert(ctx, id, string(encoded), now); err != nil {
		return AgentSessionSnapshot{}, err
	}
	return s.GetSession(ctx, id)
}

func (s *Service) UnrevertSession(ctx context.Context, id string) (AgentSessionSnapshot, error) {
	lock := s.sessionLock(id)
	lock.Lock()
	defer lock.Unlock()
	client, err := s.dbClient()
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	row, err := client.AgentSessions.Get(ctx, id)
	if err != nil {
		return AgentSessionSnapshot{}, err
	}
	if row == nil {
		return AgentSessionSnapshot{}, errors.New("agent session not found")
	}
	if row.Revert != "" {
		if err := client.AgentSessions.ClearRevert(ctx, id, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return AgentSessionSnapshot{}, err
		}
	}
	return s.GetSession(ctx, id)
}

func (s *Service) Send(ctx context.Context, id, text string, images []AgentImageInput) (SendResult, error) {
	if s.closing.Load() {
		return SendResult{}, errors.New("agent is shutting down")
	}
	text = strings.TrimSpace(text)
	if text == "" && len(images) == 0 {
		return SendResult{}, errors.New("message is required")
	}
	lock := s.sessionLock(id)
	lock.Lock()
	defer lock.Unlock()
	client, err := s.dbClient()
	if err != nil {
		return SendResult{}, err
	}
	row, err := client.AgentSessions.Get(ctx, id)
	if err != nil || row == nil {
		return SendResult{}, errors.New("agent session not found")
	}
	if _, _, err := s.resolveScope(ctx, rowScope(*row)); err != nil {
		return SendResult{}, err
	}
	approvals, err := client.AgentApprovals.ListSession(ctx, id)
	if err != nil {
		return SendResult{}, err
	}
	for _, approval := range approvals {
		if approval.Status == "pending" || approval.Status == "executing" {
			return SendResult{}, errors.New("agent session is awaiting an action decision")
		}
	}
	attachments, err := decodeImageInputs(images)
	if err != nil {
		return SendResult{}, err
	}
	if len(attachments) > 0 {
		settings, settingsErr := readSettings(ctx, client, s.crypto)
		if settingsErr != nil {
			return SendResult{}, settingsErr
		}
		if !settings.SupportsImages {
			return SendResult{}, errors.New("the configured model is not marked as accepting images")
		}
	}
	stored, err := s.storeImages(id, attachments)
	if err != nil {
		return SendResult{}, fmt.Errorf("store agent images: %w", err)
	}
	persisted := false
	defer func() {
		if !persisted {
			s.cleanupStoredImages(stored)
		}
	}()

	run := queuedRun{id: uuid.NewString(), text: text, fallbackTitle: messageTitle(text, stored)}
	payload := map[string]any{"text": run.text}
	if len(stored) > 0 {
		payload["images"] = stored
	}
	if promoted := promotedSandboxRoots(text); len(promoted) > 0 {
		payload["sandboxRoots"] = promoted
	}
	var startSequence int64
	if staged := parseSessionRevert(row.Revert); staged != nil {
		startSequence, err = s.appendRevertedTurn(ctx, client, *row, *staged, run, payload)
	} else {
		startSequence, err = s.appendEvent(
			ctx,
			db.AgentEventRow{SessionID: id, TurnID: run.id, EventType: "turn/start"},
			payload,
		)
	}
	if err != nil {
		return SendResult{}, fmt.Errorf("persist agent message: %w", err)
	}
	persisted = true
	worker := s.worker(id)
	if worker == nil {
		s.finishRunDetached(id, run.id, "turn/cancelled", map[string]any{
			"error": "agent shut down before the turn started", "status": "cancelled",
		})
		return SendResult{}, errors.New("agent is shutting down")
	}
	select {
	case worker.queue <- run:
		s.emit(id, run.id, startSequence, "status", map[string]any{"status": "queued"})
		return SendResult{RunID: run.id}, nil
	case <-s.stop:
		s.finishRunDetached(id, run.id, "turn/cancelled", map[string]any{
			"error": "agent shut down before the turn started", "status": "cancelled",
		})
		return SendResult{}, errors.New("agent is shutting down")
	case <-ctx.Done():
		s.finishRunDetached(id, run.id, "turn/cancelled", map[string]any{
			"error": ctx.Err().Error(), "status": "cancelled",
		})
		return SendResult{}, ctx.Err()
	}
}

func (s *Service) Cancel(id, runID string) error {
	s.mu.Lock()
	worker := s.workers[id]
	s.mu.Unlock()
	if worker == nil {
		return nil
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.active == runID && worker.cancel != nil {
		worker.cancel()
	}
	return nil
}

func (s *Service) GetSettings(ctx context.Context) (AgentSettingsView, error) {
	client, err := s.dbClient()
	if err != nil {
		return AgentSettingsView{}, err
	}
	s.credentialMu.Lock()
	defer s.credentialMu.Unlock()
	if err := s.migrateLegacyAPIKey(ctx, client); err != nil {
		return AgentSettingsView{}, err
	}
	return readSettings(ctx, client, s.crypto)
}

func (s *Service) UpdateSettings(ctx context.Context, input UpdateAgentSettingsInput) (AgentSettingsView, error) {
	client, err := s.dbClient()
	if err != nil {
		return AgentSettingsView{}, err
	}
	s.credentialMu.Lock()
	defer s.credentialMu.Unlock()
	if err := s.migrateLegacyAPIKey(ctx, client); err != nil {
		return AgentSettingsView{}, err
	}
	return updateSettings(ctx, client, s.crypto, input)
}

// ListProviders reports every built-in provider with its model catalog and connection state.
func (s *Service) ListProviders(ctx context.Context) (AgentProviderCatalogView, error) {
	client, err := s.dbClient()
	if err != nil {
		return AgentProviderCatalogView{}, err
	}
	s.credentialMu.Lock()
	defer s.credentialMu.Unlock()
	if err := s.migrateLegacyAPIKey(ctx, client); err != nil {
		return AgentProviderCatalogView{}, err
	}
	credentials, err := readCredentials(ctx, client, s.crypto)
	if err != nil {
		return AgentProviderCatalogView{}, err
	}
	s.scheduleCatalogRefresh()

	views := make([]AgentProviderView, 0, len(providerSpecs)+1)
	for _, spec := range providerSpecs {
		views = append(views, providerView(spec, credentials[spec.ID]))
	}
	return AgentProviderCatalogView{
		Catalog:   catalogStatus(),
		Providers: append(views, customProviderView(credentials[providerCustom])),
	}, nil
}

// RefreshProviderCatalog downloads the current models.dev catalog and reports the provider list
// built from it.
func (s *Service) RefreshProviderCatalog(ctx context.Context) (AgentProviderCatalogView, error) {
	if err := s.refreshCatalog(ctx); err != nil {
		return AgentProviderCatalogView{}, err
	}
	return s.ListProviders(ctx)
}

// UpdateProviderCredential stores or clears the API key of one provider and reports the updated
// provider entry.
func (s *Service) UpdateProviderCredential(
	ctx context.Context,
	input UpdateAgentCredentialInput,
) (AgentProviderView, error) {
	client, err := s.dbClient()
	if err != nil {
		return AgentProviderView{}, err
	}
	s.credentialMu.Lock()
	defer s.credentialMu.Unlock()
	if err := s.migrateLegacyAPIKey(ctx, client); err != nil {
		return AgentProviderView{}, err
	}
	if err := updateCredential(ctx, client, s.crypto, input); err != nil {
		return AgentProviderView{}, err
	}
	credentials, err := readCredentials(ctx, client, s.crypto)
	if err != nil {
		return AgentProviderView{}, err
	}
	if spec, builtin := providerSpecFor(strings.TrimSpace(input.ProviderID)); builtin {
		return providerView(spec, credentials[spec.ID]), nil
	}
	return customProviderView(credentials[providerCustom]), nil
}

func (s *Service) TestProvider(
	ctx context.Context,
	input UpdateAgentSettingsInput,
) (AgentProviderTestView, error) {
	if err := validateHeaders(input.Headers); err != nil {
		return AgentProviderTestView{}, err
	}
	view, err := settingsFromInput(input)
	if err != nil {
		return AgentProviderTestView{}, err
	}
	if err := validateSettings(view); err != nil {
		return AgentProviderTestView{}, err
	}
	client, err := s.dbClient()
	if err != nil {
		return AgentProviderTestView{}, err
	}
	credential, err := credentialFor(ctx, client, s.crypto, view.Provider)
	if err != nil {
		return AgentProviderTestView{}, err
	}
	// The key typed in the form is tested as given, without saving it first.
	if key := strings.TrimSpace(input.APIKey); key != "" {
		credential = providerCredential{Type: credentialKindAPI, Key: key}
	}
	if isBuiltinProvider(view.Provider) && credential.Type == "" {
		return AgentProviderTestView{}, errors.New(disconnectedProviderMessage(view.Provider))
	}
	secrets, err := resolveHeaderSecrets(ctx, client, s.crypto, input.Headers)
	if err != nil {
		return AgentProviderTestView{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, modelTimeout)
	defer cancel()
	// Connection tests have no conversation, but providers such as OpenCode Go require a session
	// header. A throwaway id is enough for that check.
	sessionID := uuid.NewString()
	headers := providerHeaders(view.Provider, view.Headers, secrets, sessionID, uuid.NewString())
	started := time.Now()
	_, err = newModelAdapter(modelAdapterConfig{
		settings:   view,
		credential: credential,
		headers:    headers,
		sessionID:  sessionID,
		refresh:    s.credentialRefresher(client, view.Provider),
	}, s.http).Complete(requestCtx, ModelRequest{
		System: "Reply with OK.", Messages: []Message{{Role: "user", Content: "Connection test"}}, MaxOutputTokens: 8,
	}, func(string, string) {})
	if err != nil {
		return AgentProviderTestView{}, err
	}
	return AgentProviderTestView{
		Provider: view.Provider,
		Model:    view.Model,
		Endpoint: view.Endpoint,
		Latency:  time.Since(started).Milliseconds(),
	}, nil
}

// providerView pairs a provider with its catalog models; a ChatGPT login hides the models the
// plan does not serve.
func providerView(spec providerSpec, credential providerCredential) AgentProviderView {
	view := AgentProviderView{
		ID:              spec.ID,
		Name:            spec.Name,
		Endpoint:        spec.Endpoint,
		DefaultProtocol: spec.DefaultProtocol,
		SupportsAPIKey:  true,
		SupportsOAuth:   spec.OAuth,
		Credential:      credentialView(credential),
	}
	for _, model := range catalogModelsFor(spec.ID) {
		if credential.Type == credentialKindOAuth && !model.OAuth {
			continue
		}
		view.Models = append(view.Models, model)
	}
	return view
}

// customProviderView describes the user-defined provider entry; its endpoint and protocol come
// from settings rather than a spec.
func customProviderView(credential providerCredential) AgentProviderView {
	return AgentProviderView{
		ID:             providerCustom,
		Custom:         true,
		SupportsAPIKey: true,
		Credential:     credentialView(credential),
	}
}

func providerName(providerID string) string {
	if spec, ok := providerSpecFor(providerID); ok {
		return spec.Name
	}
	return "Nahida Agent provider"
}

// disconnectedProviderMessage tells the user how to connect a provider that has no credential.
func disconnectedProviderMessage(providerID string) string {
	if spec, ok := providerSpecFor(providerID); ok && spec.OAuth {
		return fmt.Sprintf("%s is not connected: sign in or add an API key", spec.Name)
	}
	return fmt.Sprintf("%s is not connected: add an API key", providerName(providerID))
}

func (s *Service) ListSkills() []SkillView {
	if s.skills == nil {
		return nil
	}
	return s.skills.List()
}

func (s *Service) ReloadSkills() []SkillView {
	if s.skills == nil {
		return nil
	}
	return s.skills.Reload()
}

func (s *Service) GetUserSkillsPath() string {
	if s.skills == nil {
		return ""
	}
	return s.skills.userPath
}

func (s *Service) OpenUserSkillsFolder() error {
	if s.shell == nil || s.skills == nil {
		return errors.New("skills folder is unavailable")
	}
	return s.shell.OpenPath(s.skills.userPath)
}

func (s *Service) ServiceShutdown() error {
	if !s.closing.CompareAndSwap(false, true) {
		return nil
	}
	s.cancelLogin()
	close(s.stop)
	s.cancelRun()
	s.mu.Lock()
	for _, worker := range s.workers {
		worker.mu.Lock()
		if worker.cancel != nil {
			worker.cancel()
		}
		worker.mu.Unlock()
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("timed out waiting for Nahida Agent runs to stop")
	}
}

func (s *Service) worker(sessionID string) *sessionWorker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing.Load() {
		return nil
	}
	if worker := s.workers[sessionID]; worker != nil {
		return worker
	}
	worker := &sessionWorker{queue: make(chan queuedRun, 32)}
	s.workers[sessionID] = worker
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.cancelQueuedRuns(sessionID, worker)
		for {
			var run queuedRun
			select {
			case <-s.stop:
				return
			case run = <-worker.queue:
			}
			select {
			case <-s.stop:
				return
			case s.global <- struct{}{}:
			}
			runCtx, cancel := context.WithCancel(s.runCtx)
			lock := s.sessionLock(sessionID)
			lock.Lock()
			worker.mu.Lock()
			worker.active = run.id
			worker.cancel = cancel
			worker.mu.Unlock()
			lock.Unlock()
			if run.approvalID != "" {
				err := s.executeApprovedAction(runCtx, run.approvalID)
				if run.result != nil {
					run.result <- err
				}
			} else {
				s.executeRun(runCtx, sessionID, run)
			}
			cancel()
			lock.Lock()
			worker.mu.Lock()
			worker.active = ""
			worker.cancel = nil
			worker.mu.Unlock()
			lock.Unlock()
			<-s.global
		}
	}()
	return worker
}

func (s *Service) executeRun(ctx context.Context, sessionID string, run queuedRun) {
	client, err := s.dbClient()
	if err != nil {
		s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{"error": err.Error()})
		return
	}
	row, err := client.AgentSessions.Get(ctx, sessionID)
	if err != nil || row == nil {
		message := "agent session not found"
		if err != nil {
			message = err.Error()
		}
		s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{"error": message})
		return
	}
	_, roots, err := s.resolveScope(ctx, rowScope(*row))
	if err != nil {
		s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{"error": err.Error()})
		return
	}
	events, err := client.AgentEvents.List(ctx, sessionID)
	if err != nil {
		s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{"error": err.Error()})
		return
	}
	roots = mergeSandboxRoots(roots, sandboxRootsFromEvents(events))
	sandbox, err := NewSandbox(roots)
	if err != nil {
		s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{"error": err.Error()})
		return
	}
	defer func() { _ = sandbox.Close() }()

	// A fresh session is named after its first message right away; a summary of that message
	// replaces the name once the model has written it.
	var pendingTitle string
	if !run.resume {
		s.emit(sessionID, run.id, 0, "status", map[string]any{"status": "running"})
		if row.Title == defaultSessionTitle && run.fallbackTitle != "" {
			err := client.AgentSessions.Rename(
				ctx,
				row.ID,
				run.fallbackTitle,
				time.Now().UTC().Format(time.RFC3339Nano),
			)
			if err != nil {
				_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
					Severity: infra.DiagnosticWarn, Operation: "agent-session", Stage: "auto-rename",
					Fields: map[string]any{"sessionId": sessionID, "runId": run.id},
				})
			} else {
				pendingTitle = run.fallbackTitle
			}
		}
	} else {
		s.emit(sessionID, run.id, 0, "status", map[string]any{"status": "running"})
	}
	settings, err := readSettings(ctx, client, s.crypto)
	if err != nil {
		s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{"error": err.Error()})
		return
	}
	routeKey := contextRouteKey(settings)
	credential, err := credentialFor(ctx, client, s.crypto, settings.Provider)
	if err != nil {
		s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{"error": err.Error()})
		return
	}
	if isBuiltinProvider(settings.Provider) && credential.Type == "" {
		s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{
			"error": disconnectedProviderMessage(settings.Provider),
		})
		return
	}
	secrets, err := readHeaderSecrets(ctx, client, s.crypto)
	if err != nil {
		s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{"error": err.Error()})
		return
	}
	adapter := newModelAdapter(modelAdapterConfig{
		settings:   settings,
		credential: credential,
		headers:    providerHeaders(settings.Provider, settings.Headers, secrets, sessionID, run.id),
		sessionID:  sessionID,
		refresh:    s.credentialRefresher(client, settings.Provider),
	}, s.http)
	if pendingTitle != "" && strings.TrimSpace(run.text) != "" {
		s.startSessionTitleGeneration(adapter, sessionID, run.id, pendingTitle, run.text)
	}
	messages := s.messagesFromEvents(events, settings.SupportsImages, true)
	mcpRuntime, mcpDefinitions := s.openMCPRuntime(ctx, roots)
	defer func() { _ = mcpRuntime.Close() }()
	executor := &toolExecutor{
		sandbox: sandbox, skills: s.skills, desktop: s.actions,
		scope: rowScope(*row), mcp: mcpRuntime,
	}
	toolDefinitions := append(builtInToolDefinitions(), mcpDefinitions...)
	system := s.systemPrompt(ctx, *row, roots, settings.SupportsImages)
	requestBudget := contextInputBudget(settings.ContextWindowSize, settings.MaxOutputTokens)
	if estimateTokens(system, messages, toolDefinitions) > requestBudget {
		messages, row.DurableSummary, err = s.compactMessages(
			ctx, adapter, sessionID, run.id, messages, row.DurableSummary,
		)
		if err != nil {
			s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{"error": err.Error()})
			return
		}
		system = s.systemPrompt(ctx, *row, roots, settings.SupportsImages)
		if estimateTokens(system, messages, toolDefinitions) > requestBudget {
			s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{
				"error": "agent context is too large after compaction; start a new conversation or reduce tool output",
			})
			return
		}
	}
	for range maxToolRounds {
		var response ModelResponse
		var modelErr error
		for attempt := range 2 {
			estimatedInput := estimateTokens(system, messages, toolDefinitions)
			maxOutputTokens := contextOutputBudget(
				settings.ContextWindowSize, settings.MaxOutputTokens, estimatedInput,
			)
			if maxOutputTokens == 0 {
				modelErr = errors.New(
					"agent input context leaves no room for a model response; start a new conversation or reduce tool output",
				)
				break
			}
			modelCtx, cancel := context.WithTimeout(ctx, modelTimeout)
			response, modelErr = adapter.Complete(modelCtx, ModelRequest{
				System:          system,
				Messages:        messages,
				Tools:           toolDefinitions,
				MaxOutputTokens: maxOutputTokens,
				Reasoning:       settings.Reasoning,
			}, func(kind, delta string) {
				s.emit(sessionID, run.id, 0, kind, map[string]any{"delta": delta})
			})
			cancel()
			if !isContextOverflow(modelErr) || attempt == 1 {
				break
			}
			before := len(messages)
			messages, row.DurableSummary, err = s.compactMessages(
				ctx, adapter, sessionID, run.id, messages, row.DurableSummary,
			)
			if err != nil {
				modelErr = err
				break
			}
			system = s.systemPrompt(ctx, *row, roots, settings.SupportsImages)
			if len(messages) >= before {
				modelErr = errors.New("provider context overflow could not be compacted")
				break
			}
		}
		if modelErr != nil {
			if response.Text != "" || response.Reasoning != "" {
				persistCtx, cancel := s.persistenceContext(ctx)
				_, persistErr := s.appendEvent(
					persistCtx,
					db.AgentEventRow{SessionID: sessionID, TurnID: run.id, EventType: "message/assistant"},
					map[string]any{
						"text": response.Text, "reasoning": response.Reasoning, "interrupted": true,
					},
				)
				cancel()
				if persistErr != nil {
					s.emit(sessionID, run.id, 0, "error", map[string]any{
						"message": fmt.Sprintf("persist interrupted assistant response: %v", persistErr),
					})
				}
			}
			eventType := "turn/error"
			if errors.Is(modelErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				eventType = "turn/cancelled"
			}
			payload := map[string]any{"error": modelErr.Error()}
			if eventType == "turn/cancelled" {
				payload["status"] = "cancelled"
			}
			s.finishRunDetached(sessionID, run.id, eventType, payload)
			return
		}
		hasAssistantMessage := response.Text != "" || response.Reasoning != "" || len(response.ToolCalls) > 0
		if hasAssistantMessage {
			if _, err := s.appendEvent(
				ctx,
				db.AgentEventRow{SessionID: sessionID, TurnID: run.id, EventType: "message/assistant"},
				map[string]any{
					"text":      response.Text,
					"reasoning": response.Reasoning,
					"toolCalls": response.ToolCalls,
				},
			); err != nil {
				s.failRunPersistence(sessionID, run.id, "assistant response", err)
				return
			}
		}
		// Anchor the provider's own prompt size to the surface that produced it. The event is durable
		// so a later snapshot can reprice the next request against the same scale; a provider that
		// reports no usage leaves the previous anchor in place.
		breakdown := estimateBreakdown(system, messages, toolDefinitions)
		var anchor *contextAnchor
		if response.InputTokens > 0 {
			anchor = &contextAnchor{
				RouteKey:      routeKey,
				InputTokens:   response.InputTokens,
				SystemTokens:  breakdown.System,
				ToolsTokens:   breakdown.Tools,
				MessageTokens: breakdown.Messages,
			}
			if _, err := s.appendEvent(
				ctx,
				db.AgentEventRow{SessionID: sessionID, TurnID: run.id, EventType: contextUsageEventType},
				contextUsagePayload{
					Provider: settings.Provider, Model: settings.Model,
					RouteKey:    routeKey,
					InputTokens: response.InputTokens, OutputTokens: response.OutputTokens,
					SystemTokens: breakdown.System, ToolsTokens: breakdown.Tools, MessageTokens: breakdown.Messages,
				},
			); err != nil {
				// The anchor is observability data: losing it must not discard the assistant response
				// that was already accepted above. Keep the in-memory anchor for this turn's live
				// reading and let the next successful call re-anchor.
				_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
					Severity: infra.DiagnosticWarn, Operation: "agent-context", Stage: "persist-anchor",
					Fields: map[string]any{"sessionId": sessionID, "turnId": run.id},
				})
			}
		}
		usagePayload := map[string]any{
			"inputTokens": response.InputTokens, "outputTokens": response.OutputTokens,
		}
		// The streamed meter projects the next request, so include the assistant response persisted
		// above while tool execution is still in progress.
		projectedMessages := messages
		if hasAssistantMessage {
			projectedMessages = make([]Message, 0, len(messages)+1)
			projectedMessages = append(projectedMessages, messages...)
			projectedMessages = append(projectedMessages, Message{
				Role: "assistant", Content: response.Text, Reasoning: response.Reasoning, ToolCalls: response.ToolCalls,
			})
		}
		if contextUsage, ok := buildContextUsage(
			settings.ContextWindowSize, routeKey,
			system, projectedMessages, toolDefinitions, anchor,
		); ok {
			usagePayload["contextUsage"] = contextUsage
		}
		s.emit(sessionID, run.id, 0, "usage", usagePayload)
		if len(response.ToolCalls) == 0 {
			s.finishRunDetached(sessionID, run.id, "turn/end", map[string]any{"status": "completed"})
			return
		}
		for callIndex, call := range response.ToolCalls {
			if _, err := s.appendEvent(
				ctx,
				db.AgentEventRow{SessionID: sessionID, TurnID: run.id, EventType: "tool/start"},
				call,
			); err != nil {
				s.failRunPersistence(sessionID, run.id, "tool start", err)
				return
			}
			s.emit(sessionID, run.id, 0, "tool-start", call)
			result, toolErr := executor.Execute(ctx, call)
			if toolErr != nil && call.Name == "run_desktop_action" {
				var actionCall agentactions.Call
				if json.Unmarshal(call.Arguments, &actionCall) == nil {
					toolErr = s.reportActionError(toolErr, db.AgentApprovalRow{
						SessionID: sessionID, ActionID: actionCall.ActionID,
					}, rowScope(*row))
				}
			}
			if toolErr == nil && result.Approval != nil {
				approval, approvalErr := s.requestApproval(ctx, sessionID, run.id, call.ID, *result.Approval)
				if approvalErr != nil {
					s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{"error": approvalErr.Error()})
					return
				}
				if _, err := s.appendEvent(
					ctx,
					db.AgentEventRow{SessionID: sessionID, TurnID: run.id, EventType: "approval/requested"},
					approval,
				); err != nil {
					s.failRunPersistence(sessionID, run.id, "approval request", err)
					return
				}
				s.emit(sessionID, run.id, 0, "approval-requested", approval)
				if err := s.appendSkippedToolCalls(
					ctx, sessionID, run.id, response.ToolCalls[callIndex+1:],
				); err != nil {
					s.failRunPersistence(sessionID, run.id, "skipped tool results", err)
					return
				}
				if _, err := s.appendEvent(
					ctx,
					db.AgentEventRow{SessionID: sessionID, TurnID: run.id, EventType: "turn/awaiting-approval"},
					map[string]any{"approvalId": approval.ID, "status": "awaiting-approval"},
				); err != nil {
					s.failRunPersistence(sessionID, run.id, "awaiting approval state", err)
					return
				}
				s.emit(sessionID, run.id, 0, "status", map[string]any{
					"status": "awaiting-approval", "approvalId": approval.ID,
				})
				return
			}
			output, artifact := s.toolOutput(sessionID, run.id, call.ID, result.Output)
			payload := map[string]any{"toolName": call.Name, "toolCallId": call.ID, "result": output,
				"changedFiles": result.ChangedFiles, "artifact": artifact}
			if toolErr != nil {
				payload["error"] = toolErr.Error()
			}
			if len(result.Images) > 0 {
				if images, imageErr := s.storeImages(sessionID, result.Images); imageErr != nil {
					_ = infra.ReportError(s.log, imageErr, "Agent", infra.Diagnostic{
						Severity: infra.DiagnosticWarn, Operation: "agent-tool", Stage: "store-images",
						Fields: map[string]any{"sessionId": sessionID, "runId": run.id, "tool": call.Name},
					})
				} else {
					payload["images"] = images
				}
			}
			if _, err := s.appendEvent(
				ctx,
				db.AgentEventRow{SessionID: sessionID, TurnID: run.id, EventType: "tool/end"},
				payload,
			); err != nil {
				s.failRunPersistence(sessionID, run.id, "tool result", err)
				return
			}
			s.emit(sessionID, run.id, 0, "tool-end", payload)
		}
		events, err = client.AgentEvents.List(ctx, sessionID)
		if err != nil {
			s.failRunPersistence(sessionID, run.id, "reload durable messages", err)
			return
		}
		messages = s.messagesFromEvents(events, settings.SupportsImages, true)
	}
	s.finishRunDetached(sessionID, run.id, "turn/error", map[string]any{"error": "maximum tool rounds exceeded"})
}

func (s *Service) appendSkippedToolCalls(
	ctx context.Context,
	sessionID, runID string,
	calls []ToolCall,
) error {
	for _, call := range calls {
		if _, err := s.appendEvent(
			ctx,
			db.AgentEventRow{SessionID: sessionID, TurnID: runID, EventType: "tool/start"},
			call,
		); err != nil {
			return err
		}
		s.emit(sessionID, runID, 0, "tool-start", call)
		payload := map[string]any{
			"toolName": call.Name, "toolCallId": call.ID,
			"result": map[string]any{"completed": false}, "error": skippedToolErrorMessage,
		}
		if _, err := s.appendEvent(
			ctx,
			db.AgentEventRow{SessionID: sessionID, TurnID: runID, EventType: "tool/end"},
			payload,
		); err != nil {
			return err
		}
		s.emit(sessionID, runID, 0, "tool-end", payload)
	}
	return nil
}

func (s *Service) failRunPersistence(sessionID, runID, stage string, cause error) {
	err := fmt.Errorf("persist %s: %w", stage, cause)
	_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
		Operation: "agent-run", Stage: stage, Fields: map[string]any{"sessionId": sessionID, "runId": runID},
	})
	s.finishRunDetached(sessionID, runID, "turn/error", map[string]any{"error": err.Error()})
}

func (s *Service) ApproveAction(ctx context.Context, approvalID string) error {
	client, err := s.dbClient()
	if err != nil {
		return err
	}
	pending, err := client.AgentApprovals.Get(ctx, approvalID)
	if err != nil {
		return err
	}
	if err := s.validateApprovalContext(ctx, client, pending); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	row, err := client.AgentApprovals.Transition(ctx, approvalID, "pending", "executing", now)
	if err != nil {
		return err
	}
	approval := agentApproval(*row)
	s.emit(row.SessionID, row.TurnID, 0, "approval-updated", approval)
	result := make(chan error, 1)
	worker := s.worker(row.SessionID)
	if worker == nil {
		return s.failApprovalDetached(*row, errors.New("agent shut down before the approved action started"))
	}
	select {
	case worker.queue <- queuedRun{id: row.TurnID, approvalID: row.ID, result: result}:
	case <-s.stop:
		return s.failApprovalDetached(*row, errors.New("agent shut down before the approved action started"))
	case <-ctx.Done():
		return s.failApprovalDetached(*row, ctx.Err())
	}

	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.stop:
		return errors.New("agent is shutting down")
	}
}

func (s *Service) executeApprovedAction(ctx context.Context, approvalID string) error {
	client, err := s.dbClient()
	if err != nil {
		return err
	}
	row, err := client.AgentApprovals.Get(ctx, approvalID)
	if err != nil {
		if ctx.Err() != nil {
			persistCtx, cancel := s.persistenceContext(ctx)
			defer cancel()
			interrupted, getErr := client.AgentApprovals.Get(persistCtx, approvalID)
			if getErr == nil && interrupted != nil {
				return s.failApproval(persistCtx, *interrupted, ctx.Err())
			}
			return errors.Join(ctx.Err(), getErr)
		}
		return err
	}
	if row == nil {
		return errors.New("agent approval not found")
	}
	if row.Status != "executing" {
		return fmt.Errorf("agent approval is %s, expected executing", row.Status)
	}
	session, err := client.AgentSessions.Get(ctx, row.SessionID)
	if err != nil || session == nil {
		return s.failApprovalDetached(*row, errors.New("agent session not found"))
	}
	_, roots, err := s.resolveScope(ctx, rowScope(*session))
	if err != nil {
		return s.failApprovalDetached(*row, err)
	}
	events, err := client.AgentEvents.List(ctx, row.SessionID)
	if err != nil {
		return s.failApprovalDetached(*row, err)
	}
	roots = mergeSandboxRoots(roots, sandboxRootsFromEvents(events))
	sandbox, err := NewSandbox(roots)
	if err != nil {
		return s.failApprovalDetached(*row, err)
	}
	defer func() { _ = sandbox.Close() }()

	executor := &toolExecutor{sandbox: sandbox, skills: s.skills, desktop: s.actions,
		scope: rowScope(*session)}
	kind := "desktop"
	if strings.HasPrefix(row.ActionID, "sandbox.") {
		kind = "sandbox"
	}
	result, actionErr := executor.ExecuteApproved(ctx, row.ActionID, kind, json.RawMessage(row.Arguments))
	if actionErr != nil {
		actionErr = s.reportActionError(actionErr, *row, rowScope(*session))
	}
	output, artifact := s.toolOutput(row.SessionID, row.TurnID, row.ToolCallID, result.Output)
	payload := map[string]any{"toolName": approvalToolName(row.ActionID), "toolCallId": row.ToolCallID,
		"result": output, "changedFiles": result.ChangedFiles, "artifact": artifact}
	status, errorMessage := "completed", ""
	if actionErr != nil {
		status, errorMessage = "failed", actionErr.Error()
		payload["error"] = errorMessage
	}
	encodedResult, _ := json.Marshal(output)
	completedAt := time.Now().UTC().Format(time.RFC3339Nano)
	completionEvents, updated, err := approvalCompletionEvents(
		*row, status, string(encodedResult), errorMessage, completedAt, payload,
	)
	if err != nil {
		return err
	}
	persistCtx, cancel := s.persistenceContext(ctx)
	defer cancel()
	if err := client.AgentApprovals.CompleteWithEvents(
		persistCtx,
		row.ID,
		"executing",
		status,
		string(encodedResult),
		errorMessage,
		completedAt,
		completionEvents,
	); err != nil {
		return err
	}
	s.emit(row.SessionID, row.TurnID, 0, "tool-end", payload)
	s.emit(row.SessionID, row.TurnID, 0, "approval-updated", updated)
	s.enqueueResume(row.SessionID, row.TurnID)
	return nil
}

func (s *Service) RejectAction(ctx context.Context, approvalID string) error {
	client, err := s.dbClient()
	if err != nil {
		return err
	}
	pending, err := client.AgentApprovals.Get(ctx, approvalID)
	if err != nil {
		return err
	}
	if err := s.validateApprovalContext(ctx, client, pending); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	row, err := client.AgentApprovals.Transition(ctx, approvalID, "pending", "rejected", now)
	if err != nil {
		return err
	}
	result := json.RawMessage(`{"approved":false,"reason":"user rejected the action"}`)
	payload := map[string]any{
		"toolName":   approvalToolName(row.ActionID),
		"toolCallId": row.ToolCallID,
		"result":     result,
	}
	completionEvents, updated, err := approvalCompletionEvents(*row, "rejected", string(result), "", now, payload)
	if err != nil {
		return err
	}
	if err := client.AgentApprovals.CompleteWithEvents(
		ctx, row.ID, "rejected", "rejected", string(result), "", now, completionEvents,
	); err != nil {
		return err
	}
	s.emit(row.SessionID, row.TurnID, 0, "tool-end", payload)
	s.emit(row.SessionID, row.TurnID, 0, "approval-updated", updated)
	s.enqueueResume(row.SessionID, row.TurnID)
	return nil
}

func (s *Service) validateApprovalContext(
	ctx context.Context,
	client *db.Client,
	row *db.AgentApprovalRow,
) error {
	if row == nil {
		return errors.New("agent approval not found")
	}
	if row.Status != "pending" {
		return fmt.Errorf("agent approval is %s, expected pending", row.Status)
	}
	events, err := client.AgentEvents.List(ctx, row.SessionID)
	if err != nil {
		return err
	}
	hasToolStart, hasApprovalRequest, hasToolEnd := false, false, false
	for _, event := range events {
		if event.TurnID != row.TurnID {
			continue
		}
		switch event.EventType {
		case "tool/start":
			var call ToolCall
			if json.Unmarshal([]byte(event.Payload), &call) == nil && call.ID == row.ToolCallID {
				hasToolStart = true
			}
		case "approval/requested":
			var approval AgentApproval
			if json.Unmarshal([]byte(event.Payload), &approval) == nil && approval.ID == row.ID &&
				approval.ToolCallID == row.ToolCallID {
				hasApprovalRequest = true
			}
		case "tool/end":
			var payload struct {
				ToolCallID string `json:"toolCallId"`
			}
			if json.Unmarshal([]byte(event.Payload), &payload) == nil && payload.ToolCallID == row.ToolCallID {
				hasToolEnd = true
			}
		}
	}
	if !hasToolStart || !hasApprovalRequest || hasToolEnd {
		return errors.New("agent approval context is invalid or already completed")
	}
	return nil
}

func (s *Service) reportActionError(err error, row db.AgentApprovalRow, scope AgentScope) error {
	if err == nil || infra.IsReportedError(err) || infra.IsCancellationError(err) {
		return err
	}
	return infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
		Operation: "desktop-action",
		Stage:     "execute",
		Fields: map[string]any{
			"actionId":  row.ActionID,
			"sessionId": row.SessionID,
			"scope":     scope.Type,
			"target":    row.Target,
		},
	})
}

func (s *Service) requestApproval(
	ctx context.Context,
	sessionID, turnID, toolCallID string,
	proposal agentactions.Proposal,
) (AgentApproval, error) {
	client, err := s.dbClient()
	if err != nil {
		return AgentApproval{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	row := db.AgentApprovalRow{ID: uuid.NewString(), SessionID: sessionID, TurnID: turnID,
		ToolCallID: toolCallID, ActionID: proposal.ActionID, Arguments: string(proposal.Arguments),
		Summary: proposal.Summary, Target: proposal.Target, Impact: proposal.Impact, Status: "pending", CreatedAt: now}
	if err := client.AgentApprovals.Insert(ctx, row); err != nil {
		return AgentApproval{}, err
	}
	return agentApproval(row), nil
}

func (s *Service) failApproval(ctx context.Context, row db.AgentApprovalRow, cause error) error {
	client, err := s.dbClient()
	if err != nil {
		return errors.Join(cause, err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload := map[string]any{"toolName": approvalToolName(row.ActionID), "toolCallId": row.ToolCallID,
		"result": map[string]any{"completed": false}, "error": cause.Error()}
	completionEvents, updated, eventErr := approvalCompletionEvents(row, "failed", "", cause.Error(), now, payload)
	if eventErr != nil {
		return errors.Join(cause, eventErr)
	}
	if completeErr := client.AgentApprovals.CompleteWithEvents(
		ctx, row.ID, "executing", "failed", "", cause.Error(), now, completionEvents,
	); completeErr != nil {
		return errors.Join(cause, completeErr)
	}
	s.emit(row.SessionID, row.TurnID, 0, "tool-end", payload)
	s.emit(row.SessionID, row.TurnID, 0, "approval-updated", updated)
	s.enqueueResume(row.SessionID, row.TurnID)
	return cause
}

func approvalCompletionEvents(
	row db.AgentApprovalRow,
	status, result, errorMessage, completedAt string,
	toolPayload any,
) ([]db.AgentEventRow, AgentApproval, error) {
	row.Status = status
	row.Result = result
	row.Error = errorMessage
	row.CompletedAt = &completedAt
	updated := agentApproval(row)
	toolData, err := json.Marshal(toolPayload)
	if err != nil {
		return nil, AgentApproval{}, fmt.Errorf("encode approval tool result: %w", err)
	}
	approvalData, err := json.Marshal(updated)
	if err != nil {
		return nil, AgentApproval{}, fmt.Errorf("encode completed approval: %w", err)
	}
	return []db.AgentEventRow{
		{
			SessionID: row.SessionID,
			TurnID:    row.TurnID,
			EventType: "tool/end",
			Payload:   string(toolData),
			CreatedAt: completedAt,
		},
		{
			SessionID: row.SessionID,
			TurnID:    row.TurnID,
			EventType: "approval/updated",
			Payload:   string(approvalData),
			CreatedAt: completedAt,
		},
	}, updated, nil
}

func (s *Service) failApprovalDetached(row db.AgentApprovalRow, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), persistenceTimeout)
	defer cancel()
	return s.failApproval(ctx, row, cause)
}

func (s *Service) enqueueResume(sessionID, turnID string) {
	if s.closing.Load() {
		return
	}
	worker := s.worker(sessionID)
	if worker == nil {
		return
	}
	select {
	case worker.queue <- queuedRun{id: turnID, resume: true}:
	case <-s.stop:
	}
}

func (s *Service) cancelQueuedRuns(sessionID string, worker *sessionWorker) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for {
		select {
		case run := <-worker.queue:
			err := errors.New("agent shut down before the queued work started")
			if run.approvalID != "" {
				if client, clientErr := s.dbClient(); clientErr == nil {
					if row, getErr := client.AgentApprovals.Get(ctx, run.approvalID); getErr == nil && row != nil {
						err = s.failApproval(ctx, *row, err)
					}
				}
				if run.result != nil {
					run.result <- err
				}
				continue
			}
			_ = s.finishRun(ctx, sessionID, run.id, "turn/cancelled", map[string]any{
				"error": err.Error(), "status": "cancelled",
			})
		default:
			return
		}
	}
}

func approvalToolName(actionID string) string {
	switch actionID {
	case "sandbox.apply_patch":
		return "apply_patch"
	case "sandbox.move_path":
		return "move_path"
	default:
		return "run_desktop_action"
	}
}

func (s *Service) resolveScope(ctx context.Context, scope AgentScope) (AgentScope, []SandboxRoot, error) {
	client, err := s.dbClient()
	if err != nil {
		return AgentScope{}, nil, err
	}
	paths, err := client.GamePaths.List(ctx)
	if err != nil {
		return AgentScope{}, nil, err
	}
	gameRoots := make([]SandboxRoot, 0, len(paths))
	for _, path := range paths {
		canonical, canonicalErr := canonicalExistingDir(path.ModFolderPath)
		if canonicalErr != nil {
			continue
		}
		importer := ""
		if path.Importer != nil {
			importer = *path.Importer
		}
		gameRoots = append(
			gameRoots,
			SandboxRoot{ID: rootID(path.Game, canonical), Name: path.Game, Path: canonical, Importer: importer},
		)
	}
	switch scope.Type {
	case "global":
		if len(gameRoots) == 0 {
			return AgentScope{}, nil, errors.New("no available game Mods folders are configured")
		}
		return AgentScope{Type: "global"}, gameRoots, nil
	case "mod":
		canonical, err := canonicalExistingDir(scope.ModPath)
		if err != nil {
			return AgentScope{}, nil, fmt.Errorf("selected mod folder is unavailable: %w", err)
		}
		for _, gameRoot := range gameRoots {
			if pathWithin(gameRoot.Path, canonical) && !strings.EqualFold(gameRoot.Path, canonical) {
				name := strings.TrimSpace(scope.ModName)
				if name == "" {
					name = filepath.Base(canonical)
				}
				validated := AgentScope{Type: "mod", ModPath: canonical, ModName: name}
				return validated, []SandboxRoot{
					{ID: rootID(name, canonical), Name: name, Path: canonical, Importer: gameRoot.Importer},
				}, nil
			}
		}
		return AgentScope{}, nil, errors.New("selected mod folder is outside configured game Mods folders")
	default:
		return AgentScope{}, nil, fmt.Errorf("invalid agent scope %q", scope.Type)
	}
}

func (s *Service) systemPrompt(
	ctx context.Context,
	row db.AgentSessionRow,
	roots []SandboxRoot,
	supportsImages bool,
) string {
	rootData, _ := json.Marshal(roots)
	skillData, _ := json.Marshal(s.ListSkills())
	actionData, _ := json.Marshal(s.actions.Hints(row.ScopeType))
	language := "en"
	if s.settings != nil {
		if configured, err := s.settings.GetLanguage(ctx); err == nil {
			language = configured
		}
	}
	return renderSystemPrompt(
		row.ScopeType,
		string(rootData),
		row.DurableSummary,
		string(skillData),
		string(actionData),
		language,
		supportsImages,
	)
}

func (s *Service) compactMessages(
	ctx context.Context,
	adapter ModelAdapter,
	sessionID, runID string,
	messages []Message,
	existingSummary string,
) ([]Message, string, error) {
	const preserve = 6
	if len(messages) <= preserve {
		return messages, existingSummary, nil
	}
	cut := len(messages) - preserve
	for cut > 0 && messages[cut].Role == "tool" {
		cut--
	}
	if cut == 0 {
		return messages, existingSummary, nil
	}
	old, err := json.Marshal(summarizeMessages(messages[:cut]))
	if err != nil {
		return messages, existingSummary, fmt.Errorf("encode messages for compaction: %w", err)
	}
	prompt := "Existing summary:\n" + existingSummary + "\n\nOlder messages to summarize:\n" + string(old)
	modelCtx, cancel := context.WithTimeout(ctx, modelTimeout)
	defer cancel()
	response, err := adapter.Complete(modelCtx, ModelRequest{
		System: "Create a compact factual durable summary. Preserve user intent, decisions, file paths, changes, " +
			"tool outcomes, and unresolved work. Do not add facts.",
		Messages: []Message{{Role: "user", Content: prompt}}, MaxOutputTokens: 4096, Reasoning: "auto",
	}, func(string, string) {})
	if err != nil {
		return messages, existingSummary, fmt.Errorf("compact agent context: %w", err)
	}
	if strings.TrimSpace(response.Text) == "" {
		return messages, existingSummary, errors.New("compact agent context: provider returned an empty summary")
	}
	summary := strings.TrimSpace(response.Text)
	client, err := s.dbClient()
	if err != nil {
		return messages, existingSummary, err
	}
	payload, err := json.Marshal(map[string]any{"summary": summary, "compactedMessages": cut})
	if err != nil {
		return messages, existingSummary, fmt.Errorf("encode agent summary: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := client.AgentEvents.AppendAndUpdateSummary(ctx, db.AgentEventRow{
		SessionID: sessionID, TurnID: runID, EventType: "summary", Payload: string(payload), CreatedAt: now,
	}, summary); err != nil {
		return messages, existingSummary, err
	}
	return messages[cut:], summary, nil
}

func estimateTokens(system string, messages []Message, tools ...[]ToolDefinition) int {
	var definitions []ToolDefinition
	if len(tools) > 0 {
		definitions = tools[0]
	}
	return estimateBreakdown(system, messages, definitions).total()
}

func estimateTextTokens(value string) int {
	ascii, nonASCII := 0, 0
	for _, character := range value {
		if character <= 0x7f {
			ascii++
		} else {
			nonASCII++
		}
	}
	return (ascii+3)/4 + nonASCII
}

// estimateImageTokens approximates what a provider charges for one image. Providers derive the
// real cost from the decoded dimensions, which the agent does not track, so the estimate scales
// with the base64 payload instead.
func estimateImageTokens(encodedLength int) int {
	return 800 + encodedLength/4096
}

// summarizeMessages drops image payloads so a compaction request stays textual.
func summarizeMessages(messages []Message) []Message {
	trimmed := make([]Message, 0, len(messages))
	for _, message := range messages {
		if len(message.Images) == 0 {
			trimmed = append(trimmed, message)
			continue
		}
		notes := make([]string, 0, len(message.Images))
		for _, image := range message.Images {
			notes = append(notes, fmt.Sprintf("[%s image, %d bytes]", image.MIMEType, len(image.Data)*3/4))
		}
		message.Images = nil
		message.Content = strings.TrimSpace(message.Content + "\n" + strings.Join(notes, "\n"))
		trimmed = append(trimmed, message)
	}
	return trimmed
}

func contextInputBudget(contextWindow, maxOutput int) int {
	safetyMargin := contextSafetyMargin(contextWindow)
	outputReserve := min(maxOutput, minimumOutputReserve)
	return max(0, contextWindow-outputReserve-safetyMargin)
}

func contextOutputBudget(contextWindow, configuredMaxOutput, estimatedInput int) int {
	available := contextWindow - contextSafetyMargin(contextWindow) - estimatedInput
	return min(configuredMaxOutput, max(0, available))
}

func contextSafetyMargin(contextWindow int) int {
	return min(max(1024, contextWindow/20), contextWindow/4)
}

func (s *Service) finishRun(ctx context.Context, sessionID, runID, eventType string, payload any) error {
	defer s.sequences.Delete(runID)
	sequence, err := s.appendEvent(
		ctx,
		db.AgentEventRow{SessionID: sessionID, TurnID: runID, EventType: eventType},
		payload,
	)
	if err != nil {
		s.emit(sessionID, runID, 0, "error", map[string]any{
			"message": fmt.Sprintf("persist terminal agent event: %v", err),
		})
		return err
	}
	typeName := "status"
	if eventType == "turn/error" {
		typeName = "error"
	}
	s.emit(sessionID, runID, sequence, typeName, payload)
	return nil
}

func (s *Service) finishRunDetached(sessionID, runID, eventType string, payload any) {
	ctx, cancel := context.WithTimeout(context.Background(), persistenceTimeout)
	defer cancel()
	_ = s.finishRun(ctx, sessionID, runID, eventType, payload)
}

func (s *Service) persistenceContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent.Err() == nil {
		return context.WithTimeout(parent, persistenceTimeout)
	}
	return context.WithTimeout(context.Background(), persistenceTimeout)
}

func (s *Service) appendEvent(ctx context.Context, row db.AgentEventRow, payload any) (int64, error) {
	client, err := s.dbClient()
	if err != nil {
		return 0, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	row.Payload = string(data)
	row.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	sequence, err := client.AgentEvents.Append(ctx, row)
	if err == nil {
		if touchErr := client.AgentSessions.Touch(ctx, row.SessionID, row.CreatedAt); touchErr != nil {
			_ = infra.ReportError(s.log, touchErr, "Agent", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "agent-event", Stage: "touch-session",
				Fields: map[string]any{"sessionId": row.SessionID, "turnId": row.TurnID, "eventType": row.EventType},
			})
		}
	}
	return sequence, err
}

func (s *Service) emit(sessionID, runID string, _ int64, eventType string, payload any) {
	streamSequence, _ := s.sequences.LoadOrStore(runID, &atomic.Int64{})
	sequence := streamSequence.(*atomic.Int64).Add(1)
	if s.emitEvent != nil {
		s.emitEvent(
			"agent:update",
			AgentStreamEvent{SessionID: sessionID, RunID: runID, Sequence: sequence, Type: eventType, Payload: payload},
		)
	}
}

func (s *Service) sessionSummary(row db.AgentSessionRow) AgentSessionSummary {
	summary := AgentSessionSummary{ID: row.ID, Title: row.Title, Scope: rowScope(row), UpdatedAt: row.UpdatedAt}
	s.mu.Lock()
	worker := s.workers[row.ID]
	s.mu.Unlock()
	if worker != nil {
		worker.mu.Lock()
		summary.Running = worker.active != ""
		worker.mu.Unlock()
	}
	return summary
}

func (s *Service) dbClient() (*db.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil, errors.New("agent database is unavailable")
	}
	return s.client, nil
}

// sessionLock returns the per-session mutex that serializes revert staging, the revert commit in
// Send, run lifecycle transitions, and stable share snapshots. The lock is never removed: sessions
// are few and a stale entry costs one mutex.
func (s *Service) sessionLock(id string) *sync.Mutex {
	value, _ := s.sessionLocks.LoadOrStore(id, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (s *Service) toolOutput(sessionID, runID, callID string, output any) (any, string) {
	data, _ := json.Marshal(output)
	if len(data) <= maxToolOutput || s.appData == nil {
		return json.RawMessage(data), ""
	}
	relative := filepath.Join("agent", "artifacts", sessionID, runID+"-"+callID+".json")
	if err := s.appData.WriteFile(relative, data, 0o600); err != nil {
		return map[string]any{
			"preview": string(data[:maxToolOutput]), "truncated": true, "originalBytes": len(data),
		}, ""
	}
	return map[string]any{
		"preview": string(data[:maxToolOutput]), "truncated": true, "originalBytes": len(data), "artifact": relative,
	}, relative
}

// parseSessionRevert decodes a staged revert marker. An empty or unusable value reads as no revert,
// which matches how GetSession projects entries, so a corrupt marker never hides the conversation.
func parseSessionRevert(raw string) *AgentSessionRevert {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var staged AgentSessionRevert
	if err := json.Unmarshal([]byte(raw), &staged); err != nil {
		return nil
	}
	if staged.BoundarySequence <= 0 {
		return nil
	}
	return &staged
}

// latestSummary returns the durable summary represented by an event prefix. Internal summary
// events are already ordered, so the last summary event is the summary a request at that point used.
func latestSummary(events []db.AgentEventRow) string {
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].EventType != "summary" {
			continue
		}
		return summaryFromPayload(events[index].Payload)
	}
	return ""
}

func summaryFromPayload(payload string) string {
	var decoded struct {
		Summary string `json:"summary"`
	}
	if json.Unmarshal([]byte(payload), &decoded) != nil {
		return ""
	}
	return decoded.Summary
}

// appendRevertedTurn commits a staged revert and the replacement turn/start event atomically.
func (s *Service) appendRevertedTurn(
	ctx context.Context,
	client *db.Client,
	row db.AgentSessionRow,
	staged AgentSessionRevert,
	run queuedRun,
	payload any,
) (int64, error) {
	summaryPayload, err := client.AgentEvents.LatestSummaryBefore(ctx, row.ID, staged.BoundarySequence)
	if err != nil {
		return 0, fmt.Errorf("load agent summary before revert: %w", err)
	}
	summary := ""
	if summaryPayload != "" {
		summary = summaryFromPayload(summaryPayload)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("encode agent message: %w", err)
	}
	return client.AgentSessions.CommitRevertAndAppend(
		ctx,
		row.ID,
		staged.BoundarySequence,
		row.Revert,
		summary,
		db.AgentEventRow{
			SessionID: row.ID,
			TurnID:    run.id,
			EventType: "turn/start",
			Payload:   string(data),
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	)
}

func rowScope(row db.AgentSessionRow) AgentScope {
	scope := AgentScope{Type: row.ScopeType}
	if row.ModPath != nil {
		scope.ModPath = *row.ModPath
	}
	if row.ModName != nil {
		scope.ModName = *row.ModName
	}
	return scope
}

func agentApproval(row db.AgentApprovalRow) AgentApproval {
	approval := AgentApproval{ID: row.ID, SessionID: row.SessionID, TurnID: row.TurnID,
		ToolCallID: row.ToolCallID, ActionID: row.ActionID, Arguments: json.RawMessage(row.Arguments),
		Summary: row.Summary, Target: row.Target, Impact: row.Impact, Status: row.Status,
		Error: row.Error, CreatedAt: row.CreatedAt}
	if row.Result != "" {
		approval.Result = json.RawMessage(row.Result)
	}
	if row.DecidedAt != nil {
		approval.DecidedAt = *row.DecidedAt
	}
	if row.CompletedAt != nil {
		approval.CompletedAt = *row.CompletedAt
	}
	return approval
}

func rootID(name, path string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(path))))
	prefix := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, name)
	return strings.Trim(prefix, "-") + "-" + hex.EncodeToString(hash[:4])
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}

// messageTitle labels a new session from its first message. An image-only turn falls back to the
// attachment name so the session list stays readable.
func messageTitle(text string, images []AgentImage) string {
	if trimmed := strings.TrimSpace(text); trimmed != "" {
		return truncateRunes(trimmed, 48)
	}
	for _, image := range images {
		if name := strings.TrimSpace(image.Name); name != "" {
			return truncateRunes(name, 48)
		}
	}
	return "Image"
}

func projectEvents(events []db.AgentEventRow) []AgentChatEntry {
	type toolKey struct {
		turnID     string
		toolCallID string
	}
	keyFor := func(entry AgentChatEntry) (toolKey, bool) {
		if entry.ToolCallID == "" || (entry.Type != "tool/start" && entry.Type != "tool/end") {
			return toolKey{}, false
		}
		return toolKey{turnID: entry.TurnID, toolCallID: entry.ToolCallID}, true
	}

	projected := make([]AgentChatEntry, 0, len(events))
	latestToolEntry := make(map[toolKey]int)
	for _, event := range events {
		entry, ok := projectEvent(event)
		if !ok {
			continue
		}
		projected = append(projected, entry)
		if key, isTool := keyFor(entry); isTool {
			latestToolEntry[key] = len(projected) - 1
		}
	}

	entries := make([]AgentChatEntry, 0, len(projected))
	for index, entry := range projected {
		if key, isTool := keyFor(entry); isTool && latestToolEntry[key] != index {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func projectEvent(event db.AgentEventRow) (AgentChatEntry, bool) {
	entry := AgentChatEntry{
		Sequence:  event.Sequence,
		TurnID:    event.TurnID,
		Type:      event.EventType,
		CreatedAt: event.CreatedAt,
	}
	var payload struct {
		Text         string          `json:"text"`
		Reasoning    string          `json:"reasoning"`
		Images       []AgentImage    `json:"images"`
		ToolName     string          `json:"toolName"`
		ToolCallID   string          `json:"toolCallId"`
		Arguments    json.RawMessage `json:"arguments"`
		Result       json.RawMessage `json:"result"`
		ChangedFiles []string        `json:"changedFiles"`
		Error        string          `json:"error"`
	}
	_ = json.Unmarshal([]byte(event.Payload), &payload)
	switch event.EventType {
	case "turn/start":
		entry.Role, entry.Text, entry.Images = "user", payload.Text, payload.Images
	case "message/assistant":
		entry.Role, entry.Text, entry.Reasoning = "assistant", payload.Text, payload.Reasoning
	case "tool/start":
		var call ToolCall
		_ = json.Unmarshal([]byte(event.Payload), &call)
		entry.ToolName, entry.ToolCallID, entry.Arguments = call.Name, call.ID, call.Arguments
	case "tool/end":
		entry.ToolName, entry.ToolCallID, entry.Result = payload.ToolName, payload.ToolCallID, payload.Result
		entry.ChangedFiles, entry.Error, entry.Images = payload.ChangedFiles, payload.Error, payload.Images
	case "approval/requested":
		var approval AgentApproval
		_ = json.Unmarshal([]byte(event.Payload), &approval)
		entry.Approval = &approval
	case "turn/error", "turn/cancelled", "turn/interrupted":
		entry.Error = payload.Error
	default:
		return AgentChatEntry{}, false
	}
	return entry, true
}

// messagesFromEvents rebuilds the conversation from durable events. Images are attached only when
// the configured model accepts them, so a text-only provider never receives image parts. loadImages
// false keeps just the stored size, which lets a token estimate price images without reading the
// payloads back from disk.
func (s *Service) messagesFromEvents(events []db.AgentEventRow, supportsImages, loadImages bool) []Message {
	imagesFor := func(references []AgentImage) []MessageImage {
		if !supportsImages {
			return nil
		}
		if loadImages {
			return s.loadMessageImages(references)
		}
		return imageReferences(references)
	}
	messages := make([]Message, 0)
	restoredToolCalls := make(map[string]struct{})
	pendingToolCalls := make([]ToolCall, 0)
	completedToolCalls := make(map[string]struct{})
	flushInterruptedTools := func() {
		for _, call := range pendingToolCalls {
			if _, completed := completedToolCalls[call.ID]; completed {
				continue
			}
			payload, _ := json.Marshal(map[string]any{
				"toolName": call.Name, "toolCallId": call.ID,
				"result": map[string]any{"completed": false}, "error": interruptedToolErrorMessage,
			})
			messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Content: string(payload)})
		}
		pendingToolCalls = pendingToolCalls[:0]
		clear(completedToolCalls)
	}
	for _, event := range events {
		switch event.EventType {
		case "summary":
			var payload struct {
				CompactedMessages int `json:"compactedMessages"`
			}
			_ = json.Unmarshal([]byte(event.Payload), &payload)
			if payload.CompactedMessages > 0 && payload.CompactedMessages <= len(messages) {
				messages = messages[payload.CompactedMessages:]
			}
		case "turn/start":
			flushInterruptedTools()
			var payload struct {
				Text   string       `json:"text"`
				Images []AgentImage `json:"images"`
			}
			_ = json.Unmarshal([]byte(event.Payload), &payload)
			messages = append(messages, Message{
				Role: "user", Content: payload.Text, Images: imagesFor(payload.Images),
			})
		case "message/assistant":
			flushInterruptedTools()
			var payload struct {
				Text      string     `json:"text"`
				Reasoning string     `json:"reasoning"`
				ToolCalls []ToolCall `json:"toolCalls"`
			}
			_ = json.Unmarshal([]byte(event.Payload), &payload)
			messages = append(messages, Message{
				Role:      "assistant",
				Content:   payload.Text,
				Reasoning: payload.Reasoning,
				ToolCalls: payload.ToolCalls,
			})
			for _, call := range payload.ToolCalls {
				restoredToolCalls[call.ID] = struct{}{}
			}
			pendingToolCalls = append(pendingToolCalls, payload.ToolCalls...)
		case "tool/start":
			var call ToolCall
			_ = json.Unmarshal([]byte(event.Payload), &call)
			if _, restored := restoredToolCalls[call.ID]; restored {
				continue
			}
			messages = append(messages, Message{Role: "assistant", ToolCalls: []ToolCall{call}})
			pendingToolCalls = append(pendingToolCalls, call)
		case "tool/end":
			var payload struct {
				ToolCallID string       `json:"toolCallId"`
				Images     []AgentImage `json:"images"`
			}
			_ = json.Unmarshal([]byte(event.Payload), &payload)
			messages = append(
				messages,
				Message{
					Role: "tool", ToolCallID: payload.ToolCallID, Content: event.Payload,
					Images: imagesFor(payload.Images),
				},
			)
			completedToolCalls[payload.ToolCallID] = struct{}{}
		}
	}
	flushInterruptedTools()
	return messages
}

// TODO(agent): BlenderIntegration will discover Blender instances and guide MCP setup in a later release.
// TODO(agent): ModKnowledgeProvider will add online documentation, game-specific RAG, and image analysis later.
