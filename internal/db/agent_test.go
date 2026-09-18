package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAgentEventSequenceCascadeAndInterruptedRepair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	session := AgentSessionRow{ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1"}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}
	first, err := client.AgentEvents.Append(
		ctx,
		AgentEventRow{SessionID: session.ID, TurnID: "turn", EventType: "turn/start", Payload: `{}`, CreatedAt: "1"},
	)
	if err != nil || first != 1 {
		t.Fatalf("first append = %d, %v", first, err)
	}
	second, err := client.AgentEvents.Append(
		ctx,
		AgentEventRow{
			SessionID: session.ID,
			TurnID:    "turn",
			EventType: "message/assistant",
			Payload:   `{}`,
			CreatedAt: "2",
		},
	)
	if err != nil || second != 2 {
		t.Fatalf("second append = %d, %v", second, err)
	}
	if err := client.AgentEvents.SealInterrupted(ctx, "3"); err != nil {
		t.Fatal(err)
	}
	events, err := client.AgentEvents.List(ctx, session.ID)
	if err != nil || len(events) != 3 || events[2].EventType != "turn/interrupted" {
		t.Fatalf("events = %#v, %v", events, err)
	}
	if err := client.AgentSessions.Delete(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	events, err = client.AgentEvents.List(ctx, session.ID)
	if err != nil || len(events) != 0 {
		t.Fatalf("events after cascade = %#v, %v", events, err)
	}
}

func TestAgentApprovalLifecycleAndCascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := New(filepath.Join(t.TempDir(), "approval.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	session := AgentSessionRow{ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1"}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}
	approval := AgentApprovalRow{ID: "approval", SessionID: session.ID, TurnID: "turn", ToolCallID: "tool",
		ActionID: "mod.apply_preset", Arguments: `{"presetId":"preset"}`, Summary: "Apply preset",
		Impact: "Changes mods", Status: "pending", CreatedAt: "1"}
	if err := client.AgentApprovals.Insert(ctx, approval); err != nil {
		t.Fatal(err)
	}
	transitioned, err := client.AgentApprovals.Transition(ctx, approval.ID, "pending", "executing", "2")
	if err != nil || transitioned.Status != "executing" {
		t.Fatalf("transition = %#v, %v", transitioned, err)
	}
	if _, err := client.AgentApprovals.Transition(ctx, approval.ID, "pending", "executing", "2"); err == nil {
		t.Fatal("duplicate approval transition unexpectedly succeeded")
	}
	if err := client.AgentApprovals.Complete(
		ctx,
		approval.ID,
		"executing",
		"completed",
		`{"ok":true}`,
		"",
		"3",
	); err != nil {
		t.Fatal(err)
	}
	rows, err := client.AgentApprovals.ListSession(ctx, session.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != "completed" {
		t.Fatalf("approvals = %#v, %v", rows, err)
	}
	if err := client.AgentSessions.Delete(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = client.AgentApprovals.ListSession(ctx, session.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("approvals after cascade = %#v, %v", rows, err)
	}
}

func TestAgentApprovalCompletionPersistsEventsAtomically(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	client, err := New(filepath.Join(t.TempDir(), "approval-events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	session := AgentSessionRow{ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1"}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}
	approval := AgentApprovalRow{
		ID: "approval", SessionID: session.ID, TurnID: "turn", ToolCallID: "tool", ActionID: "action",
		Arguments: `{}`, Summary: "Test", Status: "executing", CreatedAt: "1",
	}
	if err := client.AgentApprovals.Insert(ctx, approval); err != nil {
		t.Fatal(err)
	}
	events := []AgentEventRow{
		{SessionID: session.ID, TurnID: "turn", EventType: "tool/end", Payload: `{}`, CreatedAt: "2"},
		{SessionID: session.ID, TurnID: "turn", EventType: "approval/updated", Payload: `{}`, CreatedAt: "2"},
	}
	if err := client.AgentApprovals.CompleteWithEvents(
		ctx, approval.ID, "executing", "completed", `{"ok":true}`, "", "2", events,
	); err != nil {
		t.Fatal(err)
	}
	completed, err := client.AgentApprovals.Get(ctx, approval.ID)
	if err != nil || completed == nil || completed.Status != "completed" {
		t.Fatalf("completed approval = %#v, %v", completed, err)
	}
	storedEvents, err := client.AgentEvents.List(ctx, session.ID)
	if err != nil || len(storedEvents) != 2 || storedEvents[0].EventType != "tool/end" ||
		storedEvents[1].EventType != "approval/updated" {
		t.Fatalf("completion events = %#v, %v", storedEvents, err)
	}
}

func TestAgentApprovalCompletionRollsBackWhenEventFails(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	client, err := New(filepath.Join(t.TempDir(), "approval-rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	session := AgentSessionRow{ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1"}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}
	approval := AgentApprovalRow{
		ID: "approval", SessionID: session.ID, TurnID: "turn", ToolCallID: "tool", ActionID: "action",
		Arguments: `{}`, Summary: "Test", Status: "executing", CreatedAt: "1",
	}
	if err := client.AgentApprovals.Insert(ctx, approval); err != nil {
		t.Fatal(err)
	}
	err = client.AgentApprovals.CompleteWithEvents(
		ctx,
		approval.ID,
		"executing",
		"completed",
		`{"ok":true}`,
		"",
		"2",
		[]AgentEventRow{{SessionID: "missing", TurnID: "turn", EventType: "tool/end", Payload: `{}`, CreatedAt: "2"}},
	)
	if err == nil {
		t.Fatal("completion unexpectedly succeeded")
	}
	stored, getErr := client.AgentApprovals.Get(ctx, approval.ID)
	if getErr != nil || stored == nil || stored.Status != "executing" {
		t.Fatalf("approval after rollback = %#v, %v", stored, getErr)
	}
}

func TestAgentApprovalSealExecuting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := New(filepath.Join(t.TempDir(), "approval.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	session := AgentSessionRow{ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1"}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "executing"} {
		row := AgentApprovalRow{ID: status, SessionID: session.ID, TurnID: status, ToolCallID: status,
			ActionID: "test", Arguments: `{}`, Summary: "Test", Impact: "Test", Status: status, CreatedAt: "1"}
		if err := client.AgentApprovals.Insert(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.AgentApprovals.SealExecuting(ctx, "2"); err != nil {
		t.Fatal(err)
	}
	pending, _ := client.AgentApprovals.Get(ctx, "pending")
	executing, _ := client.AgentApprovals.Get(ctx, "executing")
	if pending.Status != "pending" || executing.Status != "failed" || executing.CompletedAt == nil {
		t.Fatalf("pending = %#v, executing = %#v", pending, executing)
	}
}

func TestAgentSessionRenameIfUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := New(filepath.Join(t.TempDir(), "rename.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	session := AgentSessionRow{
		ID:        "session",
		ScopeType: "global",
		Title:     "First message",
		CreatedAt: "1",
		UpdatedAt: "1",
	}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}

	renamed, err := client.AgentSessions.RenameIfUnchanged(ctx, session.ID, "Another title", "Generated title", "2")
	if err != nil || renamed {
		t.Fatalf("rename with a stale title = %t, %v", renamed, err)
	}
	stored, err := client.AgentSessions.Get(ctx, session.ID)
	if err != nil || stored.Title != "First message" {
		t.Fatalf("title after stale rename = %#v, %v", stored, err)
	}

	renamed, err = client.AgentSessions.RenameIfUnchanged(ctx, session.ID, "First message", "Generated title", "3")
	if err != nil || !renamed {
		t.Fatalf("rename with the current title = %t, %v", renamed, err)
	}
	stored, err = client.AgentSessions.Get(ctx, session.ID)
	if err != nil || stored.Title != "Generated title" || stored.UpdatedAt != "3" {
		t.Fatalf("title after rename = %#v, %v", stored, err)
	}
}
