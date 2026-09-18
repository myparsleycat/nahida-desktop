package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/setting"
)

func TestSystemPromptRoutesRequestsToMatchingSkills(t *testing.T) {
	t.Parallel()
	prompt := New(Options{}).systemPrompt(t.Context(), db.AgentSessionRow{ScopeType: "mod"}, []SandboxRoot{{
		ID: "selected", Name: "Selected Mod", Path: `C:\Mods\Character\Selected`, Importer: "ZZMI",
	}}, false)
	requiredInstructions := []string{
		"call `load_skill` for every skill whose description matches the request",
		"load `mod-diagnosis` before modifying a mod or diagnosing a mod problem",
	}
	for _, required := range requiredInstructions {
		if !strings.Contains(prompt, required) {
			t.Fatalf("system prompt is missing %q", required)
		}
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

	messages := New(Options{}).messagesFromEvents(events, true)
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

	messages := New(Options{}).messagesFromEvents(events, true)
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

	messages := New(Options{}).messagesFromEvents(events, true)
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
	messages := New(Options{}).messagesFromEvents(events, true)
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
	if err != nil || len(modSnapshot.Roots) != 1 || modSnapshot.Roots[0].Path != selectedMod {
		t.Fatalf("mod roots = %#v, %v", modSnapshot.Roots, err)
	}
	if _, err := service.CreateSession(ctx, AgentScope{Type: "mod", ModPath: t.TempDir()}); err == nil {
		t.Fatal("outside mod scope unexpectedly succeeded")
	}
}
