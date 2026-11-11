package ui

import (
	"context"
	"encoding/json"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/infra"
)

// gbViewStateKey is the renderer state row the React store persisted the GameBanana view in. The value keeps
// zustand's {"state": ..., "version": 1} shape, so a view saved by either UI restores in the other.
const gbViewStateKey = "nahida.gamebanana.view"

type gbStoredView struct {
	State   gbViewState `json:"state"`
	Version int         `json:"version"`
}

type gbViewState struct {
	SelectedGame        string            `json:"selectedGame"`
	SelectedCategoryID  *int              `json:"selectedCategoryId,omitempty"`
	CategoryBreadcrumbs []gbStoredCrumb   `json:"categoryBreadcrumbs"`
	CategorySearch      string            `json:"categorySearch"`
	SelectedMod         *gbStoredSelected `json:"selectedMod,omitempty"`
	SubfeedPage         int               `json:"subfeedPage"`
	ModsPage            int               `json:"modsPage"`
	ModSearch           string            `json:"modSearch"`
	ModsSort            string            `json:"modsSort"`
	IsModURLOpen        bool              `json:"isModUrlOpen"`
}

type gbStoredCrumb struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type gbStoredSelected struct {
	ID        int    `json:"id"`
	ModelName string `json:"modelName"`
}

// restoreGameBananaView reads the view the screen showed when the app last ran. Call it before the window
// opens.
func (s *Shell) restoreGameBananaView(ctx context.Context) {
	rows, err := s.deps.Settings.RendererStateWithPrefix(ctx, gbViewStateKey)
	if err != nil {
		s.report(err, "read-gamebanana-view")
		return
	}
	raw, ok := rows[gbViewStateKey]
	var stored gbStoredView
	if !ok || json.Unmarshal([]byte(raw), &stored) != nil {
		return
	}
	g, v := &s.gameBanana, stored.State
	g.savedView = raw
	g.game, g.categorySearch, g.modSearch, g.modURLOpen = v.SelectedGame, v.CategorySearch, v.ModSearch, v.IsModURLOpen
	if v.SelectedCategoryID != nil && *v.SelectedCategoryID > 0 {
		g.categoryID = *v.SelectedCategoryID
		g.breadcrumbs = lo.Map(
			v.CategoryBreadcrumbs,
			func(c gbStoredCrumb, _ int) gbCrumb { return gbCrumb{c.ID, c.Name} },
		)
	}
	if v.SelectedMod != nil && v.SelectedMod.ID > 0 {
		g.mod = &gbSelection{id: v.SelectedMod.ID, model: lo.CoalesceOrEmpty(v.SelectedMod.ModelName, "Mod")}
	}
	g.subfeedPage, g.modsPage = max(v.SubfeedPage, 1), max(v.ModsPage, 1)
	if v.ModsSort != "" {
		g.modsSort = v.ModsSort
	}
}

// saveGameBananaView stores the view when it changed since the last save.
func (s *Shell) saveGameBananaView() {
	g := &s.gameBanana
	view := gbViewState{
		SelectedGame:   g.game,
		CategorySearch: g.categorySearch,
		CategoryBreadcrumbs: lo.Map(g.breadcrumbs, func(c gbCrumb, _ int) gbStoredCrumb {
			return gbStoredCrumb{ID: c.id, Name: c.name}
		}),
		SubfeedPage:  g.subfeedPage,
		ModsPage:     g.modsPage,
		ModSearch:    g.modSearch,
		ModsSort:     g.modsSort,
		IsModURLOpen: g.modURLOpen,
	}
	if g.categoryID > 0 {
		view.SelectedCategoryID = lo.ToPtr(g.categoryID)
	}
	if g.mod != nil {
		view.SelectedMod = &gbStoredSelected{ID: g.mod.id, ModelName: g.mod.model}
	}
	data, err := json.Marshal(gbStoredView{State: view, Version: 1})
	if err != nil || string(data) == g.savedView {
		return
	}
	g.savedView = string(data)
	s.setRendererState(gbViewStateKey, lo.ToPtr(g.savedView))
}

func (s *Shell) reportRendererState(err error, key string) {
	_ = infra.ReportError(s.deps.Log, err, "RendererState", infra.Diagnostic{
		Operation: "renderer-state.save", Fields: map[string]any{"key": key},
	})
}
