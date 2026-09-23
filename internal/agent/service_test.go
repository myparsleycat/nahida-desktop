package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/hunting"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/xxmi"
)

func TestSystemPromptRoutesRequestsToMatchingSkills(t *testing.T) {
	t.Parallel()
	prompt := New(Options{}).systemPrompt(t.Context(), db.AgentSessionRow{ScopeType: "mod"}, []SandboxRoot{{
		ID: "selected", Name: "Selected Mod", Path: `C:\Mods\Character\Selected`, Importer: "ZZMI",
	}}, false)
	requiredInstructions := []string{
		"call `load_skill` for every skill whose description matches the request",
		"Load `mod-diagnosis` before diagnosing or repairing a broken or conflicting mod",
		"Intentional content creation, Blender shaping, and source-model conversion",
	}
	for _, required := range requiredInstructions {
		if !strings.Contains(prompt, required) {
			t.Fatalf("system prompt is missing %q", required)
		}
	}
}

func TestCancelAcceptsRestoredRunSentinel(t *testing.T) {
	t.Parallel()
	service := New(Options{})
	cancelled := false
	service.workers["session"] = &sessionWorker{
		active: "actual-run-id",
		cancel: func() { cancelled = true },
	}
	if err := service.Cancel("session", "restored"); err != nil {
		t.Fatalf("Cancel = %v", err)
	}
	if !cancelled {
		t.Fatal("Cancel did not stop the restored active run")
	}
}

func TestCancelWaitsForActiveRunCleanup(t *testing.T) {
	t.Parallel()
	service := New(Options{})
	done := make(chan struct{})
	cancelled := make(chan struct{})
	service.workers["session"] = &sessionWorker{
		active: "run",
		cancel: func() { close(cancelled) },
		done:   done,
	}
	returned := make(chan struct{})
	go func() {
		_ = service.Cancel("session", "run")
		close(returned)
	}()

	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Cancel did not signal the active run")
	}
	select {
	case <-returned:
		t.Fatal("Cancel returned before cleanup completed")
	default:
	}
	close(done)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("Cancel did not return after cleanup completed")
	}
}

func TestCancelReturnsWhenRunDoesNotFinish(t *testing.T) {
	t.Parallel()
	service := New(Options{})
	service.cancelWaitTimeout = 20 * time.Millisecond
	cancelled := make(chan struct{})
	service.workers["session"] = &sessionWorker{
		active: "run",
		cancel: func() { close(cancelled) },
		done:   make(chan struct{}),
	}
	returned := make(chan error, 1)
	go func() { returned <- service.Cancel("session", "run") }()

	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("Cancel = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Cancel blocked on an unfinished run")
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("Cancel did not signal the run")
	}
}

func TestSystemPromptUsesAppLanguage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	settings := setting.New(client)
	if err := settings.SetLanguage(ctx, "ja"); err != nil {
		t.Fatal(err)
	}

	prompt := New(Options{Setting: settings}).systemPrompt(
		ctx,
		db.AgentSessionRow{ScopeType: "global"},
		[]SandboxRoot{},
		true,
	)
	if !strings.Contains(prompt, "Respond in Japanese (ja), the language selected in Nahida Desktop") {
		t.Fatalf("system prompt does not use app language: %q", prompt)
	}
}

func TestServiceValidatesApprovalToolContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	session := db.AgentSessionRow{
		ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1",
	}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}
	call := ToolCall{ID: "tool", Name: "run_desktop_action", Arguments: json.RawMessage(`{"actionId":"x"}`)}
	callJSON, _ := json.Marshal(call)
	if _, err := client.AgentEvents.Append(ctx, db.AgentEventRow{
		SessionID: session.ID, TurnID: "turn", EventType: "tool/start", Payload: string(callJSON), CreatedAt: "1",
	}); err != nil {
		t.Fatal(err)
	}
	row := db.AgentApprovalRow{
		ID: "approval", SessionID: session.ID, TurnID: "turn", ToolCallID: call.ID, ActionID: "x",
		Arguments: `{}`, Summary: "Test", Status: "pending", CreatedAt: "1",
	}
	if err := client.AgentApprovals.Insert(ctx, row); err != nil {
		t.Fatal(err)
	}
	approvalJSON, _ := json.Marshal(agentApproval(row))
	if _, err := client.AgentEvents.Append(ctx, db.AgentEventRow{
		SessionID: session.ID, TurnID: "turn", EventType: "approval/requested", Payload: string(approvalJSON),
		CreatedAt: "1",
	}); err != nil {
		t.Fatal(err)
	}
	service := New(Options{})
	if err := service.validateApprovalContext(ctx, client, &row); err != nil {
		t.Fatalf("valid context rejected: %v", err)
	}

	row.ToolCallID = "different-tool"
	if err := service.validateApprovalContext(ctx, client, &row); err == nil {
		t.Fatal("mismatched tool call unexpectedly accepted")
	}
}

func TestServiceSendRejectsActiveApproval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(t.TempDir(), "Mods")
	if err := os.MkdirAll(mods, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: "Game", ModFolderPath: mods}); err != nil {
		t.Fatal(err)
	}
	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateSession(ctx, AgentScope{Type: "global"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.AgentApprovals.Insert(ctx, db.AgentApprovalRow{
		ID: "approval", SessionID: session.ID, TurnID: "turn", ToolCallID: "tool", ActionID: "test",
		Arguments: `{}`, Summary: "Test", Status: "executing", CreatedAt: "1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Send(ctx, session.ID, "continue", nil); err == nil {
		t.Fatal("send unexpectedly accepted while approval was executing")
	}
}

func TestServiceGetSessionKeepsLatestToolState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	session := db.AgentSessionRow{
		ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1",
	}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}
	for _, event := range []db.AgentEventRow{
		{SessionID: session.ID, TurnID: "turn", EventType: "tool/start", Payload: `{"id":"tool","name":"read_file"}`, CreatedAt: "1"},
		{SessionID: session.ID, TurnID: "turn", EventType: "tool/end", Payload: `{"toolName":"read_file","toolCallId":"tool"}`, CreatedAt: "2"},
	} {
		if _, err := client.AgentEvents.Append(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].Type != "tool/end" ||
		snapshot.Entries[0].ToolCallID != "tool" {
		t.Fatalf("entries = %#v", snapshot.Entries)
	}
}

func TestServiceRepairsInterruptedApprovalEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	session := db.AgentSessionRow{
		ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1",
	}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := client.AgentApprovals.Insert(ctx, db.AgentApprovalRow{
		ID: "approval", SessionID: session.ID, TurnID: "turn", ToolCallID: "tool",
		ActionID: "sandbox.apply_patch", Arguments: `{}`, Summary: "Test", Status: "executing", CreatedAt: "1",
	}); err != nil {
		t.Fatal(err)
	}

	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}

	approval, err := client.AgentApprovals.Get(ctx, "approval")
	if err != nil || approval == nil || approval.Status != "failed" ||
		approval.Error != interruptedApprovalErrorMessage {
		t.Fatalf("approval = %#v, %v", approval, err)
	}
	events, err := client.AgentEvents.List(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	toolEnds, updates := 0, 0
	for _, event := range events {
		switch event.EventType {
		case "tool/end":
			toolEnds++
			var payload struct {
				ToolName string `json:"toolName"`
				Error    string `json:"error"`
			}
			if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil ||
				payload.ToolName != "apply_patch" || payload.Error != interruptedApprovalErrorMessage {
				t.Fatalf("tool payload = %#v, %v", payload, err)
			}
		case "approval/updated":
			updates++
		}
	}
	if toolEnds != 1 || updates != 1 {
		t.Fatalf("tool ends = %d, approval updates = %d", toolEnds, updates)
	}
}

func TestProjectEventsKeepsLatestToolStatePerTurn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		events    []db.AgentEventRow
		sequences []int64
		types     []string
	}{
		{
			name: "completed tool",
			events: []db.AgentEventRow{
				{Sequence: 1, TurnID: "turn", EventType: "tool/start", Payload: `{"id":"tool","name":"read_file"}`},
				{Sequence: 2, TurnID: "turn", EventType: "tool/end", Payload: `{"toolCallId":"tool"}`},
			},
			sequences: []int64{2},
			types:     []string{"tool/end"},
		},
		{
			name: "running tool",
			events: []db.AgentEventRow{
				{Sequence: 1, TurnID: "turn", EventType: "tool/start", Payload: `{"id":"tool","name":"read_file"}`},
			},
			sequences: []int64{1},
			types:     []string{"tool/start"},
		},
		{
			name: "same call ID in different turns",
			events: []db.AgentEventRow{
				{Sequence: 1, TurnID: "first", EventType: "tool/start", Payload: `{"id":"tool","name":"read_file"}`},
				{Sequence: 2, TurnID: "first", EventType: "tool/end", Payload: `{"toolCallId":"tool"}`},
				{Sequence: 3, TurnID: "second", EventType: "tool/start", Payload: `{"id":"tool","name":"read_file"}`},
			},
			sequences: []int64{2, 3},
			types:     []string{"tool/end", "tool/start"},
		},
		{
			name: "duplicate terminal event",
			events: []db.AgentEventRow{
				{Sequence: 1, TurnID: "turn", EventType: "tool/start", Payload: `{"id":"tool","name":"read_file"}`},
				{Sequence: 2, TurnID: "turn", EventType: "tool/end", Payload: `{"toolCallId":"tool"}`},
				{
					Sequence:  3,
					TurnID:    "turn",
					EventType: "tool/end",
					Payload:   `{"toolCallId":"tool","error":"interrupted"}`,
				},
			},
			sequences: []int64{3},
			types:     []string{"tool/end"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			entries := projectEvents(test.events)
			if len(entries) != len(test.types) {
				t.Fatalf("entries = %#v", entries)
			}
			for index := range entries {
				if entries[index].Sequence != test.sequences[index] || entries[index].Type != test.types[index] {
					t.Fatalf("entry %d = %#v", index, entries[index])
				}
			}
		})
	}
}

func TestMessagesFromEventsPreservesToolResultEnvelope(t *testing.T) {
	t.Parallel()
	events := []db.AgentEventRow{{
		EventType: "tool/end",
		Payload:   `{"toolCallId":"tool","result":{"ok":false},"error":"failed","changedFiles":["a"]}`,
	}}

	messages := New(Options{}).messagesFromEvents(events, true, true)
	if len(messages) != 1 || messages[0].ToolCallID != "tool" {
		t.Fatalf("messages = %#v", messages)
	}
	var payload struct {
		Error        string   `json:"error"`
		ChangedFiles []string `json:"changedFiles"`
	}
	if err := json.Unmarshal([]byte(messages[0].Content), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error != "failed" || len(payload.ChangedFiles) != 1 || payload.ChangedFiles[0] != "a" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestMessagesFromEventsRestoresReasoningWithoutDuplicatingToolCalls(t *testing.T) {
	t.Parallel()
	events := []db.AgentEventRow{
		{
			EventType: "message/assistant",
			Payload: `{"text":"checking","reasoning":"inspect files","toolCalls":[` +
				`{"id":"tool","name":"read_file","arguments":{"rootId":"root","path":"mod.ini"}}]}`,
		},
		{
			EventType: "tool/start",
			Payload:   `{"id":"tool","name":"read_file","arguments":{"rootId":"root","path":"mod.ini"}}`,
		},
		{
			EventType: "tool/end",
			Payload:   `{"toolCallId":"tool","result":{"ok":true}}`,
		},
	}

	messages := New(Options{}).messagesFromEvents(events, true, true)
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	assistant := messages[0]
	if assistant.Role != "assistant" || assistant.Content != "checking" || assistant.Reasoning != "inspect files" ||
		len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "tool" {
		t.Fatalf("assistant = %#v", assistant)
	}
	if messages[1].Role != "tool" || messages[1].ToolCallID != "tool" {
		t.Fatalf("tool result = %#v", messages[1])
	}
}

func TestMessagesFromEventsRestoresLegacyToolCalls(t *testing.T) {
	t.Parallel()
	events := []db.AgentEventRow{
		{EventType: "message/assistant", Payload: `{"text":"checking"}`},
		{
			EventType: "tool/start",
			Payload:   `{"id":"tool","name":"read_file","arguments":{"rootId":"root","path":"mod.ini"}}`,
		},
	}

	messages := New(Options{}).messagesFromEvents(events, true, true)
	if len(messages) != 3 || len(messages[1].ToolCalls) != 1 || messages[1].ToolCalls[0].ID != "tool" ||
		messages[2].Role != "tool" || messages[2].ToolCallID != "tool" ||
		!strings.Contains(messages[2].Content, interruptedToolErrorMessage) {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestEstimateTokensIncludesReasoning(t *testing.T) {
	t.Parallel()
	withoutReasoning := estimateTokens("system", []Message{{Role: "assistant", Content: "answer"}})
	withReasoning := estimateTokens("system", []Message{{
		Role: "assistant", Content: "answer", Reasoning: strings.Repeat("r", 400),
	}})

	if withReasoning-withoutReasoning != 100 {
		t.Fatalf("reasoning token estimate delta = %d, want 100", withReasoning-withoutReasoning)
	}
}

func TestEstimateTokensIncludesToolsAndNonASCIIText(t *testing.T) {
	t.Parallel()
	base := estimateTokens("system", []Message{{Role: "user", Content: "hello"}})
	withKorean := estimateTokens("system", []Message{{Role: "user", Content: strings.Repeat("한", 40)}})
	withTools := estimateTokens(
		"system",
		[]Message{{Role: "user", Content: "hello"}},
		[]ToolDefinition{{Name: "read_file", Description: strings.Repeat("schema", 40)}},
	)

	if withKorean-base < 30 {
		t.Fatalf("non-ASCII token estimate delta = %d, want a conservative estimate", withKorean-base)
	}
	if withTools <= base {
		t.Fatalf("tool-aware estimate = %d, base = %d", withTools, base)
	}
}

func TestContextInputBudgetReservesOutputAndSafetyMargin(t *testing.T) {
	t.Parallel()
	if got := contextInputBudget(32_000, 4_000); got != 26_400 {
		t.Fatalf("context input budget = %d, want 26400", got)
	}
	if got := contextInputBudget(400_000, 384_000); got != 375_904 {
		t.Fatalf("large-output context input budget = %d, want 375904", got)
	}
}

func TestContextOutputBudgetCapsConfiguredLimitToAvailableContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                             string
		contextWindow, configured, input int
		want                             int
	}{
		{name: "configured limit fits", contextWindow: 32_000, configured: 4_000, input: 10_000, want: 4_000},
		{
			name:          "configured limit is reduced",
			contextWindow: 400_000,
			configured:    384_000,
			input:         20_000,
			want:          360_000,
		},
		{name: "input consumes available context", contextWindow: 8_000, configured: 4_000, input: 7_000, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := contextOutputBudget(test.contextWindow, test.configured, test.input); got != test.want {
				t.Fatalf("context output budget = %d, want %d", got, test.want)
			}
		})
	}
}

func TestAppendSkippedToolCallsPersistsPairedResults(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.AgentSessions.Insert(ctx, db.AgentSessionRow{
		ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1",
	}); err != nil {
		t.Fatal(err)
	}
	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	calls := []ToolCall{
		{ID: "second", Name: "read_file", Arguments: json.RawMessage(`{"path":"a"}`)},
		{ID: "third", Name: "search_text", Arguments: json.RawMessage(`{"pattern":"b"}`)},
	}
	if err := service.appendSkippedToolCalls(ctx, "session", "turn", calls); err != nil {
		t.Fatal(err)
	}
	events, err := client.AgentEvents.List(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %#v", events)
	}
	messages := New(Options{}).messagesFromEvents(events, true, true)
	if len(messages) != 4 || messages[0].ToolCalls[0].ID != "second" || messages[1].ToolCallID != "second" ||
		messages[2].ToolCalls[0].ID != "third" || messages[3].ToolCallID != "third" {
		t.Fatalf("paired messages = %#v", messages)
	}
	if !strings.Contains(messages[1].Content, skippedToolErrorMessage) ||
		!strings.Contains(messages[3].Content, skippedToolErrorMessage) {
		t.Fatalf("skipped results = %#v", messages)
	}
}

func TestAppendEventPreservesSessionTitle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.AgentSessions.Insert(ctx, db.AgentSessionRow{
		ID: "session", ScopeType: "global", Title: "Keep this title", CreatedAt: "1", UpdatedAt: "1",
	}); err != nil {
		t.Fatal(err)
	}
	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	if _, err := service.appendEvent(ctx, db.AgentEventRow{
		SessionID: "session", TurnID: "turn", EventType: "turn/start",
	}, map[string]any{"text": "hello"}); err != nil {
		t.Fatal(err)
	}
	row, err := client.AgentSessions.Get(ctx, "session")
	if err != nil || row == nil || row.Title != "Keep this title" {
		t.Fatalf("session = %#v, %v", row, err)
	}
}

func TestSessionScopesResolveCurrentGameRoots(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	firstRoot := filepath.Join(t.TempDir(), "GameOne", "Mods")
	secondRoot := filepath.Join(t.TempDir(), "GameTwo", "Mods")
	selectedMod := filepath.Join(firstRoot, "Selected Mod")
	for _, path := range []string{selectedMod, secondRoot} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	importer := "GIMI"
	if err := client.GamePaths.Insert(
		ctx,
		db.GamePathRow{Game: "Game One", ModFolderPath: firstRoot, Importer: &importer},
	); err != nil {
		t.Fatal(err)
	}
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: "Game Two", ModFolderPath: secondRoot}); err != nil {
		t.Fatal(err)
	}
	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	data, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UseAppData(data); err != nil {
		t.Fatal(err)
	}

	global, err := service.CreateSession(ctx, AgentScope{Type: "global"})
	if err != nil {
		t.Fatal(err)
	}
	globalSnapshot, err := service.GetSession(ctx, global.ID)
	if err != nil || len(globalSnapshot.Roots) != 2 {
		t.Fatalf("global roots = %#v, %v", globalSnapshot.Roots, err)
	}
	mod, err := service.CreateSession(ctx, AgentScope{Type: "mod", ModPath: selectedMod, ModName: "Selected"})
	if err != nil {
		t.Fatal(err)
	}
	modSnapshot, err := service.GetSession(ctx, mod.ID)
	canonicalSelected, canonicalErr := canonicalExistingDir(selectedMod)
	if canonicalErr != nil {
		t.Fatalf("canonicalExistingDir(%q): %v", selectedMod, canonicalErr)
	}
	if err != nil || len(modSnapshot.Roots) != 1 || modSnapshot.Roots[0].Path != canonicalSelected {
		t.Fatalf("mod roots = %#v, expected = %q, %v", modSnapshot.Roots, canonicalSelected, err)
	}
	if _, err := service.CreateSession(ctx, AgentScope{Type: "mod", ModPath: t.TempDir()}); err == nil {
		t.Fatal("outside mod scope unexpectedly succeeded")
	}
}

func TestServiceRevertStagesHidesAndUnreverts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(t.TempDir(), "Mods")
	if err := os.MkdirAll(mods, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: "Game", ModFolderPath: mods}); err != nil {
		t.Fatal(err)
	}
	// Install the client before writing events: UseClient seals turns that look interrupted, and a
	// test turn has no terminal event.
	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	session := db.AgentSessionRow{
		ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1",
	}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}
	settings, err := readSettings(ctx, client, service.crypto)
	if err != nil {
		t.Fatal(err)
	}
	firstUsage, err := json.Marshal(contextUsagePayload{
		Provider: settings.Provider, Model: settings.Model, RouteKey: contextRouteKey(settings), InputTokens: 1_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondUsage, err := json.Marshal(contextUsagePayload{
		Provider: settings.Provider, Model: settings.Model, RouteKey: contextRouteKey(settings), InputTokens: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []db.AgentEventRow{
		{SessionID: session.ID, TurnID: "first", EventType: "turn/start", Payload: `{"text":"one"}`, CreatedAt: "1"},
		{SessionID: session.ID, TurnID: "first", EventType: "message/assistant", Payload: `{"text":"answer"}`, CreatedAt: "2"},
		{
			SessionID: session.ID, TurnID: "first", EventType: contextUsageEventType,
			Payload: string(firstUsage), CreatedAt: "3",
		},
		{
			SessionID: session.ID, TurnID: "second", EventType: "turn/start",
			Payload: `{"text":"two two two two two two two two two two"}`, CreatedAt: "4",
		},
		{
			SessionID: session.ID, TurnID: "second", EventType: "message/assistant",
			Payload: `{"text":"answer answer answer answer answer answer answer answer answer answer"}`, CreatedAt: "5",
		},
		{
			SessionID: session.ID, TurnID: "second", EventType: contextUsageEventType,
			Payload: string(secondUsage), CreatedAt: "6",
		},
	} {
		if _, err := client.AgentEvents.Append(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	storedEvents, err := client.AgentEvents.List(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, roots, err := service.resolveScope(ctx, rowScope(session))
	if err != nil {
		t.Fatal(err)
	}
	effectiveEvents := storedEvents[:3]
	wantUsage, ok := buildContextUsage(
		settings.ContextWindowSize, contextRouteKey(settings),
		service.systemPrompt(ctx, session, roots, settings.SupportsImages),
		service.messagesFromEvents(effectiveEvents, settings.SupportsImages, false),
		builtInToolDefinitions(), parseContextAnchor(effectiveEvents),
	)
	if !ok {
		t.Fatal("expected context usage for the staged conversation")
	}

	staged, err := service.RevertSession(ctx, session.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Revert == nil || staged.Revert.BoundarySequence != 4 || staged.Revert.BoundaryTurnID != "second" ||
		staged.Revert.RevertedCount != 2 {
		t.Fatalf("staged revert = %#v", staged.Revert)
	}
	if staged.ContextUsage == nil || *staged.ContextUsage != wantUsage {
		t.Fatalf("staged context usage = %#v, want %#v", staged.ContextUsage, wantUsage)
	}
	for _, entry := range staged.Entries {
		want := entry.Sequence >= 4
		if entry.Reverted != want {
			t.Fatalf("entry %d reverted = %v, want %v", entry.Sequence, entry.Reverted, want)
		}
	}

	if _, err := service.RevertSession(ctx, session.ID, 2); err == nil {
		t.Fatal("revert accepted a non user-message boundary")
	}

	cleared, err := service.UnrevertSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Revert != nil {
		t.Fatalf("revert survived unrevert: %#v", cleared.Revert)
	}
	for _, entry := range cleared.Entries {
		if entry.Reverted {
			t.Fatalf("entry %d still reverted after unrevert", entry.Sequence)
		}
	}
}

func TestServiceRevertContextUsageUsesSurvivingSummary(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(t.TempDir(), "Mods")
	if err := os.MkdirAll(mods, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: "Game", ModFolderPath: mods}); err != nil {
		t.Fatal(err)
	}
	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	session := db.AgentSessionRow{
		ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1",
	}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}

	const survivingSummary = "surviving summary"
	staleSummary := strings.Repeat("future summary ", 400)
	if _, err := client.AgentEvents.AppendAndUpdateSummary(ctx, db.AgentEventRow{
		SessionID: session.ID, TurnID: "first", EventType: "summary",
		Payload: `{"summary":"surviving summary","compactedMessages":0}`, CreatedAt: "2",
	}, survivingSummary); err != nil {
		t.Fatal(err)
	}
	for _, event := range []db.AgentEventRow{
		{SessionID: session.ID, TurnID: "first", EventType: "turn/start", Payload: `{"text":"one"}`, CreatedAt: "3"},
		{
			SessionID: session.ID, TurnID: "first", EventType: "message/assistant",
			Payload: `{"text":"answer"}`, CreatedAt: "4",
		},
		{SessionID: session.ID, TurnID: "second", EventType: "turn/start", Payload: `{"text":"two"}`, CreatedAt: "5"},
	} {
		if _, err := client.AgentEvents.Append(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	stalePayload, err := json.Marshal(map[string]any{"summary": staleSummary, "compactedMessages": 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentEvents.AppendAndUpdateSummary(ctx, db.AgentEventRow{
		SessionID: session.ID, TurnID: "second", EventType: "summary",
		Payload: string(stalePayload), CreatedAt: "6",
	}, staleSummary); err != nil {
		t.Fatal(err)
	}

	staged, err := service.RevertSession(ctx, session.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if staged.ContextUsage == nil {
		t.Fatal("staged context usage is nil")
	}
	settings, err := readSettings(ctx, client, service.crypto)
	if err != nil {
		t.Fatal(err)
	}
	_, roots, err := service.resolveScope(ctx, rowScope(session))
	if err != nil {
		t.Fatal(err)
	}
	effectiveRow := session
	effectiveRow.DurableSummary = survivingSummary
	wantSystemTokens := estimateTextTokens(service.systemPrompt(ctx, effectiveRow, roots, settings.SupportsImages))
	if staged.ContextUsage.SystemTokens != wantSystemTokens {
		t.Fatalf(
			"systemTokens = %d, want %d from the surviving summary",
			staged.ContextUsage.SystemTokens,
			wantSystemTokens,
		)
	}
	staleRow := session
	staleRow.DurableSummary = staleSummary
	staleSystemTokens := estimateTextTokens(service.systemPrompt(ctx, staleRow, roots, settings.SupportsImages))
	if staged.ContextUsage.SystemTokens == staleSystemTokens {
		t.Fatal("staged context usage retained the reverted summary")
	}
}

func TestServiceCommitRevertKeepsSurvivingSummary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	// Install the client before writing events: UseClient seals turns that look interrupted.
	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	session := db.AgentSessionRow{
		ID: "session", ScopeType: "global", Title: "Test", DurableSummary: "stale", CreatedAt: "1", UpdatedAt: "1",
	}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}
	for _, event := range []db.AgentEventRow{
		{SessionID: session.ID, TurnID: "first", EventType: "turn/start", Payload: `{"text":"one"}`, CreatedAt: "1"},
		{SessionID: session.ID, TurnID: "first", EventType: "summary",
			Payload: `{"summary":"surviving","compactedMessages":1}`, CreatedAt: "2"},
		{SessionID: session.ID, TurnID: "second", EventType: "turn/start", Payload: `{"text":"two"}`, CreatedAt: "3"},
		{SessionID: session.ID, TurnID: "second", EventType: "tool/start",
			Payload: `{"id":"tool","name":"apply_patch"}`, CreatedAt: "4"},
	} {
		if _, err := client.AgentEvents.Append(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.AgentApprovals.Insert(ctx, db.AgentApprovalRow{
		ID: "approval", SessionID: session.ID, TurnID: "second", ToolCallID: "tool",
		ActionID: "sandbox.apply_patch", Arguments: `{}`, Summary: "Test", Status: "completed", CreatedAt: "4",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RevertSession(ctx, session.ID, 3); err != nil {
		t.Fatal(err)
	}
	row, err := client.AgentSessions.Get(ctx, session.ID)
	if err != nil || row == nil {
		t.Fatalf("session = %#v, %v", row, err)
	}

	staged := parseSessionRevert(row.Revert)
	if staged == nil {
		t.Fatal("revert marker did not parse")
	}
	run := queuedRun{id: "replacement", text: "three"}
	sequence, err := service.appendRevertedTurn(ctx, client, *row, *staged, run, map[string]any{"text": run.text})
	if err != nil {
		t.Fatal(err)
	}
	if sequence != 3 {
		t.Fatalf("replacement sequence = %d, want 3", sequence)
	}
	updated, err := client.AgentSessions.Get(ctx, session.ID)
	if err != nil || updated == nil {
		t.Fatalf("session = %#v, %v", updated, err)
	}
	if updated.Revert != "" || updated.DurableSummary != "surviving" {
		t.Fatalf("committed session = %#v", updated)
	}
	events, err := client.AgentEvents.List(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].EventType != "turn/start" || events[1].EventType != "summary" ||
		events[2].TurnID != run.id || events[2].EventType != "turn/start" {
		t.Fatalf("surviving events = %#v", events)
	}
	approval, err := client.AgentApprovals.Get(ctx, "approval")
	if err != nil || approval != nil {
		t.Fatalf("approval survived commit: %#v, %v", approval, err)
	}

	// A stale second send must fail without inserting an event or changing the committed session.
	if _, err := client.AgentSessions.CommitRevertAndAppend(
		ctx,
		session.ID,
		3,
		row.Revert,
		"stale",
		db.AgentEventRow{
			SessionID: session.ID,
			TurnID:    "stale",
			EventType: "turn/start",
			Payload:   `{}`,
			CreatedAt: "9",
		},
	); err == nil {
		t.Fatal("stale revert commit unexpectedly succeeded")
	}
	after, err := client.AgentEvents.List(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 3 {
		t.Fatalf("stale commit changed events = %#v", after)
	}
	reloaded, err := client.AgentSessions.Get(ctx, session.ID)
	if err != nil || reloaded == nil {
		t.Fatalf("session = %#v, %v", reloaded, err)
	}
	if reloaded.DurableSummary != "surviving" {
		t.Fatalf("stale commit overwrote summary = %#v", reloaded)
	}
}

func TestDeleteSessionKeepsWorkerWhenHuntingRestoreFails(t *testing.T) {
	root := t.TempDir()
	contents := `[Hunting]
hunting = 2
marking_mode = skip
marking_actions = clipboard
toggle_hunting = no_modifiers VK_NUMPAD0
next_pixelshader = no_modifiers VK_F8
mark_pixelshader = no_modifiers VK_F9
`
	if err := os.WriteFile(filepath.Join(root, "d3dx.ini"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	input := &fakeHuntingInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	huntingService := hunting.New(hunting.Options{
		Importer: fakeHuntingImporter{root: root}, Input: input, Screen: fakeHuntingScreen{},
		IdleTimeout: time.Hour, SessionTimeout: time.Hour,
		Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := huntingService.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}

	service := New(Options{Hunting: huntingService})
	service.client = client
	worker := &sessionWorker{active: "run", cancel: func() {}}
	service.workers["session"] = worker

	if _, err := huntingService.Begin(t.Context(), "session", "GIMI", 42, true); err != nil {
		t.Fatalf("Begin = %v", err)
	}
	input.setAcquireError(errors.New("game window is gone"))

	if err := service.DeleteSession(t.Context(), "session"); !errors.Is(err, hunting.ErrCleanupIncomplete) {
		t.Fatalf("DeleteSession = %v", err)
	}
	if service.workers["session"] != worker {
		t.Fatal("DeleteSession removed the worker before the hunting restore succeeded")
	}
	worker.mu.Lock()
	paused := worker.paused
	worker.mu.Unlock()
	if paused {
		t.Fatal("DeleteSession left the worker paused after the hunting restore failed")
	}

	input.setAcquireError(nil)
	if err := service.DeleteSession(t.Context(), "session"); err != nil {
		t.Fatalf("retry DeleteSession = %v", err)
	}
	if service.workers["session"] != nil {
		t.Fatal("DeleteSession kept the worker after the hunting restore succeeded")
	}
}

func TestDeleteSessionContinuesWhenHuntingRestoreIsNotRetryable(t *testing.T) {
	root := t.TempDir()
	contents := `[Hunting]
hunting = 2
marking_mode = skip
marking_actions = clipboard
toggle_hunting = no_modifiers VK_NUMPAD0
next_pixelshader = no_modifiers VK_F8
mark_pixelshader = no_modifiers VK_F9
`
	if err := os.WriteFile(filepath.Join(root, "d3dx.ini"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	// The game exited with its window, so the hunting service releases the
	// session and reports the cleanup as no longer retryable.
	input := &fakeHuntingInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}, processAlive: false}
	huntingService := hunting.New(hunting.Options{
		Importer: fakeHuntingImporter{root: root}, Input: input, Screen: fakeHuntingScreen{},
		IdleTimeout: time.Hour, SessionTimeout: time.Hour,
		Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := huntingService.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}

	service := New(Options{Hunting: huntingService})
	service.client = client
	worker := &sessionWorker{active: "run", cancel: func() {}}
	service.workers["session"] = worker

	if _, err := huntingService.Begin(t.Context(), "session", "GIMI", 42, true); err != nil {
		t.Fatalf("Begin = %v", err)
	}
	input.setWindows(nil)
	input.setAcquireError(errors.New("game window is gone"))

	if err := service.DeleteSession(t.Context(), "session"); err != nil {
		t.Fatalf("DeleteSession = %v", err)
	}
	if service.workers["session"] != nil {
		t.Fatal("DeleteSession kept the worker although the cleanup cannot be retried")
	}
}

func TestDeleteSessionWaitsForInFlightHuntingBegin(t *testing.T) {
	root := t.TempDir()
	contents := `[Hunting]
hunting = 2
marking_mode = skip
marking_actions = clipboard
toggle_hunting = no_modifiers VK_NUMPAD0
next_pixelshader = no_modifiers VK_F8
mark_pixelshader = no_modifiers VK_F9
`
	if err := os.WriteFile(filepath.Join(root, "d3dx.ini"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	// The approved hunting.begin blocks inside its resolution stage, after the
	// deletion already started, and ignores context cancellation like a config
	// read that is already in progress.
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	importer := &blockingHuntingImporter{root: root, entered: make(chan struct{}), release: release}
	input := &fakeHuntingInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	huntingService := hunting.New(hunting.Options{
		Importer: importer, Input: input, Screen: fakeHuntingScreen{},
		IdleTimeout: time.Hour, SessionTimeout: time.Hour,
		Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := huntingService.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})
	// Release the blocked resolve before the service stops so a failed test
	// cannot leave the begin goroutine waiting forever.
	t.Cleanup(unblock)
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}

	service := New(Options{Hunting: huntingService})
	service.client = client
	beginCtx, cancelBegin := context.WithCancel(t.Context())
	defer cancelBegin()
	beginDone := make(chan struct{})
	beginFinished := make(chan struct{})
	// The worker keeps a run active until it returns, so the test holds the run
	// in flight after Begin itself returned to observe the deletion waiting.
	releaseRun := make(chan struct{})
	var releaseRunOnce sync.Once
	releaseActiveRun := func() { releaseRunOnce.Do(func() { close(releaseRun) }) }
	t.Cleanup(releaseActiveRun)
	service.workers["session"] = &sessionWorker{active: "begin", cancel: cancelBegin, done: beginDone}
	go func() {
		defer close(beginDone)
		_, _ = huntingService.Begin(beginCtx, "session", "GIMI", 42, true)
		close(beginFinished)
		<-releaseRun
	}()
	select {
	case <-importer.entered:
	case <-time.After(time.Second):
		t.Fatal("hunting begin did not start resolving")
	}

	deleted := make(chan error, 1)
	go func() { deleted <- service.DeleteSession(t.Context(), "session") }()
	select {
	case <-beginCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("DeleteSession did not cancel the in-flight hunting begin")
	}

	unblock()
	select {
	case <-beginFinished:
	case <-time.After(time.Second):
		t.Fatal("hunting begin did not return after it was unblocked")
	}
	// The run is still active, so deletion must still be waiting for it.
	select {
	case err := <-deleted:
		t.Fatalf("DeleteSession returned before the in-flight hunting begin finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	releaseActiveRun()
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatalf("DeleteSession = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DeleteSession did not finish after the hunting begin stopped")
	}
	if snapshot := huntingService.Current("session"); snapshot != nil {
		t.Fatalf("hunting session survived deletion: %+v", snapshot)
	}
	if service.workers["session"] != nil {
		t.Fatal("DeleteSession kept the worker after the hunting restore succeeded")
	}
}

type blockingHuntingImporter struct {
	root    string
	entered chan struct{}
	release chan struct{}
}

func (f *blockingHuntingImporter) ResolveHuntingRuntime(
	context.Context,
	string,
) (xxmi.HuntingRuntime, error) {
	close(f.entered)
	<-f.release
	return xxmi.HuntingRuntime{
		ImporterKey: "GIMI", ImporterFolder: f.root, INIPath: filepath.Join(f.root, "d3dx.ini"),
		GameEXENames: []string{"game.exe"},
	}, nil
}

type fakeHuntingImporter struct {
	root string
}

func (f fakeHuntingImporter) ResolveHuntingRuntime(context.Context, string) (xxmi.HuntingRuntime, error) {
	return xxmi.HuntingRuntime{
		ImporterKey: "GIMI", ImporterFolder: f.root, INIPath: filepath.Join(f.root, "d3dx.ini"),
		GameEXENames: []string{"game.exe"},
	}, nil
}

type fakeHuntingInput struct {
	mu           sync.Mutex
	windows      []platform.WindowInfo
	acquireErr   error
	processAlive bool
}

func (f *fakeHuntingInput) ListWindows(
	_ context.Context,
	filter platform.WindowFilter,
) ([]platform.WindowInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]platform.WindowInfo, 0, len(f.windows))
	for _, window := range f.windows {
		if filter.PID == 0 || filter.PID == window.PID {
			result = append(result, window)
		}
	}
	return result, nil
}

func (f *fakeHuntingInput) AcquireForeground(
	_ context.Context,
	_ platform.WindowTarget,
) (platform.ForegroundController, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	return fakeHuntingLease{}, nil
}

func (f *fakeHuntingInput) ValidateKeys([]string, []string) error { return nil }

func (f *fakeHuntingInput) ProcessAlive(uint32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.processAlive
}

func (f *fakeHuntingInput) setWindows(windows []platform.WindowInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.windows = windows
}

func (f *fakeHuntingInput) setAcquireError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquireErr = err
}

type fakeHuntingLease struct{}

func (fakeHuntingLease) Window() platform.WindowInfo {
	return platform.WindowInfo{PID: 42, Title: "Game"}
}

func (fakeHuntingLease) SendKeys(context.Context, []string) (platform.KeyResult, error) {
	return platform.KeyResult{}, nil
}

func (fakeHuntingLease) PressedKeys([]string) ([]string, error) { return nil, nil }

func (fakeHuntingLease) Close() error { return nil }

type fakeHuntingScreen struct{}

func (fakeHuntingScreen) CaptureWindow(
	_ context.Context,
	request platform.CaptureRequest,
) (platform.CaptureResult, error) {
	return platform.CaptureResult{
		Window: platform.WindowInfo{PID: request.Target.PID, Title: "Game"},
		Width:  100, Height: 100, Scale: 1, PNG: []byte("png"),
	}, nil
}
