package togglepersist

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"nahida.live/desktop/internal/gamelink"
	"nahida.live/desktop/internal/infra"
)

const (
	// gamePreviewRestoreDelay is how long the games keep a preview after it ends, for the next one to replace it.
	gamePreviewRestoreDelay = 150 * time.Millisecond
)

// gamePreviewRetryDelays space the attempts to send a state again to a game that answered BUSY or is
// reconnecting, which the link manager does at once after a dropped pipe.
var gamePreviewRetryDelays = []time.Duration{
	200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 1600 * time.Millisecond,
	3200 * time.Millisecond,
}

// GamePreviewINI is the toggle state one INI previews, keyed by the declared names without `$`.
type GamePreviewINI struct {
	Path  string
	State map[string]any
}

// GamePreview shows the toggle values the model viewer previews in the running games that load the mod, and
// sets back the values the games had once the preview ends. One goroutine sends the requests in order and only
// the latest state, so hovering down a list does not queue requests in the game.
type GamePreview struct {
	service      *Service
	wake         chan struct{}
	restoreDelay time.Duration
	failures     infra.DiagnosticThrottle

	mu      sync.Mutex
	pending []GamePreviewINI
	closed  bool

	// The fields below belong to the run goroutine. contents caches the INIs read while a preview lasts.
	contents map[string]string
	games    map[uint32]*previewGame
}

// previewGame holds, by variable name, the values a game had before the preview and the values it has now.
// missing lists the names the game does not declare.
type previewGame struct {
	original map[string]float32
	shown    map[string]float32
	missing  map[string]bool
}

// NewGamePreview starts the preview of one model viewer. Close it when the viewer closes.
func (t *Service) NewGamePreview() *GamePreview {
	p := &GamePreview{
		service: t, wake: make(chan struct{}, 1), restoreDelay: gamePreviewRestoreDelay,
		games: map[uint32]*previewGame{},
	}
	go p.run()
	return p
}

// Show previews inis in the games; nil ends the preview and sets back the values it changed.
func (p *GamePreview) Show(inis []GamePreviewINI) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.pending = inis
	p.signal()
}

// Close ends the preview without waiting for the games to get their values back.
func (p *GamePreview) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed, p.pending = true, nil
	p.signal()
}

func (p *GamePreview) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *GamePreview) run() {
	for range p.wake {
		inis, closed := p.next()
		failed := p.show(inis)
		for _, delay := range gamePreviewRetryDelays {
			if len(failed) == 0 {
				break
			}
			if !p.wait(delay) {
				// A new state replaces the failed one; leave it for the loop to take.
				p.signal()
				failed = nil
				break
			}
			inis, closed = p.take()
			failed = p.show(inis)
		}

		// The values a game refused to take back stay in it; forgetting them keeps a later preview from setting
		// the game back to values from before it, such as ones applied to the game since.
		for _, pid := range failed {
			delete(p.games, pid)
			p.service.log.Warn(
				fmt.Sprintf("Game %d keeps the toggle values the model viewer previewed", pid),
				"TogglePersist.gamePreview",
			)
		}
		if closed {
			return
		}
	}
}

func (p *GamePreview) take() ([]GamePreviewINI, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pending, p.closed
}

// next takes the state to show. The end of a preview is held back for restoreDelay: moving the pointer to the
// next row of a list leaves the previous row first, and setting the values back in between would show them in
// the game for a frame.
func (p *GamePreview) next() ([]GamePreviewINI, bool) {
	inis, closed := p.take()
	if len(inis) > 0 || closed || len(p.games) == 0 {
		return inis, closed
	}
	deadline := time.Now().Add(p.restoreDelay)
	for !p.wait(time.Until(deadline)) {
		if inis, closed := p.take(); len(inis) > 0 || closed {
			return inis, closed
		}
	}
	return p.take()
}

// wait waits for d and reports whether it passed without a new state.
func (p *GamePreview) wait(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-p.wake:
		return false
	}
}

// show sets every game to inis and returns the games that did not take it but may still: one that answered
// BUSY or is reconnecting. Their records stay, since an earlier preview may still show in them.
func (p *GamePreview) show(inis []GamePreviewINI) []uint32 {
	desired := map[uint32]map[string]float32{}
	for _, ini := range inis {
		links, _ := p.service.gameLinks(ini.Path)
		if len(links) == 0 {
			continue
		}
		content, err := p.content(ini.Path)
		if err != nil {
			p.failures.Report(p.service.log, err, "TogglePersist.gamePreview", infra.Diagnostic{
				Operation: "game-preview", Stage: "read-ini", Fields: map[string]any{"path": ini.Path},
			})
			continue
		}
		for _, link := range links {
			for _, assign := range gameAssignments(content, link.namespace, ini.State, false) {
				if desired[link.pid] == nil {
					desired[link.pid] = map[string]float32{}
				}
				desired[link.pid][assign.Name] = assign.Value
			}
		}
	}

	for pid := range p.games {
		if desired[pid] == nil {
			desired[pid] = map[string]float32{}
		}
	}
	var failed []uint32
	for pid, values := range desired {
		err := p.showGame(pid, values)
		switch {
		case err == nil:
			continue
		case !p.running(pid):
			delete(p.games, pid)
			continue
		case !errors.Is(err, context.DeadlineExceeded):
			failed = append(failed, pid)
		}
		p.failures.Report(p.service.log, err, "TogglePersist.gamePreview", infra.Diagnostic{
			Operation: "game-preview", Fields: map[string]any{"pid": pid, "variables": len(values)},
		})
	}
	if len(p.games) == 0 {
		p.contents = nil
	}
	return failed
}

// running reports whether the game is still running, though its link may be reconnecting.
func (p *GamePreview) running(pid uint32) bool {
	return slices.ContainsFunc(p.service.link.States(), func(state gamelink.State) bool {
		return state.PID == pid
	})
}

func (p *GamePreview) content(path string) (string, error) {
	if content, ok := p.contents[path]; ok {
		return content, nil
	}
	content, _, err := readPersistINI(path)
	if err != nil {
		return "", err
	}
	if p.contents == nil {
		p.contents = map[string]string{}
	}
	p.contents[path] = string(content)
	return string(content), nil
}

// showGame sets the variables of one game to values, and the variables an earlier state previewed but values
// leaves out back to what the game had.
func (p *GamePreview) showGame(pid uint32, values map[string]float32) error {
	game := p.games[pid]
	if game == nil {
		if len(values) == 0 {
			return nil
		}
		game = &previewGame{original: map[string]float32{}, shown: map[string]float32{}, missing: map[string]bool{}}
		p.games[pid] = game
	}

	var unread []string
	for name := range values {
		if _, ok := game.original[name]; !ok && !game.missing[name] {
			unread = append(unread, name)
		}
	}
	if len(unread) > 0 {
		slices.Sort(unread)
		reply, err := p.request(pid, gamelink.GetVars{Names: unread})
		if err != nil {
			return fmt.Errorf("read game variables: %w", err)
		}
		read, ok := reply.(gamelink.VarsReply)
		if !ok {
			return fmt.Errorf("read game variables: unexpected reply %T", reply)
		}
		for _, value := range read.Values {
			if value.Exists {
				game.original[value.Name], game.shown[value.Name] = value.Value, value.Value
			} else {
				game.missing[value.Name] = true
			}
		}
	}

	var vars []gamelink.Assign
	for _, name := range slices.Sorted(maps.Keys(game.original)) {
		want, ok := values[name]
		if !ok {
			want = game.original[name]
		}
		if game.shown[name] != want {
			vars = append(vars, gamelink.Assign{Name: name, Value: want})
		}
	}
	if len(vars) > 0 {
		if err := p.setVars(pid, game, vars); err != nil {
			return err
		}
	}

	if len(values) == 0 {
		delete(p.games, pid)
	}
	return nil
}

func (p *GamePreview) setVars(pid uint32, game *previewGame, vars []gamelink.Assign) error {
	reply, err := p.request(pid, gamelink.SetVars{Vars: vars})
	if errors.Is(err, context.DeadlineExceeded) {
		// A game that stopped presenting keeps the request queued and applies it in order once it presents
		// again, so the values count as set and sending them again would only queue a copy.
		for _, v := range vars {
			game.shown[v.Name] = v.Value
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("set game variables: %w", err)
	}
	applied, ok := reply.(gamelink.VarsApplied)
	if !ok {
		return fmt.Errorf("set game variables: unexpected reply %T", reply)
	}
	for _, value := range applied.Values {
		if value.Exists {
			game.shown[value.Name] = value.Value
		}
	}
	return nil
}

func (p *GamePreview) request(pid uint32, msg gamelink.Message) (gamelink.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gameApplyTimeout)
	defer cancel()
	return p.service.link.Request(ctx, pid, msg)
}
