package modbisect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	// sessionMarkerPrefix keys an app_state row that exists while a session may have renamed INI files, so
	// startup recovery walks only those mod folders instead of every configured one.
	sessionMarkerPrefix = "mod_bisect_session:"
	// legacyScanDoneKey records that the full walk for orphans left by builds without session markers ran.
	legacyScanDoneKey = "mod_bisect_marker_scan_done"
)

// sessionMarker records the mod folder of a session and, relative to it with forward slashes, every INI the
// session may rename. Markers from older builds have no Paths.
type sessionMarker struct {
	game        string
	ModRootPath string   `json:"modRootPath"`
	Paths       []string `json:"paths,omitempty"`
}

func (t *Service) writeSessionMarker(ctx context.Context, game, modRootPath string, candidates []string) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		relative, err := filepath.Rel(modRootPath, candidate)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relative))
	}
	raw, err := json.Marshal(sessionMarker{ModRootPath: modRootPath, Paths: paths})
	if err != nil {
		return err
	}
	return client.AppState.Upsert(ctx, sessionMarkerPrefix+game, string(raw), appStateTimestamp())
}

// clearSessionMarker only logs a failure: a marker left behind costs one walk of its folder on the next launch,
// which then removes it.
func (t *Service) clearSessionMarker(ctx context.Context, game string) {
	client, err := t.requireClient()
	if err == nil {
		err = client.AppState.Delete(ctx, sessionMarkerPrefix+game)
	}
	t.logError(err, "ModBisect:clearSessionMarker")
}

func (t *Service) sessionMarkers(ctx context.Context) ([]sessionMarker, error) {
	client, err := t.requireClient()
	if err != nil {
		return nil, err
	}
	rows, err := client.AppState.ListByPrefix(ctx, sessionMarkerPrefix)
	if err != nil {
		return nil, err
	}

	markers := make([]sessionMarker, 0, len(rows))
	for _, row := range rows {
		// LIKE treats the underscores in the prefix as wildcards.
		game, ok := strings.CutPrefix(row.Key, sessionMarkerPrefix)
		if !ok {
			continue
		}
		marker := sessionMarker{game: game}
		err := json.Unmarshal([]byte(row.Value), &marker)
		if err == nil && marker.ModRootPath == "" {
			err = errors.New("missing mod root path")
		}
		if err != nil {
			t.logError(fmt.Errorf("invalid bisect session marker for %q: %w", game, err), "ModBisect:readSessionMarker")
			continue
		}
		markers = append(markers, marker)
	}
	return markers, nil
}

func appStateTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
