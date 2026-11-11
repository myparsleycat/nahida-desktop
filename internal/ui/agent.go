package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/agent"
	"nahida.live/desktop/internal/hunting"
	"nahida.live/desktop/internal/infra"
)

const (
	agentMaxImages     = 4
	agentMaxImageBytes = 5 << 20
	// agentRestoredRun stands for a run that was already going when the conversation was opened.
	agentRestoredRun = "restored"
	agentChatKey     = "agent.chat"
)

var agentImageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif",
}

// agentScreen is the state of the agent screen. shown is the conversation on screen; generation invalidates
// snapshot loads that a later switch, revert or reload superseded.
type agentScreen struct {
	active   bool
	loading  bool
	sessions []agent.AgentSessionSummary
	shown    string
	// requested and requestScope name what enterAgent opens next: a conversation, or the latest of a scope.
	requested    string
	requestScope *agent.AgentScope
	generation   int
	snapshot     *agent.AgentSessionSnapshot
	live         []agentEntry
	runID        string
	latest       map[string]int64
	// ended holds the runs whose terminal event arrived, so a late Send result does not revive one.
	ended map[string]bool
	// usage is the context usage the running turn reported for usageSession; a reloaded snapshot replaces it.
	usage        *agent.AgentContextUsage
	usageSession string

	draft    string
	images   []agentDraftImage
	imageSeq int
	// sentImages keeps the bytes of sent attachments until the stored conversation replaces the pending message.
	sentImages  map[string][]byte
	deciding    string
	reverting   bool
	huntingBusy string
	rename      *agentRename
	share       *agentShare
	// expanded holds the disclosure state of reasoning and tool entries the user toggled, by entry key.
	expanded  map[string]bool
	copied    string
	meterOpen bool
}

type agentDraftImage struct {
	id       string
	name     string
	mimeType string
	data     []byte
}

type agentRename struct {
	target agent.AgentSessionSummary
	value  string
	busy   bool
}

type agentShare struct {
	target agent.AgentSessionSummary
	busy   bool
}

func (s *Shell) registerAgentEvents(on func(string, func([]any))) {
	on("agent:update", func(data []any) {
		if event, ok := first(data).(agent.AgentStreamEvent); ok {
			if event.Type == "tool-end" && elevatedHelperRequired(agentPayloadString(event.Payload, "error")) {
				s.promptElevatedHelper()
			}
			s.agentEvent(event)
		}
	})
}

func (s *Shell) agentEvent(update agent.AgentStreamEvent) {
	a := &s.agent
	// A generated title can land after its turn finished, so it skips the per-run sequence check.
	if update.Type == "session-title" {
		title := agentPayloadString(update.Payload, "title")
		if title == "" {
			return
		}
		for i := range a.sessions {
			if a.sessions[i].ID == update.SessionID {
				a.sessions[i].Title = title
			}
		}
		if a.snapshot != nil && a.snapshot.Summary.ID == update.SessionID {
			a.snapshot.Summary.Title = title
		}
		return
	}
	if update.SessionID != a.shown {
		return
	}
	if a.latest == nil {
		a.latest = map[string]int64{}
	}
	if !acceptAgentEvent(a.latest, update) {
		return
	}

	switch {
	case update.Type == "assistant-delta" || update.Type == "reasoning-delta" || update.Type == "tool-start" ||
		update.Type == "tool-end":
		a.live = updateAgentLiveEntries(a.live, update)
	case update.Type == "usage":
		values, _ := update.Payload.(map[string]any)
		if usage, ok := values["contextUsage"].(agent.AgentContextUsage); ok {
			a.usage, a.usageSession = &usage, update.SessionID
		}
	case agentApprovalEvent(update.Type):
		if update.Type == "approval-requested" {
			a.runID = ""
		}
		s.openAgentSnapshot(update.SessionID)
	case update.Type == "status" || update.Type == "error":
		status := agentPayloadString(update.Payload, "status")
		if update.Type == "status" && status == "running" {
			a.runID = update.RunID
		}
		if update.Type == "error" || status == "completed" || status == "cancelled" {
			if a.ended == nil {
				a.ended = map[string]bool{}
			}
			a.ended[update.RunID] = true
			a.runID, a.live = "", nil
			s.openAgentSnapshot(update.SessionID)
			s.refreshAgentSessions()
		}
	}
}

// agentCall runs call off the UI thread after startup maintenance, reports a failure, and hands the result to
// done on the UI thread.
func agentCall[T any](s *Shell, stage string, fields map[string]any, call func(context.Context) (T, error),
	done func(T, error),
) {
	go func() {
		ctx := context.Background()
		var value T
		err := s.waitReady(ctx)
		if err == nil {
			value, err = call(ctx)
		}
		if err != nil && !infra.IsCancellationError(err) {
			err = infra.ReportError(s.deps.Log, err, "Agent", infra.Diagnostic{
				Operation: "agent." + stage, Fields: fields,
			})
		}
		fairy.Go(func() { done(value, err) })
	}()
}

func (s *Shell) waitReady(ctx context.Context) error {
	if s.deps.Ready == nil {
		return nil
	}
	return s.deps.Ready(ctx)
}

func (s *Shell) agentFailed(err error) {
	if err != nil && !infra.IsCancellationError(err) {
		s.toasts.Show(kit.ToastError, err.Error(), "")
	}
}

// enterAgent loads the screen when it comes into view: the conversation list, then the requested conversation,
// the latest one of the requested scope, the one shown before, or the latest global one.
func (s *Shell) enterAgent() {
	a := &s.agent
	a.active, a.loading = true, true
	requested, scope := a.requested, a.requestScope
	if requested == "" && scope == nil {
		requested = a.shown
	}
	if requested == "" && scope == nil {
		scope = &agent.AgentScope{Type: "global"}
	}
	a.requested, a.requestScope = "", nil
	a.generation++
	generation := a.generation
	fields := map[string]any{"sessionId": requested}
	if scope != nil {
		fields["scope"], fields["modPath"] = scope.Type, scope.ModPath
	}
	type opened struct {
		sessions []agent.AgentSessionSummary
		id       string
	}
	agentCall(s, "enter", fields, func(ctx context.Context) (opened, error) {
		id := requested
		if scope != nil {
			session, err := s.deps.Agent.OpenSession(ctx, *scope)
			if err != nil {
				return opened{}, err
			}
			id = session.ID
		}
		sessions, err := s.deps.Agent.ListSessions(ctx)
		return opened{sessions: sessions, id: id}, err
	}, func(result opened, err error) {
		if err != nil {
			a.loading = false
			s.agentFailed(err)
			return
		}
		a.sessions = result.sessions
		// A conversation the user opened meanwhile, or a later entry, wins over this one.
		if generation != a.generation {
			a.loading = false
			return
		}
		s.openAgentSnapshotThen(result.id, func() { a.loading = false })
	})
}

// openAgentScope shows the latest conversation of scope, which the backend creates when there is none.
func (s *Shell) openAgentScope(scope agent.AgentScope) {
	a := &s.agent
	a.requested, a.requestScope, a.active = "", &scope, false
	s.navigate(RouteAgent)
}

// openAgentSession shows conversation id, as the /agent?session= path does.
func (s *Shell) openAgentSession(id string) {
	a := &s.agent
	a.requested, a.requestScope, a.active = id, nil, false
}

func (s *Shell) refreshAgentSessions() {
	a := &s.agent
	agentCall(s, "list-sessions", nil, func(ctx context.Context) ([]agent.AgentSessionSummary, error) {
		return s.deps.Agent.ListSessions(ctx)
	}, func(sessions []agent.AgentSessionSummary, err error) {
		if err == nil {
			a.sessions = sessions
		}
	})
}

func (s *Shell) openAgentSnapshot(id string) { s.openAgentSnapshotThen(id, nil) }

// openAgentSnapshotThen shows conversation id, ignoring the result when another load or switch started meanwhile.
func (s *Shell) openAgentSnapshotThen(id string, then func()) {
	a := &s.agent
	a.generation++
	generation := a.generation
	if a.shown != id {
		a.expanded = nil
	}
	a.shown, a.reverting, a.usage = id, false, nil
	agentCall(s, "get-session", map[string]any{"sessionId": id},
		func(ctx context.Context) (agent.AgentSessionSnapshot, error) {
			return s.deps.Agent.GetSession(ctx, id)
		}, func(snapshot agent.AgentSessionSnapshot, err error) {
			if then != nil {
				then()
			}
			if generation != a.generation || a.shown != id {
				return
			}
			if err != nil {
				s.agentFailed(err)
				return
			}
			a.snapshot, a.live, a.sentImages = &snapshot, nil, nil
			a.runID = ""
			if snapshot.Summary.Running {
				a.runID = agentRestoredRun
			}
			s.scrollAgentToBottom()
		})
}

func (s *Shell) scrollAgentToBottom() {
	if s.win != nil {
		s.win.ScrollToBottom(agentChatKey)
	}
}

func (s *Shell) agentEntries() []agentEntry {
	a := &s.agent
	var entries []agentEntry
	if a.snapshot != nil {
		entries = make([]agentEntry, 0, len(a.snapshot.Entries)+len(a.live))
		for _, entry := range a.snapshot.Entries {
			entries = append(entries, agentEntry{AgentChatEntry: entry})
		}
	}
	return append(entries, a.live...)
}

func (s *Shell) agentPending() bool {
	return s.agent.snapshot != nil && agentPendingApproval(s.agent.snapshot.Approvals)
}

func (s *Shell) agentCanSend() bool {
	a := &s.agent
	return a.snapshot != nil && (strings.TrimSpace(a.draft) != "" || len(a.images) > 0) && a.runID == "" &&
		!s.agentPending() && !a.reverting && a.snapshot.UnavailableReason == ""
}

func (s *Shell) sendAgent() {
	a := &s.agent
	if !s.agentCanSend() {
		return
	}
	id, text, attachments := a.snapshot.Summary.ID, strings.TrimSpace(a.draft), a.images
	a.draft, a.images, a.live = "", nil, nil

	// Sending commits a staged revert, which deletes the reverted events, so they leave the screen now.
	entries := make([]agent.AgentChatEntry, 0, len(a.snapshot.Entries)+1)
	for _, entry := range a.snapshot.Entries {
		if !entry.Reverted {
			entries = append(entries, entry)
		}
	}
	pending := agent.AgentChatEntry{
		Sequence: math.MaxInt64, TurnID: "pending", Type: "turn/start", Role: "user", Text: text,
		CreatedAt: agentNow(),
	}
	inputs := make([]agent.AgentImageInput, 0, len(attachments))
	if a.sentImages == nil {
		a.sentImages = map[string][]byte{}
	}
	for _, image := range attachments {
		a.sentImages[image.id] = image.data
		pending.Images = append(pending.Images, agent.AgentImage{
			Name: image.name, MIMEType: image.mimeType, Bytes: len(image.data), Src: agentDraftSrc(image.id),
		})
		inputs = append(inputs, agent.AgentImageInput{
			Name: image.name, MIMEType: image.mimeType, Data: base64.StdEncoding.EncodeToString(image.data),
		})
	}
	snapshot := *a.snapshot
	entries = append(entries, pending)
	snapshot.Entries, snapshot.Revert = entries, nil
	a.snapshot = &snapshot
	s.scrollAgentToBottom()

	agentCall(s, "send", map[string]any{"sessionId": id, "images": len(inputs)},
		func(ctx context.Context) (agent.SendResult, error) {
			return s.deps.Agent.Send(ctx, id, text, inputs)
		}, func(run agent.SendResult, err error) {
			if a.shown != id {
				s.agentFailed(err)
				return
			}
			if err != nil {
				s.agentFailed(err)
				if strings.TrimSpace(a.draft) == "" && len(a.images) == 0 {
					a.draft, a.images = text, attachments
				}
				s.openAgentSnapshot(id)
				return
			}
			// The run's events travel separately and may have ended it before this result arrived.
			if !a.ended[run.RunID] {
				a.runID = run.RunID
			}
		})
}

func (s *Shell) cancelAgentRun() {
	a := &s.agent
	if a.snapshot == nil || a.runID == "" {
		return
	}
	id, runID := a.snapshot.Summary.ID, a.runID
	agentCall(s, "cancel", map[string]any{"sessionId": id, "runId": runID},
		func(context.Context) (struct{}, error) { return struct{}{}, s.deps.Agent.Cancel(id, runID) },
		func(_ struct{}, err error) { s.agentFailed(err) })
}

// createAgentSession starts a conversation in the scope on screen, reusing an empty one so repeated clicks do not
// leave a trail of empty conversations.
func (s *Shell) createAgentSession() {
	a := &s.agent
	if a.snapshot != nil && len(a.snapshot.Entries) == 0 {
		return
	}
	scope := agent.AgentScope{Type: "global"}
	if a.snapshot != nil {
		scope = a.snapshot.Summary.Scope
	}
	var candidates []string
	for _, session := range a.sessions {
		if session.Scope.Type == scope.Type && session.Scope.ModPath == scope.ModPath {
			candidates = append(candidates, session.ID)
		}
	}
	agentCall(s, "create-session", map[string]any{"scope": scope.Type, "modPath": scope.ModPath},
		func(ctx context.Context) (string, error) {
			for _, id := range candidates {
				snapshot, err := s.deps.Agent.GetSession(ctx, id)
				if err != nil {
					return "", err
				}
				if len(snapshot.Entries) == 0 {
					return id, nil
				}
			}
			session, err := s.deps.Agent.CreateSession(ctx, scope)
			return session.ID, err
		}, func(id string, err error) {
			if err != nil {
				s.agentFailed(err)
				return
			}
			s.openAgentSnapshot(id)
			s.refreshAgentSessions()
		})
}

func (s *Shell) submitAgentRename() {
	r := s.agent.rename
	if r == nil || r.busy || strings.TrimSpace(r.value) == "" {
		return
	}
	r.busy = true
	id, title := r.target.ID, strings.TrimSpace(r.value)
	agentCall(s, "rename-session", map[string]any{"sessionId": id},
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, s.deps.Agent.RenameSession(ctx, id, title)
		}, func(_ struct{}, err error) {
			r.busy = false
			if err != nil {
				s.agentFailed(err)
				return
			}
			if s.agent.rename == r {
				s.agent.rename = nil
			}
			s.refreshAgentSessions()
			if s.agent.snapshot != nil && s.agent.snapshot.Summary.ID == id {
				s.openAgentSnapshot(id)
			}
		})
}

func (s *Shell) deleteAgentSession(target agent.AgentSessionSummary) {
	s.Confirm(Confirmation{
		Title:        T("page.agent.delete_title"),
		Description:  T("page.agent.delete_description", "title", target.Title),
		ConfirmLabel: T("page.agent.delete_confirm"),
		Danger:       true,
		OnResult: func(confirmed bool) {
			if !confirmed {
				return
			}
			a := &s.agent
			var remaining []agent.AgentSessionSummary
			for _, session := range a.sessions {
				if session.ID != target.ID {
					remaining = append(remaining, session)
				}
			}
			agentCall(s, "delete-session", map[string]any{"sessionId": target.ID},
				func(ctx context.Context) (string, error) {
					if err := s.deps.Agent.DeleteSession(ctx, target.ID); err != nil {
						return "", err
					}
					if len(remaining) > 0 {
						return remaining[0].ID, nil
					}
					session, err := s.deps.Agent.OpenSession(ctx, agent.AgentScope{Type: "global"})
					return session.ID, err
				}, func(next string, err error) {
					if err != nil {
						s.agentFailed(err)
						return
					}
					a.sessions = slices.DeleteFunc(a.sessions, func(session agent.AgentSessionSummary) bool {
						return session.ID == target.ID
					})
					if a.shown == target.ID {
						s.openAgentSnapshot(next)
					}
					s.refreshAgentSessions()
				})
		},
	})
}

func (s *Shell) agentShareBlocked() bool {
	a := &s.agent
	return a.snapshot == nil || a.runID != "" || a.reverting || a.snapshot.Revert != nil
}

func (s *Shell) confirmAgentShare() {
	sh := s.agent.share
	if sh == nil || sh.busy || s.agentShareBlocked() {
		return
	}
	sh.busy = true
	id := sh.target.ID
	agentCall(s, "share-session", map[string]any{"sessionId": id},
		func(ctx context.Context) (agent.ShareSessionResult, error) {
			return s.deps.Agent.SubmitSessionForTraining(ctx, id)
		}, func(result agent.ShareSessionResult, err error) {
			sh.busy = false
			if err != nil {
				s.agentFailed(err)
				return
			}
			if s.agent.share == sh {
				s.agent.share = nil
			}
			s.toasts.Show(kit.ToastSuccess,
				T("page.agent.share_success", "messages", result.Messages, "images", result.Images), "")
		})
}

func (s *Shell) decideAgentApproval(approval agent.AgentApproval, approved, forSession bool) {
	a := &s.agent
	if approval.Status != "pending" || a.deciding != "" {
		return
	}
	a.deciding = approval.ID
	agentCall(s, "decide-approval",
		map[string]any{"approvalId": approval.ID, "actionId": approval.ActionID, "approved": approved},
		func(ctx context.Context) (struct{}, error) {
			switch {
			case approved && forSession:
				return struct{}{}, s.deps.Agent.ApproveActionForSession(ctx, approval.ID)
			case approved:
				return struct{}{}, s.deps.Agent.ApproveAction(ctx, approval.ID)
			}
			return struct{}{}, s.deps.Agent.RejectAction(ctx, approval.ID)
		}, func(_ struct{}, err error) {
			a.deciding = ""
			s.agentFailed(err)
			s.openAgentSnapshot(approval.SessionID)
		})
}

// huntingAction runs one hunting control; busy names the control that shows a spinner meanwhile.
func (s *Shell) huntingAction(busy string, call func(context.Context, string) (hunting.Snapshot, error)) {
	a := &s.agent
	if a.snapshot == nil || a.huntingBusy != "" {
		return
	}
	id := a.snapshot.Summary.ID
	a.huntingBusy = busy
	agentCall(s, "hunting", map[string]any{"sessionId": id, "control": busy},
		func(ctx context.Context) (hunting.Snapshot, error) { return call(ctx, id) },
		func(snapshot hunting.Snapshot, err error) {
			a.huntingBusy = ""
			if err != nil {
				s.toasts.Show(kit.ToastError, T("page.agent.hunting_action_failed", "error", err.Error()), "")
				if a.shown == id {
					s.openAgentSnapshot(id)
				}
				return
			}
			if a.snapshot != nil && a.snapshot.Summary.ID == id {
				a.snapshot.Hunting = &snapshot
			}
		})
}

func (s *Shell) huntingNext(category hunting.Category) {
	s.huntingAction(string(category), func(ctx context.Context, id string) (hunting.Snapshot, error) {
		return s.deps.Agent.HuntingNext(ctx, id, category)
	})
}

func (s *Shell) huntingFound() { s.huntingAction("found", s.deps.Agent.HuntingFound) }

func (s *Shell) huntingCancel() { s.huntingAction("cancel", s.deps.Agent.HuntingCancel) }

// revertAgent stages a revert to entry and puts its text back into an empty composer.
func (s *Shell) revertAgent(entry agentEntry) {
	a := &s.agent
	if a.snapshot == nil || a.runID != "" || s.agentPending() || a.reverting {
		return
	}
	s.agentRevertCall("revert-session", func(ctx context.Context, id string) (agent.AgentSessionSnapshot, error) {
		return s.deps.Agent.RevertSession(ctx, id, entry.Sequence)
	}, func() {
		a.live = nil
		if entry.Text != "" && strings.TrimSpace(a.draft) == "" {
			a.draft = entry.Text
		}
	})
}

func (s *Shell) unrevertAgent() {
	a := &s.agent
	if a.snapshot == nil || a.reverting {
		return
	}
	s.agentRevertCall("unrevert-session", s.deps.Agent.UnrevertSession, nil)
}

func (s *Shell) agentRevertCall(stage string, call func(context.Context, string) (agent.AgentSessionSnapshot, error),
	applied func(),
) {
	a := &s.agent
	id := a.snapshot.Summary.ID
	a.generation++
	generation := a.generation
	a.shown, a.reverting = id, true
	agentCall(s, stage, map[string]any{"sessionId": id},
		func(ctx context.Context) (agent.AgentSessionSnapshot, error) { return call(ctx, id) },
		func(snapshot agent.AgentSessionSnapshot, err error) {
			if generation != a.generation || a.shown != id {
				return
			}
			a.reverting = false
			if err != nil {
				s.agentFailed(err)
				return
			}
			a.snapshot = &snapshot
			if applied != nil {
				applied()
			}
		})
}

func (s *Shell) pickAgentImages() {
	go func() {
		result, err := s.deps.Dialog.ShowOpenDialog(agentImagePickerOptions())
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "Agent", infra.Diagnostic{Operation: "agent.attach-image"})
		}
		fairy.Go(func() {
			s.agentFailed(err)
			if err == nil && !result.Canceled {
				s.addAgentImages(result.FilePaths)
			}
		})
	}()
}

// addAgentImages attaches the image files in paths, up to the limits the agent accepts.
func (s *Shell) addAgentImages(paths []string) {
	a := &s.agent
	room := agentMaxImages - len(a.images)
	go func() {
		var accepted []agentDraftImage
		var problems []string
		overflow := false
		for _, path := range paths {
			if len(accepted) >= room {
				overflow = true
				break
			}
			name := filepath.Base(path)
			mimeType, ok := agentImageTypes[strings.ToLower(filepath.Ext(path))]
			if !ok {
				problems = append(problems, T("page.agent.image_unsupported_type", "name", name))
				continue
			}
			info, err := os.Stat(path)
			if err == nil && info.Size() > agentMaxImageBytes {
				problems = append(problems,
					T("page.agent.image_too_large", "name", name, "limit", agentMaxImageBytes>>20))
				continue
			}
			var data []byte
			if err == nil {
				data, err = os.ReadFile(path)
			}
			if err != nil {
				problems = append(problems, infra.ReportError(s.deps.Log, err, "Agent", infra.Diagnostic{
					Operation: "agent.attach-image", Fields: map[string]any{"path": path},
				}).Error())
				continue
			}
			accepted = append(accepted, agentDraftImage{name: name, mimeType: mimeType, data: data})
		}
		fairy.Go(func() {
			for _, problem := range problems {
				s.toasts.Show(kit.ToastError, problem, "")
			}
			if overflow {
				s.toasts.Show(kit.ToastError, T("page.agent.image_limit_reached", "count", agentMaxImages), "")
			}
			for _, image := range accepted {
				if len(a.images) >= agentMaxImages {
					break
				}
				a.imageSeq++
				image.id = itoa(a.imageSeq)
				a.images = append(a.images, image)
			}
		})
	}()
}

// pasteAgentImage attaches the clipboard image as a PNG when the clipboard holds an image and no text, so a
// text paste still inserts the text. It reports whether it took the paste.
func (s *Shell) pasteAgentImage() bool {
	if s.win == nil || strings.TrimSpace(s.win.ClipboardText()) != "" {
		return false
	}
	img, ok := s.win.ClipboardImage()
	if !ok {
		return false
	}
	a := &s.agent
	if len(a.images) >= agentMaxImages {
		s.toasts.Show(kit.ToastError, T("page.agent.image_limit_reached", "count", agentMaxImages), "")
		return true
	}
	const name = "clipboard.png"
	go func() {
		var buf bytes.Buffer
		err := png.Encode(&buf, img)
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "Agent", infra.Diagnostic{
				Operation: "agent.paste-image", Stage: "encode",
			})
		}
		fairy.Go(func() {
			switch {
			case err != nil:
				s.agentFailed(err)
			case buf.Len() > agentMaxImageBytes:
				s.toasts.Show(kit.ToastError,
					T("page.agent.image_too_large", "name", name, "limit", agentMaxImageBytes>>20), "")
			case len(a.images) >= agentMaxImages:
				s.toasts.Show(kit.ToastError, T("page.agent.image_limit_reached", "count", agentMaxImages), "")
			default:
				a.imageSeq++
				a.images = append(a.images, agentDraftImage{
					id: itoa(a.imageSeq), name: name, mimeType: "image/png", data: buf.Bytes(),
				})
			}
		})
	}()
	return true
}

func (s *Shell) copyAgentText(key, text string) {
	if text == "" || s.win == nil {
		return
	}
	if !s.win.SetClipboardText(text) {
		s.toasts.Show(kit.ToastError, T(lo.Ternary(key == "hunting", "page.agent.hunting_copy_failed",
			"page.agent.copy_failed")), "")
		return
	}
	s.agent.copied = key
	time.AfterFunc(1500*time.Millisecond, func() {
		fairy.Go(func() {
			if s.agent.copied == key {
				s.agent.copied = ""
			}
		})
	})
}

// openAgentLink opens a web or mail link of an assistant message in the browser; other schemes are ignored.
func (s *Shell) openAgentLink(link string) {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
		return
	}
	go func() {
		if err := s.deps.Platform.OpenExternal(link); err != nil {
			_ = infra.ReportError(s.deps.Log, err, "Agent", infra.Diagnostic{
				Operation: "agent.open-link", Fields: map[string]any{"url": infra.SanitizeLogURL(link)},
			})
		}
	}()
}
