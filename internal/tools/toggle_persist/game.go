package togglepersist

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"nahida.live/desktop/internal/gamelink"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const gameApplyTimeout = 5 * time.Second

// Link is the desktop channel to the running games.
type Link interface {
	States() []gamelink.State
	Request(ctx context.Context, pid uint32, msg gamelink.Message) (gamelink.Message, error)
}

// GameApplyResult reports what reached the running games. Games is 0 when no running game loads the INI
// through the desktop channel. Missing lists the variables a game does not have, for example because the mod
// is not loaded or the game has not reloaded since the declaration was added.
type GameApplyResult struct {
	Games   int      `json:"games"`
	Applied []string `json:"applied"`
	Missing []string `json:"missing"`
}

// GameLinked reports whether a running game whose importer folder holds path accepts variable changes.
func (t *Service) GameLinked(path string) bool {
	_, linked := t.gameLinks(path)
	return linked
}

// ApplyStateToGame sets the global variables the INI declares to the values in state, in every running game
// that loads it. state is keyed by the declared name without `$`. With persistentOnly, only persist variables
// are set, as a save to the INI writes only those.
func (t *Service) ApplyStateToGame(
	ctx context.Context,
	iniPath string,
	state map[string]any,
	persistentOnly bool,
) (GameApplyResult, error) {
	links, _ := t.gameLinks(iniPath)
	if len(links) == 0 {
		return GameApplyResult{}, nil
	}
	content, _, err := readPersistINI(iniPath)
	if err != nil {
		return GameApplyResult{}, err
	}

	result := GameApplyResult{}
	for _, link := range links {
		result.Games++
		vars := gameAssignments(string(content), link.namespace, state, persistentOnly)
		if len(vars) == 0 {
			continue
		}
		requestCtx, cancel := context.WithTimeout(ctx, gameApplyTimeout)
		reply, err := t.link.Request(requestCtx, link.pid, gamelink.SetVars{Vars: vars})
		cancel()
		if err != nil {
			return result, infra.AnnotateError(err, infra.Diagnostic{
				Operation: "apply-to-game",
				Stage:     "set-vars",
				Fields: map[string]any{
					"importer":  link.importer,
					"pid":       link.pid,
					"iniPath":   iniPath,
					"variables": len(vars),
				},
			})
		}
		applied, ok := reply.(gamelink.VarsApplied)
		if !ok {
			return result, fmt.Errorf("set game variables of %s: unexpected reply %T", link.importer, reply)
		}
		for _, value := range applied.Values {
			if value.Exists {
				result.Applied = append(result.Applied, value.Name)
			} else {
				result.Missing = append(result.Missing, value.Name)
			}
		}
	}
	return result, nil
}

type gameLink struct {
	importer string
	pid      uint32
	// namespace is the one 3DMigoto gives an INI without a namespace line: its path relative to the folder of
	// the d3d11.dll, which also holds d3dx.ini.
	namespace string
}

func (t *Service) gameLinks(path string) ([]gameLink, bool) {
	if t.link == nil || path == "" {
		return nil, false
	}
	path, err := platform.LongPath(path)
	if err != nil {
		return nil, false
	}
	var links []gameLink
	for _, state := range t.link.States() {
		if state.Status != gamelink.StatusConnected || state.Info.Capabilities&gamelink.CapabilityVars == 0 {
			continue
		}
		relative, ok := gameRelativePath(state.Info.ImporterFolder, path)
		if !ok || slices.ContainsFunc(strings.Split(relative, string(filepath.Separator)), func(name string) bool {
			return strings.HasPrefix(strings.ToLower(name), "disabled")
		}) {
			continue
		}
		links = append(links, gameLink{
			importer: state.Importer, pid: state.PID,
			namespace: strings.ReplaceAll(relative, string(filepath.Separator), `\`),
		})
	}
	return links, len(links) > 0
}

func gameRelativePath(folder, path string) (string, bool) {
	if folder == "" {
		return "", false
	}
	if long, err := platform.LongPath(folder); err == nil {
		folder = long
	}
	if relative, ok := childRelative(folder, path); ok {
		return relative, true
	}

	for _, prefix := range []string{"", "Mods"} {
		root, err := platform.FinalPath(filepath.Join(folder, prefix))
		if err != nil {
			continue
		}
		var relative string
		var found bool

		// Match the highest ancestor so junctions below it retain their loaded names in the namespace.
		for current := path; ; current = filepath.Dir(current) {
			if physical, err := platform.FinalPath(current); err == nil {
				if parent, ok := childRelative(root, physical); ok {
					if suffix, err := filepath.Rel(current, path); err == nil {
						relative, found = filepath.Join(prefix, parent, suffix), true
					}
				}
			}
			if platform.SamePathFold(current, filepath.Dir(current)) {
				break
			}
		}
		if found {
			return relative, true
		}
	}
	return "", false
}

func childRelative(root, path string) (string, bool) {
	if root == "" || !platform.SameOrChildPath(root, path) {
		return "", false
	}
	relative, err := filepath.Rel(root, path)
	return relative, err == nil
}

func gameAssignments(content, namespace string, state map[string]any, persistentOnly bool) []gamelink.Assign {
	values := make(map[string]float32, len(state))
	for name, value := range state {
		if number, ok := gameValue(value); ok {
			values[strings.ToLower(strings.TrimSpace(name))] = number
		}
	}

	targets, _ := parsePersistTargets(content, namespace)
	var vars []gamelink.Assign
	seen := map[string]bool{}
	for _, target := range targets {
		if persistentOnly && !target.persistent {
			continue
		}
		value, ok := values[strings.ToLower(target.varName)]
		if !ok || seen[target.key] {
			continue
		}
		seen[target.key] = true
		vars = append(vars, gamelink.Assign{Name: target.key, Value: value})
	}
	return vars
}

func gameValue(value any) (float32, bool) {
	switch typed := value.(type) {
	case float64:
		return float32(typed), true
	case float32:
		return typed, true
	case int:
		return float32(typed), true
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 32)
		return float32(number), err == nil
	}
	return 0, false
}
