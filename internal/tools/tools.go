package tools

import (
	"context"
	"errors"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	bodyshape "nahida.live/desktop/internal/tools/body_shape"
	fixinspection "nahida.live/desktop/internal/tools/fix_inspection"
	fixtool "nahida.live/desktop/internal/tools/fix_tool"
	fixer4001 "nahida.live/desktop/internal/tools/fixer_4001"
	"nahida.live/desktop/internal/tools/menumaker"
	modbisect "nahida.live/desktop/internal/tools/mod_bisect"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
	"nahida.live/desktop/internal/tools/texture"
	togglepersist "nahida.live/desktop/internal/tools/toggle_persist"
	touchprofile "nahida.live/desktop/internal/tools/touch_profile"
	wuwafixer "nahida.live/desktop/internal/tools/wuwa_fixer"
	zzmifixer "nahida.live/desktop/internal/tools/zzmi_fixer"
	"nahida.live/desktop/internal/xxmi"
)

type BisectSettings interface {
	GetBisectPreserveD3dx(context.Context) (bool, error)
	GetDisabledPrefixStyle(context.Context) (string, error)
}

type ModDisabler interface {
	Disable(context.Context, string) (string, error)
	Enable(context.Context, string) (string, error)
}

type PEDiversifier interface {
	Diversify(context.Context, string, string) (PEDiversificationReport, error)
}

type Options struct {
	Log                    *infra.Log
	EventEmit              func(string, ...any)
	Notify                 func(title, body string) error
	Settings               BisectSettings
	XXMI                   *xxmi.XXMI
	FS                     *platform.FS
	HTTP                   *infra.Client
	Download               *infra.Download
	Archive                *infra.Archive
	Protocol               *infra.Protocol
	GitHubRate             *infra.GitHubRateCoordinator
	Mod                    ModDisabler
	FindModelViewerPreview func(string) *string
	// PEDiversifier diversifies packed executable content.
	PEDiversifier PEDiversifier
}

// Tools is the Wails service the renderer calls. Feature behavior lives in the
// subpackages; this type keeps the binding contract and wires them together.
type Tools struct {
	githubRate *infra.GitHubRateCoordinator

	bisect        *modbisect.Service
	bodyShape     *bodyshape.Service
	fixInspection *fixinspection.Service
	fixTool       *fixtool.Service
	fixer4001     *fixer4001.Service
	menuMaker     *menumaker.MenuMaker
	modelViewer   *modelviewer.Service
	texture       *texture.Service
	persist       *togglepersist.Service
	touchProfile  *touchprofile.Service
	wuwa          *wuwafixer.Service
	zzmi          *zzmifixer.Service
}

func New() *Tools { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Tools {
	if opts.FS == nil {
		opts.FS = platform.NewFS()
	}
	if opts.Protocol == nil {
		opts.Protocol = infra.NewProtocol()
	}
	fixInspection := fixinspection.NewWithOptions(fixinspection.Options{
		Log:       opts.Log,
		EventEmit: opts.EventEmit,
		Settings:  opts.Settings,
	})
	fixTool := fixtool.NewWithOptions(fixtool.Options{
		Log:       opts.Log,
		EventEmit: opts.EventEmit,
	})
	zzmi := zzmifixer.NewWithOptions(zzmifixer.Options{
		Log:        opts.Log,
		EventEmit:  opts.EventEmit,
		HTTP:       opts.HTTP,
		GitHubRate: opts.GitHubRate,
		Runner:     fixTool,
	})
	fixInspection.Register(zzmifixer.NewInspector(zzmi))
	return &Tools{
		githubRate: opts.GitHubRate,
		bisect: modbisect.NewWithOptions(modbisect.Options{
			Log:       opts.Log,
			EventEmit: opts.EventEmit,
			Settings:  opts.Settings,
			XXMI:      opts.XXMI,
		}),
		bodyShape: bodyshape.NewWithOptions(bodyshape.Options{
			Log:      opts.Log,
			FS:       opts.FS,
			Protocol: opts.Protocol,
			Mod:      opts.Mod,
		}),
		fixInspection: fixInspection,
		fixTool:       fixTool,
		fixer4001: fixer4001.NewWithOptions(fixer4001.Options{
			Log:           opts.Log,
			EventEmit:     opts.EventEmit,
			FS:            opts.FS,
			HTTP:          opts.HTTP,
			Download:      opts.Download,
			Archive:       opts.Archive,
			XXMI:          opts.XXMI,
			PEDiversifier: opts.PEDiversifier,
		}),
		menuMaker: menumaker.NewWithOptions(menumaker.Options{Log: opts.Log}),
		modelViewer: modelviewer.NewWithOptions(modelviewer.Options{
			Log:                    opts.Log,
			Protocol:               opts.Protocol,
			FindModelViewerPreview: opts.FindModelViewerPreview,
		}),
		texture: texture.NewWithOptions(texture.Options{
			Log:       opts.Log,
			EventEmit: opts.EventEmit,
			Download:  opts.Download,
			Archive:   opts.Archive,
		}),
		persist: togglepersist.NewWithOptions(togglepersist.Options{
			Log:       opts.Log,
			EventEmit: opts.EventEmit,
			Settings:  opts.Settings,
			XXMI:      opts.XXMI,
		}),
		touchProfile: touchprofile.NewWithOptions(touchprofile.Options{
			Log:       opts.Log,
			EventEmit: opts.EventEmit,
			FS:        opts.FS,
			Protocol:  opts.Protocol,
			Mod:       opts.Mod,
			XXMI:      opts.XXMI,
		}),
		wuwa: wuwafixer.NewWithOptions(wuwafixer.Options{
			Log:        opts.Log,
			EventEmit:  opts.EventEmit,
			Notify:     opts.Notify,
			Settings:   opts.Settings,
			HTTP:       opts.HTTP,
			GitHubRate: opts.GitHubRate,
			Runner:     fixTool,
		}),
		zzmi: zzmi,
	}
}

//wails:ignore
func (t *Tools) UseClient(client *db.Client) {
	if t == nil {
		return
	}
	if t.githubRate != nil && client != nil {
		t.githubRate.UseAppState(client.AppState)
	}
	t.bisect.UseClient(client)
	t.fixInspection.UseClient(client)
	t.fixTool.UseClient(client)
	t.fixer4001.UseClient(client)
	t.texture.UseClient(client)
	t.wuwa.UseClient(client)
	t.zzmi.UseClient(client)
}

//wails:ignore
func (t *Tools) UseAppData(data *appdata.Store) {
	if t == nil {
		return
	}
	t.bisect.UseAppData(data)
	t.texture.UseAppData(data)
	t.touchProfile.UseAppData(data)
	t.wuwa.UseAppData(data)
	t.zzmi.UseAppData(data)
}

//wails:ignore
func (t *Tools) ServiceShutdown() error {
	if t == nil {
		return nil
	}
	return errors.Join(
		t.shutdownFixTool(),
		t.shutdownFixInspections(),
		t.shutdownBisect(),
		t.stopWuwaAutoUpdateCheck(),
		t.shutdownTouchProfiles(),
		t.shutdownBodyShape(),
		t.shutdownModelViewer(),
		t.shutdownPersistWatcher(),
	)
}
