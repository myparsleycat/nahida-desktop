package actions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"nahida.live/desktop/internal/hunting"
)

func TestHuntingImageActionsFollowModelCapability(t *testing.T) {
	t.Parallel()
	service := hunting.New(hunting.Options{})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})
	registry := NewRegistry(Dependencies{Hunting: service})

	definitions := registry.DefinitionsFor("global", "hunting", "", false)
	ids := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		ids[definition.ID] = true
	}
	for _, hidden := range []string{"hunting.inspect", "hunting.begin"} {
		if ids[hidden] {
			t.Fatalf("%s is visible without image support", hidden)
		}
	}
	if !ids["hunting.status"] {
		t.Fatal("hunting.status is hidden without image support")
	}
	for _, removed := range []string{"hunting.scan", "hunting.continue", "hunting.accept", "hunting.cancel"} {
		if _, ok := ids[removed]; ok {
			t.Fatalf("removed autonomous action %s is still registered", removed)
		}
	}

	_, err := registry.PrepareWithContext("global", nil, Call{
		ActionID: "hunting.inspect", Arguments: json.RawMessage(`{"importerKey":"GIMI"}`),
	}, ExecutionContext{SupportsImages: false})
	if !errors.Is(err, hunting.ErrImagesRequired) {
		t.Fatalf("PrepareWithContext = %v, want %v", err, hunting.ErrImagesRequired)
	}
}

func TestHuntingBeginPreparesApprovalWithInitiallyActive(t *testing.T) {
	t.Parallel()
	service := hunting.New(hunting.Options{})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})
	registry := NewRegistry(Dependencies{Hunting: service})

	plan, err := registry.PrepareWithContext("global", nil, Call{
		ActionID: "hunting.begin",
		Arguments: json.RawMessage(
			`{"importerKey":"ZZMI","pid":35628,"initiallyActive":false}`,
		),
	}, ExecutionContext{SessionID: "agent-session", SupportsImages: true})
	if err != nil {
		t.Fatalf("PrepareWithContext = %v", err)
	}
	if plan.Risk != RiskConfirm {
		t.Fatalf("Risk = %q, want %q", plan.Risk, RiskConfirm)
	}
	if plan.Proposal == nil {
		t.Fatal("Proposal is nil")
	}
	if plan.Proposal.ActionID != "hunting.begin" {
		t.Fatalf("Proposal.ActionID = %q, want hunting.begin", plan.Proposal.ActionID)
	}
	if plan.Proposal.Target != "ZZMI game pid 35628" {
		t.Fatalf("Proposal.Target = %q, want ZZMI game pid 35628", plan.Proposal.Target)
	}
}
