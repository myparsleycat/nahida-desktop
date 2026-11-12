package modbisect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type sessionMarker struct {
	game        string
	ModRootPath string `json:"modRootPath"`
}

func (t *Service) writeSessionMarker(ctx context.Context, game, modRootPath string) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(sessionMarker{ModRootPath: modRootPath})
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
