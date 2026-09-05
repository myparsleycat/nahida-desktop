package mod

import (
	"fmt"
	"os"

	"nahida.live/desktop/internal/infra"
)

var (
	rollbackRemovePath = os.RemoveAll
	rollbackMovePath   = movePathOverwrite
	rollbackWriteFile  = os.WriteFile
)

type mergeRollback struct {
	kind    string
	path    string
	from    string
	to      string
	content []byte
	mode    os.FileMode
}

type mergeRollbackFailure struct {
	action mergeRollback
	err    error
}

type mergeRollbackFailureLog struct {
	Action string `json:"action"`
	Error  string `json:"error"`
}

type mergeFailureLog struct {
	Operation        string                    `json:"operation"`
	GroupPath        string                    `json:"groupPath"`
	Placement        string                    `json:"placement"`
	PackName         string                    `json:"packName"`
	Stage            string                    `json:"stage"`
	Created          []string                  `json:"created"`
	RollbackFailures []mergeRollbackFailureLog `json:"rollbackFailures"`
	Error            string                    `json:"error"`
}

func rollbackMerge(actions []mergeRollback) []mergeRollbackFailure {
	failures := make([]mergeRollbackFailure, 0)
	for i := len(actions) - 1; i >= 0; i-- {
		action := actions[i]
		var err error
		switch action.kind {
		case "remove":
			err = rollbackRemovePath(action.path)
		case "move":
			if _, statErr := os.Stat(action.from); statErr == nil {
				err = rollbackMovePath(action.from, action.to)
			} else if !os.IsNotExist(statErr) {
				err = statErr
			}
		case "restore":
			err = rollbackWriteFile(action.path, action.content, action.mode)
		default:
			err = fmt.Errorf("unknown merge rollback action %q", action.kind)
		}
		if err != nil {
			failures = append(failures, mergeRollbackFailure{action: action, err: err})
		}
	}
	return failures
}

func describeMergeRollback(action mergeRollback) string {
	switch action.kind {
	case "remove":
		return action.path
	case "restore":
		return "restore:" + action.path
	case "move":
		return action.from + "->" + action.to
	default:
		return action.kind + ":" + action.path
	}
}

func (m *Mod) logMergeFailure(
	request MergeModsRequest,
	actions []mergeRollback,
	failures []mergeRollbackFailure,
	err error,
) error {
	if err == nil {
		return nil
	}
	created := make([]string, len(actions))
	for i, action := range actions {
		created[i] = describeMergeRollback(action)
	}
	rollbackFailures := make([]mergeRollbackFailureLog, len(failures))
	for i, failure := range failures {
		rollbackFailures[i] = mergeRollbackFailureLog{
			Action: describeMergeRollback(failure.action),
			Error:  failure.err.Error(),
		}
	}
	var log *infra.Log
	if m != nil {
		log = m.log
	}
	return infra.ReportError(log, err, "Mod", infra.Diagnostic{
		Operation: "merge-mods", Stage: "execute",
		Fields: map[string]any{
			"groupPath": request.GroupPath, "placement": request.Placement,
			"packName": request.PackName, "created": created,
			"rollbackFailures": rollbackFailures,
		},
	})
}
