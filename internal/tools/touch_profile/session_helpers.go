package touchprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
)

// lockTouchSession returns the session with its mutex held. A call that was waiting while the session was
// discarded sees it closed instead of working on a torn-down session.
func (t *Service) lockTouchSession(id string) (*touchSession, error) {
	t.touchMu.Lock()
	session := t.touchSessions[id]
	t.touchMu.Unlock()
	if session == nil {
		return nil, infra.ContractError(fmt.Sprintf("Touch profile session not found: %s", id))
	}
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return nil, infra.ContractError(fmt.Sprintf("Touch profile session not found: %s", id))
	}
	return session, nil
}

// relocateTouchSession keeps the session's mesh paths valid after apply or rollback renamed the source folder.
// The rename already happened, so a failure is reported instead of failing the operation.
func (t *Service) relocateTouchSession(session *touchSession, sourceRoot, operation string) {
	analysis, err := relocateTouchAnalysis(session.Analysis, sourceRoot)
	if err != nil {
		_ = infra.ReportError(t.log, err, "Tools", infra.Diagnostic{
			Operation: operation, Stage: "relocate-analysis",
			Fields: map[string]any{"sourceRoot": sourceRoot},
		})
		return
	}
	session.Analysis = analysis
}

func (t *Service) emitTouchProgress(sessionID, stage string, progress float64, message, componentID string) {
	event := TouchProgressEvent{Stage: stage, Progress: progress, Message: message}
	if sessionID != "" {
		event.SessionID = &sessionID
	}
	if componentID != "" {
		event.ComponentID = &componentID
	}
	t.emitEvent(touchProgressEventName, event)
}

func (t *Service) shutdownTouchProfiles() error {
	t.touchMu.Lock()
	sessions := t.touchSessions
	t.touchSessions = map[string]*touchSession{}
	t.touchMu.Unlock()
	var err error
	for _, session := range sessions {
		session.mu.Lock()
		session.closed = true
		if t.protocol != nil {
			t.protocol.CleanupMemorySession(session.ProtocolID)
		}
		err = errors.Join(err, os.RemoveAll(session.Dir))
		session.mu.Unlock()
	}
	return err
}

func clearTouchPreviewBuffers(protocol *infra.Protocol, session *touchSession) {
	if protocol != nil {
		for _, preview := range session.Preview {
			protocol.RemoveMemoryBuffer(session.ProtocolID, preview.bufferID)
		}
	}
	session.Preview = make(map[string]touchCachedPreview)
}

func touchMaskSettingsChanged(previous, next TouchZoneSettings) bool {
	return previous.MaskStrength != next.MaskStrength ||
		previous.MaskCurve != next.MaskCurve ||
		previous.MaskRadiusScale != next.MaskRadiusScale ||
		previous.MaskCoreAttenuation != next.MaskCoreAttenuation
}

func findTouchComponent(components []TouchComponentAnalysis, id string) *TouchComponentAnalysis {
	for i := range components {
		if components[i].ID == id {
			return &components[i]
		}
	}
	return nil
}

func findTouchDraft(drafts []TouchComponentDraft, id string) *TouchComponentDraft {
	for i := range drafts {
		if drafts[i].ComponentID == id {
			return &drafts[i]
		}
	}
	return nil
}

func appendUniqueString(values []string, value string) []string {
	for _, entry := range values {
		if entry == value {
			return values
		}
	}
	return append(values, value)
}

func touchDraftAutoApplyable(interactive []TouchComponentDraft) bool {
	if len(interactive) == 0 {
		return false
	}
	minimum, total := 1.0, 0.0
	for _, entry := range interactive {
		minimum = min(minimum, entry.Confidence)
		total += entry.Confidence
	}
	return minimum >= touchConfidenceAutoMin && total/float64(len(interactive)) >= touchConfidenceAutoAverage
}

func assertTouchDraftCanApply(draft TouchDraft, force bool) error {
	if !force && !draft.CanAutoApply {
		return infra.ContractError(
			"Touch draft confidence is too low for automatic apply. Review zones or pass force=true.",
		)
	}
	return nil
}

func writeTouchDraft(dir string, draft TouchDraft) error {
	raw, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	return writeTouchFileAtomic(filepath.Join(dir, "draft.json"), raw, 0600)
}

func (t *Service) touchUseFrameGuard(ctx context.Context, sourceRoot string) bool {
	if t.xxmi == nil {
		return false
	}
	importers, err := t.xxmi.GetEnabledImporters(ctx)
	if err != nil {
		return false
	}
	for _, importer := range importers {
		mods := filepath.Join(importer.ImporterFolder, "Mods")
		relative, err := filepath.Rel(mods, sourceRoot)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
			filepath.IsAbs(relative) {
			continue
		}
		version, ok := t.xxmi.DeployedLibsVersion(ctx, importer.Key)
		return ok && supportsTouchFrameNumberGuard(version)
	}
	return false
}
