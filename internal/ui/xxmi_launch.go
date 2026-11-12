package ui

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/reshade"
	"nahida.live/desktop/internal/tools/texture"
	"nahida.live/desktop/internal/xxmi"
)

// Launch blocker codes, kept in sync with internal/xxmi/launch_guard.go and launch_builtin.go.
const (
	blockerDCR           = "GIMI_DCR_ENABLED"
	blockerSmoothMotion  = "NVIDIA_SMOOTH_MOTION_ENABLED"
	blockerWWMIWounded   = "WWMI_WOUNDED_FX_DECISION_REQUIRED"
	blockerResourceTier  = "WWMI_RESOURCE_TIER_DECISION_REQUIRED"
	blockerD3D11Mode     = "XXMI_D3D11_MODE_NOTICE_REQUIRED"
	blockerLogging       = "XXMI_LOGGING_ENABLED"
	blockerGameFolder    = "XXMI_GAME_FOLDER_NOT_CONFIGURED"
	blockerRuntime       = "XXMI_RUNTIME_CORRUPTED"
	dialogGameFolder     = "game-folder"
	dialogRuntimeRepair  = "runtime-repair"
	dialogResourceTier   = "wwmi-resource-tier"
	dialogD3D11Mode      = "d3d11-mode"
	dialogLogging        = "xxmi-logging"
	dialogWWMIWounded    = "wwmi-wounded"
	dialogLaunchBlockers = "launch-blockers"
	dialogDCR            = "gimi-dcr"
	dialogSmoothMotion   = "smooth-motion"
)

// launchErrorCodes are matched in order, so a cause goes before the codes that wrap it.
var launchErrorCodes = []string{
	"ELEVATED_HELPER_BLOCKED_BY_ANTIVIRUS",
	"XXMI_BUSY",
	"XXMI_GAME_RUNNING",
	"XXMI_RUNTIME_LOCKED",
	"XXMI_ELEVATION_DENIED",
	"XXMI_LOADER_TOO_OLD",
	"XXMI_INJECT_FAILED",
	"XXMI_GAME_START_TIMEOUT",
	"XXMI_GAME_LAUNCHER_NOT_FOUND",
	"XXMI_LEGACY_LOADER_RUNNING",
	"XXMI_LEGACY_LOADER_EXITED",
	"XXMI_LEGACY_LOADER_NOT_READY",
	"XXMI_PLATFORM_NOT_FOUND",
	"XXMI_PLATFORM_GAME_NOT_FOUND",
	"XXMI_PLATFORM_OPTIONS_FAILED",
	"GIMI_FPS_UNLOCKER_RUNNING",
	"GIMI_FPS_UNLOCKER_CONFIG_FAILED",
	"GIMI_HDR_CONFIG_FAILED",
	"SRMI_FPS_UNLOCK_FAILED",
	"HIMI_FPS_UNLOCK_FAILED",
	"ZZMI_GAME_CONFIG_FAILED",
	"WWMI_GAME_CONFIG_FAILED",
	"RESHADE_NOT_INSTALLED",
}

var wwmiResourceTiers = []string{"UHD", "HD", "SD"}

type launchGuard struct {
	// starting marks importers whose launch is being checked or started, so their buttons wait.
	starting map[string]bool

	// importer names the launch a blocker dialog holds; the dialog is closed while it is empty.
	importer   string
	dialog     string
	confirming bool
	// keeping marks a confirmation that keeps the wounded effect instead of applying the dialog's fix.
	keeping      bool
	runtimeError string
	gameFolder   string
	resourceTier string
	detected     *[]xxmi.GameFolderCandidate
	// generation drops a confirmation still running when its dialog was dismissed, so it does not launch.
	generation int
	// keepLogging and ignoreSmoothMotion are the answers given earlier in the same launch.
	keepLogging        bool
	ignoreSmoothMotion bool

	update      *pendingUpdate
	effects     *pendingEffects
	textures    *pendingTextures
	effectsBusy bool

	// declined and ignored hold the preset effects and textures the user launched without, which are not
	// asked about again until the app restarts.
	declined map[string]bool
	ignored  map[string]bool
}

type pendingUpdate struct {
	importer string
	updates  []xxmi.UpdateStatus
	busy     bool
}

type pendingEffects struct {
	importer string
	effects  reshade.PresetEffects
}

type pendingTextures struct {
	importer string
	textures []texture.UncompressedTexture
	// progress is set while the textures are compressed.
	progress *[2]int
	cancel   context.CancelFunc
}

func newLaunchGuard() launchGuard {
	return launchGuard{
		starting: map[string]bool{}, resourceTier: "HD", declined: map[string]bool{}, ignored: map[string]bool{},
	}
}

func launchErrorCode(message string) string {
	for _, code := range launchErrorCodes {
		if strings.Contains(message, code) {
			return code
		}
	}
	return ""
}

func launchDialog(message string) string {
	switch {
	case strings.Contains(message, blockerGameFolder):
		return dialogGameFolder
	case strings.Contains(message, blockerRuntime):
		return dialogRuntimeRepair
	case strings.Contains(message, blockerResourceTier):
		return dialogResourceTier
	case strings.Contains(message, blockerD3D11Mode):
		return dialogD3D11Mode
	case strings.Contains(message, blockerLogging):
		return dialogLogging
	case strings.Contains(message, blockerWWMIWounded):
		return dialogWWMIWounded
	}
	dcr, smoothMotion := strings.Contains(message, blockerDCR), strings.Contains(message, blockerSmoothMotion)
	switch {
	case dcr && smoothMotion:
		return dialogLaunchBlockers
	case dcr:
		return dialogDCR
	case smoothMotion:
		return dialogSmoothMotion
	}
	return ""
}

func (s *Shell) startGame(ctx context.Context, importer string, keepLogging, ignoreSmoothMotion bool) error {
	switch {
	case ignoreSmoothMotion:
		return s.deps.XXMI.StartGameWithOptions(ctx, importer,
			xxmi.LaunchOptions{KeepLogging: keepLogging, IgnoreSmoothMotion: true})
	case keepLogging:
		return s.deps.XXMI.StartGameWithLogging(ctx, importer)
	}
	return s.deps.XXMI.StartGame(ctx, importer)
}

// startImporter launches an importer through the launch guard: pending updates, missing ReShade effects and
// uncompressed textures are offered first, and a launch blocker opens the dialog that resolves it. A failed check
// never keeps the game from launching.
func (s *Shell) startImporter(key string) {
	g := &s.xxmi.guard
	if g.starting[key] {
		return
	}
	g.starting[key] = true
	s.background(func(ctx context.Context) {
		updates, err := s.deps.XXMI.LaunchUpdates(ctx, key)
		fairy.Go(func() {
			if err == nil && len(updates) > 0 {
				delete(g.starting, key)
				g.update = &pendingUpdate{importer: key, updates: updates}
				return
			}
			s.launchWithEffects(key)
		})
	})
}

func (s *Shell) launchWithEffects(key string) {
	g := &s.xxmi.guard
	g.starting[key] = true
	s.background(func(ctx context.Context) {
		effects, err := s.deps.XXMI.LaunchPresetEffects(ctx, key)
		fairy.Go(func() {
			if err == nil && (slices.ContainsFunc(effects.Packages, func(pkg reshade.EffectPackage) bool {
				return !g.declined[pkg.ID]
			}) || slices.ContainsFunc(effects.Unknown, func(file string) bool { return !g.declined[file] })) {
				delete(g.starting, key)
				g.effects = &pendingEffects{importer: key, effects: effects}
				return
			}
			s.launchWithTextures(key)
		})
	})
}

func (s *Shell) launchWithTextures(key string) {
	g := &s.xxmi.guard
	g.starting[key] = true
	s.background(func(ctx context.Context) {
		found, err := s.deps.XXMI.LaunchUncompressedTextures(ctx, key)
		fairy.Go(func() {
			var textures []texture.UncompressedTexture
			if err == nil {
				for _, item := range found {
					if !g.ignored[item.Path] {
						textures = append(textures, item)
					}
				}
			}
			if len(textures) > 0 {
				delete(g.starting, key)
				g.textures = &pendingTextures{importer: key, textures: textures}
				return
			}
			s.launch(key, false, false)
		})
	})
}

func (s *Shell) launch(key string, keepLogging, ignoreSmoothMotion bool) {
	g := &s.xxmi.guard
	g.keepLogging, g.ignoreSmoothMotion = keepLogging, ignoreSmoothMotion
	g.starting[key] = true
	s.background(func(ctx context.Context) {
		err := s.startGame(ctx, key, keepLogging, ignoreSmoothMotion)
		fairy.Go(func() {
			delete(g.starting, key)
			if err != nil {
				s.launchFailed(key, err)
			}
		})
	})
}

func (s *Shell) launchFailed(key string, err error) {
	g := &s.xxmi.guard
	message := err.Error()
	if strings.Contains(message, "XXMI_NOT_CONFIGURED") || strings.Contains(message, "XXMI_IMPORTER_NOT_INSTALLED") {
		s.toasts.Show(kit.ToastInfo, T("page.setting.xxmi.builtin.importerSetupRequired", "importer", key), "")
		if s.router.Current() != RouteXXMI {
			s.navigate(RouteXXMI)
		}
		s.openXXMIPage(key)
		return
	}
	if kind := launchDialog(message); kind != "" {
		g.dialog, g.importer = kind, key
		g.runtimeError = ""
		if kind == dialogRuntimeRepair {
			g.runtimeError = message
		}
		return
	}
	if code := launchErrorCode(message); code != "" {
		if !infra.IsCancellationError(err) {
			_ = infra.ReportError(s.deps.Log, err, "XXMI", infra.Diagnostic{
				Operation: "start-game", Fields: map[string]any{"importer": key, "code": code},
			})
			s.toasts.Show(kit.ToastError, T("page.setting.xxmi.builtin.launchErrors."+code), message)
		}
		return
	}
	s.xxmiFail(err, "start-game", map[string]any{"importer": key}, "")
}

func (g *launchGuard) closeDialog() {
	g.generation++
	g.confirming, g.keeping = false, false
	g.importer, g.runtimeError, g.gameFolder = "", "", ""
	g.resourceTier = "HD"
	g.detected = nil
}

// confirmBlocker applies the dialog's fix and launches again. The resource tier, the DirectX 11 reminder and
// logging are saved answers, and the launch may still need another question, so those go through the guard again;
// after the other fixes a rejection means the fix did not take effect, which is shown instead of looping.
func (s *Shell) confirmBlocker() {
	g := &s.xxmi.guard
	if g.importer == "" || g.confirming {
		return
	}
	importer, dialog, folder, tier := g.importer, g.dialog, g.gameFolder, g.resourceTier
	keepLogging, ignoreSmoothMotion := g.keepLogging, g.ignoreSmoothMotion
	g.generation++
	generation := g.generation
	g.confirming = true
	s.background(func(ctx context.Context) {
		var warnings []string
		err := s.applyBlockerFix(ctx, importer, dialog, folder, tier, keepLogging, ignoreSmoothMotion, &warnings)
		fairy.Go(func() {
			if g.generation != generation {
				return
			}
			g.confirming = false
			for _, warning := range warnings {
				s.toasts.Show(kit.ToastWarning, warning, "")
			}
			if err != nil {
				if kind := launchDialog(err.Error()); kind != "" {
					g.dialog = kind
					g.runtimeError = ""
					if kind == dialogRuntimeRepair {
						g.runtimeError = err.Error()
					}
					return
				}
				s.xxmiFail(err, "launch-guard."+dialog, map[string]any{"importer": importer}, "")
				return
			}
			g.importer = ""
			if dialog == dialogResourceTier || dialog == dialogD3D11Mode || dialog == dialogLogging {
				s.launch(importer, keepLogging, ignoreSmoothMotion)
				return
			}
			g.starting[importer] = true
			s.background(func(ctx context.Context) {
				err := s.startGame(ctx, importer, keepLogging, ignoreSmoothMotion)
				fairy.Go(func() {
					delete(g.starting, importer)
					if err != nil {
						s.xxmiFail(err, "start-game", map[string]any{"importer": importer, "after": dialog}, "")
					}
				})
			})
		})
	})
}

func (s *Shell) applyBlockerFix(ctx context.Context, importer, dialog, folder, tier string,
	keepLogging, ignoreSmoothMotion bool, warnings *[]string,
) error {
	x := s.deps.XXMI
	update := func(change func(*xxmi.ImporterConfig) error) error {
		config, err := x.GetImporterConfig(ctx, importer)
		if err != nil {
			return err
		}
		if err := change(&config); err != nil {
			return err
		}
		return x.SaveImporterConfig(ctx, importer, config)
	}
	errNoWWMI := errors.New("WWMI settings are unavailable")
	switch dialog {
	case dialogRuntimeRepair:
		var err error
		*warnings, err = x.RepairRuntime(ctx, importer)
		return err
	case dialogGameFolder:
		if _, err := x.ValidateGameFolder(ctx, importer, folder); err != nil {
			return err
		}
		return update(func(config *xxmi.ImporterConfig) error {
			config.GameFolder = folder
			return nil
		})
	case dialogWWMIWounded:
		return update(func(config *xxmi.ImporterConfig) error {
			if config.WWMI == nil {
				return errNoWWMI
			}
			config.WoundedFXDecided, config.WWMI.DisableWoundedFX = true, true
			return nil
		})
	case dialogResourceTier:
		return update(func(config *xxmi.ImporterConfig) error {
			if config.WWMI == nil {
				return errNoWWMI
			}
			config.WWMI.ResourceTier, config.WWMI.ResourceTierDecided = tier, true
			return nil
		})
	case dialogD3D11Mode:
		return update(func(config *xxmi.ImporterConfig) error {
			config.D3D11ModeNoticeShown = true
			return nil
		})
	case dialogLogging:
		return x.DisableLogging(ctx, importer)
	}
	if ignoreSmoothMotion {
		return x.ClearLaunchBlockersWithOptions(ctx, importer,
			xxmi.LaunchOptions{KeepLogging: keepLogging, IgnoreSmoothMotion: true})
	}
	return x.ClearLaunchBlockers(ctx, importer)
}

func (s *Shell) keepWoundedFX() {
	g := &s.xxmi.guard
	if g.importer == "" || g.confirming {
		return
	}
	importer, keepLogging, ignoreSmoothMotion := g.importer, g.keepLogging, g.ignoreSmoothMotion
	g.generation++
	generation := g.generation
	g.confirming, g.keeping = true, true
	s.background(func(ctx context.Context) {
		config, err := s.deps.XXMI.GetImporterConfig(ctx, importer)
		if err == nil && config.WWMI == nil {
			err = errors.New("WWMI settings are unavailable")
		}
		if err == nil {
			config.WoundedFXDecided, config.WWMI.DisableWoundedFX = true, false
			err = s.deps.XXMI.SaveImporterConfig(ctx, importer, config)
		}
		fairy.Go(func() {
			if g.generation != generation {
				return
			}
			g.confirming, g.keeping = false, false
			if err != nil {
				s.xxmiFail(err, "launch-guard.keep-wounded", map[string]any{"importer": importer}, "")
				return
			}
			g.importer = ""
			g.starting[importer] = true
			s.background(func(ctx context.Context) {
				err := s.startGame(ctx, importer, keepLogging, ignoreSmoothMotion)
				fairy.Go(func() {
					delete(g.starting, importer)
					if err != nil {
						s.xxmiFail(err, "start-game", map[string]any{"importer": importer}, "")
					}
				})
			})
		})
	})
}

func (s *Shell) launchGuardDialogs(cx *fairy.Cx) {
	g := &s.xxmi.guard
	if g.importer != "" {
		s.blockerDialog(cx)
	}
	if u := g.update; u != nil {
		s.xxmiUpdateDialog(cx, u.importer, u.updates, T("page.setting.xxmi.builtin.launchUpdateConfirm"), u.busy,
			func() { s.installLaunchUpdates(u) }, func() {
				if !u.busy {
					g.update = nil
				}
			})
	}
	if e := g.effects; e != nil {
		skip := "page.setting.xxmi.builtin.reshade.presetEffectsIgnoreLaunch"
		if len(e.effects.Packages) > 0 {
			skip = "page.setting.xxmi.builtin.reshade.presetEffectsSkipLaunch"
		}
		s.presetEffectsDialog(cx, e.effects, T("page.setting.xxmi.builtin.reshade.presetEffectsInstallAndLaunch"),
			T(skip), &g.effectsBusy, func() { s.launchPendingEffects(false) }, func() { s.launchPendingEffects(true) },
			func() { g.effects = nil })
	}
	if g.textures != nil {
		s.texturesDialog(cx)
	}
}

// installLaunchUpdates installs the offered updates and launches; like the automatic update, a failed install
// launches with the current files.
func (s *Shell) installLaunchUpdates(u *pendingUpdate) {
	x := &s.xxmi
	if u.busy {
		return
	}
	u.busy = true
	s.background(func(ctx context.Context) {
		_, err := s.deps.XXMI.InstallUpdates(ctx, u.importer, packagesOf(u.updates))
		fairy.Go(func() {
			if err != nil && !infra.IsCancellationError(err) {
				err = infra.ReportError(s.deps.Log, err, "XXMI", infra.Diagnostic{
					Operation: "launch-updates", Fields: map[string]any{"importer": u.importer},
				})
				s.toasts.Show(
					kit.ToastWarning,
					T("page.setting.xxmi.builtin.launchUpdateFailed", "importer", u.importer),
					err.Error(),
				)
			}
			x.guard.update = nil
			x.refresh()
			s.launchWithEffects(u.importer)
		})
	})
}

func (s *Shell) launchPendingEffects(declined bool) {
	g := &s.xxmi.guard
	e := g.effects
	if e == nil {
		return
	}
	if declined {
		for _, pkg := range e.effects.Packages {
			g.declined[pkg.ID] = true
		}
		for _, file := range e.effects.Unknown {
			g.declined[file] = true
		}
	}
	g.effects = nil
	s.launchWithTextures(e.importer)
}

func (s *Shell) launchPendingTextures(ignored []string) {
	g := &s.xxmi.guard
	p := g.textures
	if p == nil {
		return
	}
	for _, path := range ignored {
		g.ignored[path] = true
	}
	g.textures = nil
	s.launch(p.importer, false, false)
}

// compressTextures compresses the offered textures and launches. A texture that could not be compressed launches
// as it is, and asking about it again would only fail the same way.
func (s *Shell) compressTextures() {
	g := &s.xxmi.guard
	p := g.textures
	if p == nil || p.progress != nil {
		return
	}
	paths := make([]string, 0, len(p.textures))
	for _, item := range p.textures {
		paths = append(paths, item.Path)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.progress = &[2]int{0, len(paths)}
	s.background(func(context.Context) {
		result, err := s.deps.XXMI.CompressLaunchTextures(ctx, p.importer, paths)
		fairy.Go(func() {
			cancel()
			if g.textures != p {
				return
			}
			p.progress, p.cancel = nil, nil
			if err != nil {
				s.xxmiFail(err, "compress-launch-textures", map[string]any{"importer": p.importer}, "")
				return
			}
			var failed []string
			for _, file := range result.Files {
				if file.Status == "failed" {
					failed = append(failed, file.FilePath)
				}
			}
			if len(failed) > 0 {
				s.toasts.Show(kit.ToastWarning,
					T("page.mod.dialog.uncompressed-textures.failed", "count", len(failed)), "")
			}
			s.launchPendingTextures(failed)
		})
	})
}

// closeTextures drops the offer. A compression in progress stops starting more files; the ones it is encoding
// are still written, and the game is not started.
func (s *Shell) closeTextures() {
	g := &s.xxmi.guard
	if p := g.textures; p != nil && p.cancel != nil {
		p.cancel()
	}
	g.textures = nil
}

func (s *Shell) texturesDialog(cx *fairy.Cx) {
	t := kit.T()
	p := s.xxmi.guard.textures
	list := fairy.Col().Gap(4).MaxH(288).ScrollY()
	for _, item := range p.textures {
		list.Child(fairy.Col(
			fairy.Text(item.RelativePath).FontSize(12).LineHeight(16).Wrap(),
			fairy.Text(itoa(item.Width)+"×"+itoa(item.Height)+" · "+formatSize(item.FileSize)).
				FontSize(12).
				LineHeight(16).
				Color(t.MutedForeground),
		).PadXY(12, 8).Radius(8).Fill(t.Muted.Alpha(0.5)))
	}
	body := fairy.Col(list).Gap(16)
	if p.progress != nil {
		body.Child(fairy.Text(T("page.mod.dialog.uncompressed-textures.progress", "done", p.progress[0],
			"total", p.progress[1])).FontSize(14).LineHeight(20).Color(t.MutedForeground).Role(fairy.RoleStatusBar))
	}
	busy := p.progress != nil
	ignore := func() {
		paths := make([]string, 0, len(p.textures))
		for _, item := range p.textures {
			paths = append(paths, item.Path)
		}
		s.launchPendingTextures(paths)
	}
	body.Child(xxmiDialogButtons(
		kit.Button(T("g.cancel"), s.closeTextures, kit.Outline),
		kit.Button(T("page.mod.dialog.uncompressed-textures.ignore"), ignore, kit.Outline, kit.Disabled(busy)),
		kit.Button(T("page.mod.dialog.uncompressed-textures.confirm"), s.compressTextures, kit.Disabled(busy)),
	))
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T("page.mod.dialog.uncompressed-textures.title"),
		Description: T("page.mod.dialog.uncompressed-textures.description", "importer", p.importer,
			"count", len(p.textures)),
		Body:    body,
		Width:   512,
		OnClose: s.closeTextures,
		Key:     "xxmi.textures",
	})
}

// presetEffectsDialog offers the effect packages a preset needs. A failed install still runs onInstalled, so a
// launch waiting on it goes ahead with the effects already there.
func (s *Shell) presetEffectsDialog(cx *fairy.Cx, effects reshade.PresetEffects, confirmLabel, skipLabel string,
	busy *bool, onInstalled, onSkip, onClose func(),
) {
	t := kit.T()
	list := fairy.Col().Gap(12).MaxH(384).ScrollY()
	for _, pkg := range effects.Packages {
		item := fairy.Col(fairy.Text(pkg.Name).FontSize(14).LineHeight(20).Medium()).Gap(2).Pad(12).Radius(8).
			Fill(t.Muted.Alpha(0.5))
		if pkg.Description != "" {
			item.Child(fairy.Text(pkg.Description).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())
		}
		list.Child(item)
	}
	if len(effects.Unknown) > 0 {
		unknown := fairy.Col().Gap(4)
		if len(effects.Packages) > 0 {
			unknown.Child(
				fairy.Text(T("page.setting.xxmi.builtin.reshade.presetEffectsUnknown")).FontSize(12).LineHeight(16).
					Color(t.MutedForeground),
			)
		}
		list.Child(
			unknown.Child(
				fairy.Text(strings.Join(effects.Unknown, ", ")).Font(monoFont).FontSize(12).LineHeight(16).Wrap(),
			),
		)
	}
	description := "page.setting.xxmi.builtin.reshade.presetEffectsUnknownOnly"
	if len(effects.Packages) > 0 {
		description = "page.setting.xxmi.builtin.reshade.presetEffectsDescription"
	}
	cancel := "g.close"
	if len(effects.Packages) > 0 || onSkip != nil {
		cancel = "g.cancel"
	}
	actions := []*fairy.El{kit.Button(T(cancel), onClose, kit.Outline, kit.Disabled(*busy))}
	if onSkip != nil {
		variant := kit.Default
		if len(effects.Packages) > 0 {
			variant = kit.Outline
		}
		actions = append(actions, kit.Button(skipLabel, onSkip, variant, kit.Disabled(*busy)))
	}
	if len(effects.Packages) > 0 {
		ids := make([]string, 0, len(effects.Packages))
		for _, pkg := range effects.Packages {
			ids = append(ids, pkg.ID)
		}
		install := func() {
			if *busy {
				return
			}
			*busy = true
			s.background(func(ctx context.Context) {
				_, err := s.deps.ReShade.InstallEffectPackages(ctx, ids)
				fairy.Go(func() {
					*busy = false
					if err != nil {
						s.xxmiFail(err, "install-effect-packages", map[string]any{"packages": ids}, "")
					}
					s.xxmi.effects.invalidate()
					onInstalled()
				})
			})
		}
		actions = append(actions, kit.Button(confirmLabel, install, kit.Icon(lucide.Download), kit.Loading(*busy)))
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T("page.setting.xxmi.builtin.reshade.presetEffectsTitle"),
		Description: T(description),
		Body:        fairy.Col(list, xxmiDialogButtons(actions...)).Gap(16),
		Width:       448,
		OnClose: func() {
			if !*busy {
				onClose()
			}
		},
		Key: "xxmi.preset-effects",
	})
}

func (s *Shell) blockerDialog(cx *fairy.Cx) {
	t := kit.T()
	g := &s.xxmi.guard
	importer, dialog := g.importer, g.dialog

	title := T("page.mod.dialog." + dialog + ".title")
	description := T("page.mod.dialog."+dialog+".description", "importer", importer)
	confirm := T("page.mod.dialog." + dialog + ".confirm")
	switch dialog {
	case dialogGameFolder:
		title, description, confirm = T("page.setting.xxmi.builtin.gameFolder"), "", T("g.save")
	case dialogRuntimeRepair:
		title = T("page.setting.xxmi.builtin.repairRuntime")
		description = T("page.setting.xxmi.builtin.runtimeRepairPrompt", "importer", importer)
		confirm = title
	}

	var body *fairy.El
	switch dialog {
	case dialogRuntimeRepair:
		if g.runtimeError != "" {
			body = fairy.Col(fairy.Text(g.runtimeError).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap()).
				MaxH(96).
				ScrollY()
		}
	case dialogGameFolder:
		label := T("page.setting.xxmi.builtin.gameFolder")
		body = fairy.Col(fairy.Row(
			kit.TextField(g.gameFolder, func(value string) { g.gameFolder = value }, kit.Label(label)).Grow(1).
				MinW(0),
			kit.Button(T("page.setting.xxmi.builtin.detectGame"), func() {
				s.detectGameFolders(importer, func(found []xxmi.GameFolderCandidate) {
					if g.importer == importer {
						g.detected = &found
					}
				})
			}, kit.Outline, kit.Disabled(g.confirming), kit.Loading(s.xxmi.busy["detect-game:"+importer])),
		).Gap(8)).Gap(8)
		if g.detected != nil {
			body.Child(gameFolderChoices(*g.detected, func(path string) { g.gameFolder = path }))
		}
	case dialogResourceTier:
		items := plainItems(wwmiResourceTiers...)
		for i := range items {
			items[i].Disabled = g.confirming
		}
		body = xxmiButtonGroup(
			g.resourceTier,
			"",
			items,
			func(value string) { g.resourceTier = value },
			kit.SizeDefault,
		).
			AlignSelf(fairy.Start)
	}

	var actions []*fairy.El
	if dialog == dialogWWMIWounded {
		actions = append(actions, kit.Button(T("page.mod.dialog.wwmi-wounded.keep"), s.keepWoundedFX, kit.Outline,
			kit.Disabled(g.confirming), kit.Loading(g.keeping)))
	} else {
		actions = append(actions, kit.Button(T("g.cancel"), g.closeDialog, kit.Outline, kit.Disabled(g.confirming)))
	}
	if dialog == dialogLogging {
		actions = append(actions, kit.Button(T("page.mod.dialog.xxmi-logging.keep"), func() {
			ignore := g.ignoreSmoothMotion
			g.closeDialog()
			s.launch(importer, true, ignore)
		}, kit.Outline, kit.Disabled(g.confirming)))
	}
	if dialog == dialogSmoothMotion || dialog == dialogLaunchBlockers {
		actions = append(actions, kit.Button(T("page.mod.dialog.smooth-motion.ignore"), func() {
			keep := g.keepLogging
			g.closeDialog()
			s.launch(importer, keep, true)
		}, kit.Outline, kit.Disabled(g.confirming)))
	}
	actions = append(actions, kit.Button(confirm, s.confirmBlocker, kit.Loading(g.confirming && !g.keeping),
		kit.Disabled(g.keeping || dialog == dialogGameFolder && strings.TrimSpace(g.gameFolder) == "")))

	width := float32(0)
	if dialog == dialogSmoothMotion || dialog == dialogLaunchBlockers {
		width = 512
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: title, Description: description, Body: body, Actions: actions, Width: width,
		OnClose: g.closeDialog, Alert: true, Key: "xxmi.launch-blocker",
	})
}

func (s *Shell) detectGameFolders(importer string, found func([]xxmi.GameFolderCandidate)) {
	var result []xxmi.GameFolderCandidate
	s.xxmiAction("detect-game:"+importer, func(ctx context.Context) error {
		var err error
		result, err = s.deps.XXMI.DetectGameFolders(ctx, importer)
		return err
	}, func() { found(result) }, nil)
}

func gameFolderChoices(candidates []xxmi.GameFolderCandidate, pick func(string)) *fairy.El {
	t := kit.T()
	if len(candidates) == 0 {
		return fairy.Text(T("page.setting.xxmi.builtin.noGameFolders")).
			FontSize(14).
			LineHeight(20).
			Color(t.MutedForeground)
	}
	list := fairy.Col().Gap(4).MaxH(192).ScrollY()
	for _, candidate := range candidates {
		path := candidate.Path
		border, fill, hover := t.Border, t.Background, t.Muted
		if t.Dark {
			border, fill, hover = t.Input, opacity(t.Input, 0.3), opacity(t.Input, 0.5)
		}
		list.Child(fairy.Row(fairy.Text(path).FontSize(14).LineHeight(20).Medium().Wrap().Grow(1).MinW(0)).
			Key(candidate.ExePath).PadXY(10, 4).Radius(t.RadiusLg()).Border(1, border).Fill(fill).
			HoverFill(hover).Animate(motion.Fast).Focusable().FocusVisibleBorder(t.Ring).
			FocusRing(3, t.Ring.Alpha(0.5)).Cursor(fairy.CursorPointer).Label(path).OnClick(func() { pick(path) }))
	}
	return list
}
