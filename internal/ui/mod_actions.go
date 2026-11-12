package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/agent"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
)

var supportedArchives = []string{".zip", ".rar", ".7z"}

// The paste preview fills the default dialog body width at this height.
const (
	pastePreviewWidth  float32 = 384
	pastePreviewHeight float32 = 256
)

func (s *Shell) openPath(path string) {
	go func() {
		if err := s.deps.Platform.OpenPath(path); err != nil {
			fairy.Go(func() { s.toasts.Show(kit.ToastError, err.Error(), "") })
		}
	}()
}

func (s *Shell) refreshFixInspections() {
	if s.deps.Tools == nil {
		return
	}
	s.background(func(ctx context.Context) {
		snapshot := s.deps.Tools.RefreshFixInspections(ctx)
		fairy.Go(func() { s.setFixInspections(snapshot) })
	})
}

// confirmTrash asks before moving path to the recycle bin and runs done on the UI thread after it was moved.
func (s *Shell) confirmTrash(path, title, description, failed string, done func()) {
	if failed == "" {
		failed = T("page.mod.toast.trash-error")
	}
	s.Confirm(Confirmation{
		Title: title, Description: description, ConfirmLabel: T("g.delete"), Danger: true,
		OnResult: func(confirmed bool) {
			if !confirmed {
				return
			}
			s.background(func(context.Context) {
				err := s.deps.Platform.Trash(path)
				if err != nil {
					s.modError(err, "trash", map[string]any{"path": path})
				}
				fairy.Go(func() {
					if err != nil {
						s.toasts.Show(kit.ToastError, failed, "")
						return
					}
					done()
				})
			})
		},
	})
}

// toggleMod enables or disables a mod; exclusive also disables the other mods of its folder. Enabling a mod
// with a missing dependency asks first.
func (s *Shell) toggleMod(info mod.ModInfo, exclusive bool) {
	m := &s.mods
	if m.group == nil {
		return
	}
	groupPath := m.group.Path
	s.confirmEnable(info, func() { s.runToggle(info, groupPath, exclusive) })
}

func (s *Shell) confirmEnable(info mod.ModInfo, enable func()) {
	m := &s.mods
	if info.IsEnabled {
		enable()
		return
	}
	check := func(scanned mod.ModInfo) {
		missing := missingDependencies(scanned)
		if len(missing) == 0 {
			enable()
			return
		}
		s.Confirm(Confirmation{
			Title:        T("page.mod.dialog.missing-dependencies.title"),
			Description:  T("page.mod.dialog.missing-dependencies.description", "name", scanned.Name),
			ConfirmLabel: T("page.mod.dialog.missing-dependencies.confirm"),
			Body: func() *fairy.El {
				badges := fairy.Row().Wrap().Gap(6)
				for _, name := range missing {
					badges.Child(kit.Badge(name, kit.Destructive, kit.Icon(lucide.Package)))
				}
				return badges
			},
			OnResult: func(confirmed bool) {
				if confirmed {
					enable()
				}
			},
		})
	}
	find := func(group *mod.FolderGroup) mod.ModInfo {
		if group != nil {
			if i := slices.IndexFunc(group.Mods, func(c mod.ModInfo) bool { return c.Path == info.Path }); i >= 0 {
				return group.Mods[i]
			}
		}
		return info
	}
	if m.listingFull {
		check(find(m.listing))
		return
	}

	// The quick listing carries no dependencies, so an enable waits for the full scan.
	if m.pendingEnable[info.Path] {
		return
	}
	m.pendingEnable[info.Path] = true
	path := m.group.Path
	s.background(func(ctx context.Context) {
		group, err := s.deps.Mod.GetMods(ctx, path)
		fairy.Go(func() {
			delete(m.pendingEnable, info.Path)
			if err != nil {
				s.modError(err, "read-mods", map[string]any{"path": path})
				s.toasts.Show(kit.ToastError, err.Error(), "")
				return
			}
			s.applyListing(path, group)
			check(find(&group))
		})
	})
}

// runToggle renames the mod folder. A second request for the same mod would target a path that no longer
// exists, and an exclusive toggle renames the whole folder, so overlapping requests are refused.
func (s *Shell) runToggle(info mod.ModInfo, groupPath string, exclusive bool) {
	m := &s.mods
	if _, busy := m.toggling[info.Path]; busy {
		return
	}
	if m.exclusive[groupPath] || exclusive && slices.Contains(slices.Collect(maps.Values(m.toggling)), groupPath) {
		s.toasts.Show(kit.ToastInfo, T("page.mod.hooks.use-mod-mutations.toggle-mutation.busy"), "")
		return
	}
	m.toggling[info.Path] = groupPath
	if exclusive {
		m.exclusive[groupPath] = true
	}
	s.background(func(ctx context.Context) {
		var err error
		if exclusive {
			_, err = s.deps.Mod.ExclusiveToggle(ctx, info.Path)
		} else {
			_, err = s.deps.Mod.Toggle(ctx, info.Path)
		}
		var group mod.FolderGroup
		if err == nil {
			group, err = s.deps.Mod.GetMods(ctx, groupPath)
		}
		fairy.Go(func() {
			delete(m.toggling, info.Path)
			if exclusive {
				delete(m.exclusive, groupPath)
			}
			if err != nil {
				s.showModToggleError(err)
				return
			}
			s.applyListing(groupPath, group)
		})
	})
}

func (s *Shell) showModToggleError(err error) {
	s.showFolderError(err, "page.mod.hooks.use-mod-mutations.toggle-mutation.")
}

// showFolderError maps the errors of a folder rename to the messages under prefix.
func (s *Shell) showFolderError(err error, prefix string) {
	text := err.Error()
	switch {
	case strings.Contains(text, "ALREADY_EXISTS"):
		_, name, _ := strings.Cut(text, "ALREADY_EXISTS:")
		if name == "" {
			name = T("g.unknown")
		}
		s.toasts.Show(kit.ToastError, T(prefix+"0", "name", name), "")
	case strings.Contains(text, "MOD_FOLDER_LOCKED"):
		if _, processes, _ := strings.Cut(text, "MOD_FOLDER_LOCKED|"); processes != "" {
			s.toasts.Show(kit.ToastWarning, T(prefix+"1", "processes", processes), "")
		} else {
			s.toasts.Show(kit.ToastWarning, T(prefix+"1-fallback"), "")
		}
	case strings.Contains(text, "MOD_DOWNLOAD_IN_PROGRESS"):
		s.toasts.Show(kit.ToastWarning, T("page.mod.download_action_blocked"), "")
	case prefix == "page.mod.hooks.use-mod-mutations.rename-mutation." &&
		(strings.Contains(text, "INVALID_WINDOWS_FILENAME") || strings.Contains(text, "INVALID_MOD_NAME")):
		s.toasts.Show(kit.ToastError, T(prefix+"2"), "")
	case prefix == "page.mod.hooks.use-mod-mutations.rename-mutation.":
		s.toasts.Show(kit.ToastError, T(prefix+"3"), "")
	default:
		s.toasts.Show(kit.ToastError, T(prefix+"2"), "")
	}
}

func (s *Shell) deleteMod(info mod.ModInfo) {
	s.confirmTrash(info.Path, T("page.mod.dialog.delete-mod.title"), T("page.mod.dialog.delete-mod.description"), "",
		func() {
			s.refreshListing()
			s.refreshFixInspections()
		})
}

func (s *Shell) deletePreview(info mod.ModInfo) {
	if info.Preview == nil {
		return
	}
	s.confirmTrash(*info.Preview, T("page.mod.dialog.delete-preview.title"),
		T("page.mod.dialog.delete-preview.description", "name", info.Name), "", s.refreshListing)
}

func (s *Shell) openPreviewViewer(info mod.ModInfo) {
	if info.Preview == nil {
		return
	}
	file := *info.Preview
	go func() {
		if err := s.deps.Platform.OpenExternal(file); err != nil {
			s.toasts.Show(kit.ToastError, T("page.mod.toast.open-preview-failed"), err.Error())
		}
	}()
}

// openModLightbox shows the mod's preview large over the window.
func (s *Shell) openModLightbox(info mod.ModInfo) {
	if info.Preview != nil && isPreviewMedia(*info.Preview) {
		s.mods.lightbox = &info
	}
}

func (s *Shell) modLightboxDialog(cx *fairy.Cx) {
	info := s.mods.lightbox
	if info == nil {
		return
	}
	file := *info.Preview
	windowW, windowH := cx.Size()
	white := fairy.Hex(0xffffff)
	var content *fairy.El
	if isPreviewVideo(file) {
		content = fairy.Spinner().Size(28, 28).Color(white)
		if video := s.modVideo(file, info.Mtime); video != nil && video.State() != fairy.VideoLoading {
			content = fairy.VideoView(video).WFull().HFull()
		}
	} else {
		key := fmt.Sprint("mod-lightbox:", file, ":", info.Mtime)
		img := fairy.Images.Sized(key, windowW*0.8, windowH*0.8, fairy.FitContain, func() ([]byte, error) {
			return os.ReadFile(file)
		})
		switch {
		case img != nil:
			content = fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitContain).Label(info.Name)
		case fairy.Images.SizedErr(key, windowW*0.8, windowH*0.8, fairy.FitContain) != nil:
			content = fairy.Icon(lucide.ImageOff).Size(48, 48).Color(white)
		default:
			content = fairy.Spinner().Size(28, 28).Color(white)
		}
	}
	lightbox{key: "mod.lightbox", label: info.Name, content: content, close: func() { s.mods.lightbox = nil }}.show(cx)
}

// gameBananaSource is the GameBanana mod ID read from the metadata of the mod whose context menu was opened last.
// An id of 0 means it is still loading or the mod did not come from GameBanana.
type gameBananaSource struct {
	path string
	id   int64
}

// gameBananaModID returns the cached GameBanana ID of modPath and starts reading it when another mod was cached.
func (s *Shell) gameBananaModID(modPath string) int64 {
	m := &s.mods
	if m.gameBanana.path != "" && platform.SamePathFold(m.gameBanana.path, modPath) {
		return m.gameBanana.id
	}
	m.gameBanana = gameBananaSource{path: modPath}
	s.background(func(ctx context.Context) {
		id, err := s.deps.Mod.GetGameBananaModID(ctx, modPath)
		fairy.Go(func() {
			if m.gameBanana.path != modPath {
				return
			}
			if err != nil {
				s.modError(err, "mod.context-menu.gamebanana-id", map[string]any{"modPath": modPath})
				m.gameBanana = gameBananaSource{}
				return
			}
			if id != nil {
				m.gameBanana.id = *id
			}
		})
	})
	return 0
}

func (s *Shell) modMenuItems(info mod.ModInfo) []kit.MenuItem {
	nte := s.isNteGame()
	var items []kit.MenuItem
	if id := s.gameBananaModID(info.Path); id > 0 {
		items = append(items,
			kit.MenuItem{
				Label: T("page.mod.context-menu.open-gamebanana"), Icon: lucide.ExternalLink,
				OnClick: func() { s.navigatePath("/gamebanana?mod=" + strconv.FormatInt(id, 10)) },
			},
			kit.MenuItem{Separator: true},
		)
	}
	items = append(items, kit.MenuItem{Label: T("page.mod.context-menu.group-preview"), Heading: true})
	if info.Preview != nil && isPreviewMedia(*info.Preview) {
		items = append(items, kit.MenuItem{
			Label: T("page.mod.context-menu.open-preview-viewer"), Icon: lucide.Image,
			OnClick: func() { s.openPreviewViewer(info) },
		})
	}
	items = append(items, kit.MenuItem{
		Label: T(
			"page.mod.context-menu.paste-preview",
		),
		Icon:    lucide.Clipboard,
		OnClick: func() { s.pastePreview(info) },
	})
	if info.Preview != nil && isPreviewMedia(*info.Preview) {
		items = append(items, kit.MenuItem{
			Label: T("page.mod.context-menu.delete-preview"), Icon: lucide.Trash, Danger: true,
			OnClick: func() { s.deletePreview(info) },
		})
	}
	items = append(items, kit.MenuItem{Separator: true})
	if !nte && s.deps.Tools != nil {
		items = append(items, s.fixMenuItems(info.Path)...)
	}
	items = append(items, kit.MenuItem{Label: T("page.mod.context-menu.group-mod"), Heading: true})
	if !nte {
		items = append(items, kit.MenuItem{
			Label: T("page.mod.context-menu.open-cmd"), Icon: lucide.SquareTerminal,
			OnClick: func() {
				go func() {
					if err := s.deps.Platform.OpenCmd(info.Path); err != nil {
						s.toasts.Show(kit.ToastError, err.Error(), "")
					}
				}()
			},
		})
	}
	items = append(items, kit.MenuItem{
		Label: T("page.mod.context-menu.open-folder"), Icon: lucide.Folder, OnClick: func() { s.openPath(info.Path) },
	})
	if !nte {
		items = append(items, kit.MenuItem{
			Label: T("page.mod.context-menu.mark-manual-subgroup"), Icon: lucide.FolderTree,
			OnClick: func() { s.setManualSubGroup(info.Path, true) },
		})
	}
	items = append(items,
		kit.MenuItem{
			Label:   T("page.mod.context-menu.rename"),
			Icon:    lucide.Pencil,
			OnClick: func() { s.mods.renameDraft = &renameDraft{mod: info, groupPath: s.mods.group.Path, name: info.Name} },
		},
		kit.MenuItem{Separator: true},
	)
	items = append(items,
		kit.MenuItem{Label: T("page.mod.context-menu.group-tools"), Heading: true},
		kit.MenuItem{
			Label: T("page.agent.title"), Icon: lucide.Sparkles,
			OnClick: func() {
				s.openAgentScope(agent.AgentScope{Type: "mod", ModPath: info.Path, ModName: info.Name})
			},
		},
	)
	items = append(append(items, s.modToolItems(info, nte)...), kit.MenuItem{Separator: true})
	return append(items,
		kit.MenuItem{Label: T("g.delete"), Icon: lucide.Trash, Danger: true, OnClick: func() { s.deleteMod(info) }},
	)
}

// modToolItems lists the tools that work on one mod.
func (s *Shell) modToolItems(info mod.ModInfo, nte bool) []kit.MenuItem {
	if nte || s.deps.Tools == nil {
		return nil
	}
	opening := s.modelViewerOpening(info.Path)
	return append([]kit.MenuItem{{
		Label: T("page.tools.model_viewer.title"), Icon: lucide.Box, Loading: opening, Disabled: opening,
		OnClick: func() { s.openModelViewer(info.Path) },
	}}, s.modFileToolItems(info)...)
}

// modFileToolItems lists the tools that work on one mod's files, which the context menu and the card's tools
// menu share.
func (s *Shell) supportsLiquidEmitter() bool {
	game := s.gameConfig()
	return game != nil && deref(game.Importer) != "" && s.deps.Tools != nil &&
		slices.Contains(s.deps.Tools.LiquidEmitterImporters(), deref(game.Importer))
}

func (s *Shell) modFileToolItems(info mod.ModInfo) []kit.MenuItem {
	items := []kit.MenuItem{
		{
			Label: T("page.tools.texture_resizer.title"), Icon: lucide.Image,
			OnClick: func() { s.openTextureResize(info) },
		},
		{
			Label: T("page.tools.menu_maker.title"), Icon: lucide.PanelsTopLeft,
			OnClick: func() { s.openMenuMaker(info.Path, info.Name) },
		},
		{
			Label: T("page.tools.body_shape.title") + " (" + T("g.beta") + ")", Icon: lucide.PersonStanding,
			OnClick: func() { s.openBodyShapeDialog(info.Path, info.Name) },
		},
		{
			Label: T("page.tools.touch_profile.title") + " (" + T("g.beta") + ")", Icon: lucide.Sparkles,
			OnClick: func() { s.openTouchProfileDialog(info.Path, info.Name) },
		},
	}
	if s.isEfmiGame() {
		items = append(items, kit.MenuItem{
			Label: T("page.tools.efmi_outline.title"), Icon: lucide.Layers,
			OnClick: func() { s.openOutline(info.Path, info.Name) },
		})
	}
	if s.supportsLiquidEmitter() {
		items = append(items, kit.MenuItem{
			Label: T("page.tools.liquid_emitter.title") + " (" + T("g.beta") + ")", Icon: lucide.Droplets,
			OnClick: func() { s.openLiquidEmitterDialog(info.Path, info.Name) },
		})
	}
	return append(items, kit.MenuItem{
		Label: T("page.mod.context-menu.find-conflict"), Icon: lucide.Swords,
		OnClick: func() { s.openConflictFinder(info) },
	})
}

func (s *Shell) previewMenuItems(info mod.ModInfo) []kit.MenuItem {
	return []kit.MenuItem{
		{
			Label: T("page.mod.context-menu.open-preview-lightbox"), Icon: lucide.ZoomIn,
			OnClick: func() { s.openModLightbox(info) },
		},
		{
			Label: T("page.mod.context-menu.open-preview-viewer"), Icon: lucide.Image,
			OnClick: func() { s.openPreviewViewer(info) },
		},
		{Separator: true},
		{
			Label:   T("page.mod.context-menu.paste-preview"),
			Icon:    lucide.Clipboard,
			OnClick: func() { s.pastePreview(info) },
		},
		{
			Label: T("page.mod.context-menu.delete-preview"), Icon: lucide.Trash, Danger: true,
			OnClick: func() { s.deletePreview(info) },
		},
	}
}

// stepPreview makes the previous or next image of the mod its default preview.
func (s *Shell) stepPreview(info mod.ModInfo, direction int) {
	m := &s.mods
	images := info.PreviewImages
	if len(images) < 2 || m.stepping[info.Path] {
		return
	}
	next := 0
	if current := slices.Index(images, deref(info.Preview)); current >= 0 {
		next = (current + direction + len(images)) % len(images)
	} else if direction < 0 {
		next = len(images) - 1
	}
	image := images[next]
	m.stepping[info.Path] = true
	groupPath := m.group.Path
	s.background(func(ctx context.Context) {
		err := s.deps.Mod.SetDefaultPreview(ctx, info.Path, image)
		fairy.Go(func() {
			delete(m.stepping, info.Path)
			if err != nil {
				s.toasts.Show(kit.ToastError, T("page.mod.toast.change-preview-error"), "")
				return
			}
			if m.group != nil && m.group.Path == groupPath && m.listing != nil {
				listing := *m.listing
				listing.Mods = slices.Clone(listing.Mods)
				for i := range listing.Mods {
					if listing.Mods[i].Path == info.Path {
						listing.Mods[i].Preview = &image
					}
				}
				m.listing = &listing
			}
			s.refreshListing()
		})
	})
}

const modRenameFieldKey = "mod.rename.name"

type renameDraft struct {
	mod       mod.ModInfo
	groupPath string
	name      string
	busy      bool
	selected  bool
}

func (s *Shell) renameDialog(cx *fairy.Cx) {
	m := &s.mods
	d := m.renameDraft
	if d == nil {
		return
	}
	submit := func(string) {
		name := strings.TrimSpace(d.name)
		if d.busy {
			return
		}
		if name == "" {
			s.toasts.Show(kit.ToastError, T("page.mod.hooks.use-mod-mutations.rename-mutation.2"), "")
			return
		}
		d.busy = true
		s.background(func(ctx context.Context) {
			_, err := s.deps.Mod.Rename(ctx, d.mod.Path, name)
			var group mod.FolderGroup
			if err == nil {
				group, err = s.deps.Mod.GetMods(ctx, d.groupPath)
			}
			fairy.Go(func() {
				d.busy = false
				if err != nil {
					s.showFolderError(err, "page.mod.hooks.use-mod-mutations.rename-mutation.")
					return
				}
				m.renameDraft = nil
				s.applyListing(d.groupPath, group)
			})
		})
	}
	close := func() {
		if !d.busy {
			m.renameDraft = nil
		}
	}
	field := kit.TextField(d.name, func(v string) { d.name = v },
		kit.Placeholder(T("page.mod.dialog.rename-mod.name-placeholder")), kit.Disabled(d.busy))
	field.Key(modRenameFieldKey).AutoFocus().OnSubmit(submit)
	if !d.selected {
		d.selected = true
		// The field is placed after this build, so the selection waits for it.
		fairy.Go(func() { s.win.EditInput(modRenameFieldKey, fairy.EditSelectAll) })
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T("page.mod.dialog.rename-mod.title"),
		Description: T("page.mod.dialog.rename-mod.description"),
		Body:        field,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline, kit.Disabled(d.busy)),
			kit.Button(T("page.mod.dialog.rename-mod.confirm"), func() { submit(d.name) }, kit.Loading(d.busy)),
		},
		OnClose: close,
		Key:     "mod.rename",
	})
}

// pasteDraft is a preview read from the clipboard: a file path, an image URL or image data.
type pasteDraft struct {
	mod       mod.ModInfo
	groupPath string
	kind      string
	data      string
	image     *fairy.Image
}

// pastePreview reads an image from the clipboard as the React screen did: a copied image file, then an image
// URL, then image data.
func (s *Shell) pastePreview(info mod.ModInfo) {
	m := &s.mods
	draft := &pasteDraft{mod: info, groupPath: m.group.Path}
	files := s.deps.Platform.GetClipboardFiles()
	text := strings.TrimSpace(s.win.ClipboardText())
	switch {
	case len(files) > 0 && isPreviewImage(files[0]):
		draft.kind, draft.data = "path", files[0]
	case strings.HasPrefix(text, "http") && isPreviewImage(strings.SplitN(text, "?", 2)[0]):
		draft.kind, draft.data = "url", text
	default:
		img, ok := s.win.ClipboardImage()
		if !ok {
			s.toasts.Show(kit.ToastWarning, T("page.mod.toast.paste-preview.no-image"), "")
			return
		}
		data, err := encodePNGDataURL(img)
		if err != nil {
			s.modError(err, "paste-preview", map[string]any{"stage": "encode"})
			s.toasts.Show(kit.ToastError, T("page.mod.toast.paste-preview.clipboard-error"), "")
			return
		}
		draft.kind, draft.data, draft.image = "base64", data, fairy.NewImage(img)
	}
	m.pasteDraft = draft
}

func encodePNGDataURL(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func (s *Shell) pasteDialog(cx *fairy.Cx) {
	m := &s.mods
	d := m.pasteDraft
	if d == nil {
		return
	}
	prefix := "page.mod.dialog.paste-preview"
	if d.mod.Preview != nil {
		prefix = "page.mod.dialog.overwrite-preview"
	}
	var body *fairy.El
	switch {
	case d.image != nil:
		body = fairy.ImageEl(d.image)
	case d.kind == "path":
		if img := fairy.Images.Sized(
			"paste:"+d.data,
			pastePreviewWidth,
			pastePreviewHeight,
			fairy.FitContain,
			func() ([]byte, error) { return os.ReadFile(d.data) },
		); img != nil {
			body = fairy.ImageEl(img)
		}
	case d.kind == "url":
		// The site can refuse this request while the backend's download still succeeds, so a failure only
		// hides the picture.
		if img := s.httpImage("paste:", d.data, pastePreviewWidth, pastePreviewHeight); img != nil {
			body = fairy.ImageEl(img)
		} else if fairy.Images.SizedErr("paste:"+d.data, pastePreviewWidth, pastePreviewHeight, fairy.FitContain) == nil {
			body = fairy.Box(fairy.Spinner().Size(20, 20)).Center()
		}
	}
	if body != nil {
		body = fairy.Box(body.WFull().HFull().Fit(fairy.FitContain)).
			WFull().
			H(pastePreviewHeight).
			Radius(6).
			Fill(kit.T().Muted).
			Clip()
	}
	close := func() { m.pasteDraft = nil }
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T(prefix + ".title"),
		Alert:       true,
		Description: T(prefix+".description", "name", d.mod.Name),
		Body:        body,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline),
			kit.Button(T(prefix+".confirm"), func() {
				m.pasteDraft = nil
				s.savePastedPreview(d)
			}),
		},
		OnClose: close,
		Key:     "mod.paste-preview",
	})
}

func (s *Shell) savePastedPreview(d *pasteDraft) {
	failed := map[string]string{
		"path":   "page.mod.toast.paste-preview.copy-error",
		"url":    "page.mod.toast.paste-preview.download-error",
		"base64": "page.mod.toast.paste-preview.save-error",
	}[d.kind]
	// The download can take a while, so the loading toast stays until the preview itself shows the result.
	var loading kit.ToastID
	if d.kind == "url" {
		loading = s.toasts.Loading(T("page.mod.toast.paste-preview.downloading"), "")
	}
	s.background(func(ctx context.Context) {
		_, err := s.deps.Mod.PastePreview(ctx, d.mod.Path, d.data, d.kind, nil)
		if err != nil {
			s.modError(err, "paste-preview", map[string]any{"path": d.mod.Path, "kind": d.kind})
		}
		fairy.Go(func() {
			switch {
			case err != nil && loading != 0:
				s.toasts.Update(loading, kit.ToastError, T(failed), "")
				return
			case err != nil:
				s.toasts.Show(kit.ToastError, T(failed), "")
				return
			case loading != 0:
				s.toasts.Dismiss(loading)
			}
			if m := &s.mods; m.group != nil && m.group.Path == d.groupPath {
				s.refreshListing()
			}
		})
	})
}

type extractAsk struct {
	fileName string
	answer   chan string
}

type downloadAsk struct {
	requestID string
	fileName  string
}

// dropIntoGroup adds dropped folders and archives to the folder at groupPath, one after another, as the
// React screen did. Archives may ask where their contents go.
func (s *Shell) dropIntoGroup(groupPath string, paths []string) {
	game := s.mods.game
	s.background(func(ctx context.Context) {
		for _, path := range paths {
			name := filepath.Base(path)
			info, statErr := os.Stat(path)
			isDir := statErr == nil && info.IsDir()
			archive := slices.Contains(supportedArchives, strings.ToLower(filepath.Ext(path)))
			if !isDir && !archive {
				s.toasts.Show(
					kit.ToastWarning,
					T("page.mod.drag_drop.unsupported_file_archive_only", "fileName", name),
					"",
				)
				continue
			}
			if archive {
				mode, err := s.archiveExtractMode(ctx, name, path)
				if err != nil {
					s.modError(err, "drop-archive", map[string]any{"path": path, "stage": "mode"})
					s.toasts.Show(kit.ToastError, T("page.mod.drag_drop.processing_error"), "")
					continue
				}
				if mode == "" {
					continue
				}
				go s.importDropped(game, groupPath, path, "extract", func(ctx context.Context) error {
					_, err := s.deps.Mod.ExtractArchiveToGroup(ctx, path, groupPath, mode)
					return err
				})
				continue
			}
			go s.importDropped(game, groupPath, path, "copy", func(ctx context.Context) error {
				_, err := s.deps.Mod.CopyFolderToGroup(ctx, path, groupPath)
				return err
			})
		}
	})
}

// archiveExtractMode reads where an archive's contents go. With "ask_every_time", an archive with one folder at
// its top asks the user; "" means the user cancelled.
func (s *Shell) archiveExtractMode(ctx context.Context, name, path string) (string, error) {
	value, err := s.deps.Settings.Get(ctx, setting.KeyModArchiveExtractPathMode)
	if err != nil {
		return "", err
	}
	mode := asString(value)
	if mode != "ask_every_time" {
		return mode, nil
	}
	single, err := s.deps.Mod.HasSingleTopLevelDirectory(ctx, path)
	if err != nil {
		return "", err
	}
	if !single {
		return "flatten_single_root", nil
	}
	answer := make(chan string, 1)
	fairy.Go(func() { s.mods.extractAsks = append(s.mods.extractAsks, extractAsk{fileName: name, answer: answer}) })
	select {
	case mode := <-answer:
		return mode, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *Shell) importDropped(game, groupPath, path, kind string, run func(context.Context) error) {
	name := filepath.Base(path)
	toast := s.toasts.Loading(T("page.mod.drag_drop."+kind+".loading", "fileName", name), "")
	err := run(context.Background())
	if err != nil {
		err = infra.ReportError(s.deps.Log, err, "Mod", infra.Diagnostic{
			Operation: "drop-" + kind, Fields: map[string]any{"path": path, "groupPath": groupPath},
		})
	}
	fairy.Go(func() {
		if err != nil {
			if _, folder, ok := strings.Cut(err.Error(), "ALREADY_EXISTS:"); ok {
				key := "page.mod.drag_drop.already_exists_item"
				if kind == "extract" {
					key = "page.mod.drag_drop.already_exists_folder"
				}
				s.toasts.Update(toast, kit.ToastError, T(key, "folderName", folder), "")
				return
			}
			s.toasts.Update(toast, kit.ToastError, T("page.mod.drag_drop."+kind+".error", "fileName", name), "")
			return
		}
		s.toasts.Update(toast, kit.ToastSuccess, T("page.mod.drag_drop."+kind+".success", "fileName", name), "")
		if s.mods.game == game {
			s.refreshFolders()
			if s.mods.group != nil {
				s.refreshListing()
			}
		}
	})
}

// extractDialog asks where an archive's contents go, for a dropped archive or a download.
func (s *Shell) extractDialog(cx *fairy.Cx) {
	m := &s.mods
	var fileName string
	var answer func(mode string)
	switch {
	case m.downloadAsk != nil:
		ask := m.downloadAsk
		fileName = ask.fileName
		answer = func(mode string) {
			m.downloadAsk = nil
			var choice *string
			if mode != "" {
				choice = &mode
			}
			go func() {
				if err := s.deps.Mod.ResolveArchiveExtractPrompt(ask.requestID, choice); err != nil {
					s.modError(err, "resolve-archive-prompt", map[string]any{"requestId": ask.requestID})
				}
			}()
		}
	case len(m.extractAsks) > 0:
		ask := m.extractAsks[0]
		fileName = ask.fileName
		answer = func(mode string) {
			m.extractAsks = m.extractAsks[1:]
			ask.answer <- mode
		}
	default:
		return
	}
	t := kit.T()
	option := func(label, example string) *fairy.El {
		return fairy.Col(fairy.Text(label).Medium(), fairy.Text(example).FontSize(13)).Gap(4).Pad(12).Radius(6).
			Fill(t.Muted.Alpha(0.5))
	}
	const prefix = "page.mod.dialog.extract_archive_path."
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T(prefix + "title"),
		Alert:       true,
		Description: T(prefix+"description", "fileName", fileName),
		Body: fairy.Col(
			option(T(prefix+"flatten_single_root"), T(prefix+"flatten_single_root_example")),
			option(T(prefix+"keep_archive_root"), T(prefix+"keep_archive_root_example")),
		).Gap(8),
		Width: 520,
		Actions: []*fairy.El{
			kit.Button(T(prefix+"cancel"), func() { answer("") }, kit.Outline),
			kit.Button(T(prefix+"flatten_single_root"), func() { answer("flatten_single_root") }),
			kit.Button(T(prefix+"keep_archive_root"), func() { answer("keep_archive_root") }),
		},
		OnClose: func() { answer("") },
		Key:     "mod.extract",
	})
}

func (s *Shell) showModDialogs(cx *fairy.Cx) {
	s.folderDraftDialog(cx)
	s.renameDialog(cx)
	s.modKeyDialog(cx)
	s.pasteDialog(cx)
	s.presetDialog(cx)
	s.presetDraftDialog(cx)
	s.classificationDialog(cx)
	s.extractDialog(cx)
	s.gameDialog(cx)
	s.nteLaunchDialog(cx)
	s.urlDownloadDialog(cx)
	s.mergeDialog(cx)
	s.conflictDialog(cx)
	s.textureResizeDialog(cx)
	s.outlineDialog(cx)
	s.wuwaDialogs(cx)
	s.zzmiDialogs(cx)
	s.fixLogDialog(cx)
	s.launchGuardDialogs(cx)
	s.modelViewerDialog(cx)
	s.bodyShapeDialog(cx)
	s.touchProfileDialog(cx)
	s.liquidEmitterDialog(cx)
	s.modLightboxDialog(cx)
}
