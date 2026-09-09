package tools

import (
	"context"
	"fmt"
	"math"

	"nahida.live/desktop/internal/infra"
)

type ModelViewerBlockingVariable struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Value string `json:"value"`
}

type ModelViewerResolutionChange struct {
	VarID     string `json:"varId"`
	VarLabel  string `json:"varLabel"`
	FromValue string `json:"fromValue"`
	ToValue   string `json:"toValue"`
}

type ModelViewerResolutionSuggestion struct {
	Display string                        `json:"display"`
	Changes []ModelViewerResolutionChange `json:"changes"`
}

type ModelViewerIneffectiveValue struct {
	VariableID   string                            `json:"variableId"`
	Value        string                            `json:"value"`
	BlockingVars []ModelViewerBlockingVariable     `json:"blockingVars"`
	Suggestions  []ModelViewerResolutionSuggestion `json:"suggestions"`
}

type modelViewerEvaluator struct {
	payload  ModelViewerTransport
	blocking map[string][]ModelViewerVariable
	ctx      context.Context
}

func newModelViewerEvaluator(ctx context.Context, payload ModelViewerTransport) *modelViewerEvaluator {
	e := &modelViewerEvaluator{payload: payload, ctx: ctx, blocking: make(map[string][]ModelViewerVariable)}
	// Co-occurrence depends on the manifest, not the selected values.
	all := make(map[string]any, len(payload.Variables))
	byID := make(map[string]ModelViewerVariable, len(payload.Variables))
	for _, variable := range payload.Variables {
		all[variable.ID] = 0
		byID[variable.ID] = variable
	}
	for _, variable := range payload.Variables {
		if variable.ControlType == "slider" || len(variable.Values) == 0 {
			continue
		}
		for _, blocking := range buildViewerBlockingVars(payload, variable.ID, all) {
			e.blocking[variable.ID] = append(e.blocking[variable.ID], byID[blocking.ID])
		}
	}
	return e
}

func (t *Tools) GetModelViewerIneffectiveValues(
	ctx context.Context,
	sessionID string,
	state map[string]any,
) (result []ModelViewerIneffectiveValue, err error) {
	t.modelViewerMu.Lock()
	session := t.modelViewerSessions[sessionID]
	t.modelViewerMu.Unlock()
	defer func() {
		if err != nil && ctx.Err() == nil {
			fields := map[string]any{"memorySessionId": sessionID}
			if session != nil {
				fields["modPath"] = session.modPath
			}
			err = infra.ReportError(
				t.log,
				err,
				"Tools.GetModelViewerIneffectiveValues",
				infra.Diagnostic{Operation: "evaluate-toggle-choices", Stage: "evaluate", Fields: fields},
			)
		}
	}()
	if session == nil || session.evaluator == nil {
		return nil, fmt.Errorf("model viewer session is unavailable")
	}
	for key, value := range state {
		switch v := value.(type) {
		case string:
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("invalid model viewer state value for %s", key)
			}
		case int:
		default:
			return nil, fmt.Errorf("invalid model viewer state value for %s", key)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(session.evaluator.ctx, cancel)
	defer stop()
	return session.evaluator.evaluate(ctx, state)
}

func (e *modelViewerEvaluator) evaluate(
	ctx context.Context,
	state map[string]any,
) ([]ModelViewerIneffectiveValue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved := viewerEvalApplyStateRules(mergeViewerEvalState(e.payload.DefaultState, state), e.payload.StateRules)
	baseline := evaluateViewerTransport(e.payload, resolved)
	result := make([]ModelViewerIneffectiveValue, 0)
	for _, variable := range e.payload.Variables {
		if variable.ControlType == "slider" || len(variable.Values) == 0 {
			continue
		}
		current := modelViewerEvalValue(resolved, variable)
		blocking := make([]ModelViewerBlockingVariable, 0, len(e.blocking[variable.ID]))
		for _, other := range e.blocking[variable.ID] {
			if value, ok := viewerEvalLookup(resolved, other.ID); ok {
				blocking = append(
					blocking,
					ModelViewerBlockingVariable{ID: other.ID, Label: other.Label, Value: modelViewerString(value)},
				)
			}
		}
		for _, entry := range variable.Values {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if modelViewerString(entry.Value) == current {
				continue
			}
			next := evaluateViewerTransport(e.payload, applyViewerVariableSelection(resolved, variable, entry.Value))
			if viewerEvalStatesDiffer(baseline, next) {
				continue
			}
			suggestions, err := e.suggestions(ctx, variable, modelViewerString(entry.Value), resolved, baseline)
			if err != nil {
				return nil, err
			}
			result = append(
				result,
				ModelViewerIneffectiveValue{
					VariableID:   variable.ID,
					Value:        modelViewerString(entry.Value),
					BlockingVars: blocking,
					Suggestions:  suggestions,
				},
			)
		}
	}
	return result, ctx.Err()
}

func (e *modelViewerEvaluator) suggestions(
	ctx context.Context,
	tested ModelViewerVariable,
	value string,
	state map[string]any,
	baseline viewerEvalState,
) ([]ModelViewerResolutionSuggestion, error) {
	result := make([]ModelViewerResolutionSuggestion, 0)
	for _, blocking := range e.blocking[tested.ID] {
		current, ok := viewerEvalLookup(state, blocking.ID)
		if !ok {
			continue
		}
		for _, entry := range blocking.Values {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if modelViewerString(current) == modelViewerString(entry.Value) {
				continue
			}
			alternative := applyViewerVariableSelection(state, blocking, entry.Value)
			testState := applyViewerVariableSelection(alternative, tested, value)
			testEval := evaluateViewerTransport(e.payload, testState)
			if !viewerEvalStatesDiffer(evaluateViewerTransport(e.payload, alternative), testEval) ||
				!viewerEvalStatesDiffer(baseline, testEval) {
				continue
			}
			changes := make([]ModelViewerResolutionChange, 0)
			for _, variable := range e.payload.Variables {
				from, to := modelViewerEvalValue(state, variable), modelViewerEvalValue(testState, variable)
				if from != to {
					changes = append(
						changes,
						ModelViewerResolutionChange{
							VarID:     variable.ID,
							VarLabel:  variable.Label,
							FromValue: from,
							ToValue:   to,
						},
					)
				}
			}
			result = append(
				result,
				ModelViewerResolutionSuggestion{
					Display: fmt.Sprintf(
						"%s: %s → %s",
						blocking.Label,
						modelViewerString(current),
						modelViewerString(entry.Value),
					),
					Changes: changes,
				},
			)
		}
	}
	return result, nil
}

func modelViewerEvalValue(state map[string]any, variable ModelViewerVariable) string {
	value, ok := viewerEvalLookup(state, variable.ID)
	if !ok {
		value = variable.DefaultValue
	}
	return modelViewerString(value)
}
