package ui

import (
	"context"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
)

// The development screen is shown only in unpackaged builds, so its text is not translated.

type developmentScreen struct {
	trace       string
	traceErr    error
	traceBusy   bool
	traceLoaded bool
	// throwing makes the next build of the screen panic, to exercise the error screen.
	throwing bool
}

func (s *Shell) developmentPage() *fairy.El {
	t := kit.T()
	d := &s.development
	if d.throwing {
		d.throwing = false
		panic("Intentional runtime error")
	}
	if !d.traceLoaded && !d.traceBusy {
		s.loadCDNTrace()
	}

	var trace *fairy.El
	switch {
	case d.traceErr != nil:
		trace = fairy.Text(d.traceErr.Error()).FontSize(12).LineHeight(16).Color(t.Destructive).Wrap()
	case !d.traceLoaded:
		trace = fairy.Row(fairy.Spinner().Size(14, 14), fairy.Text("Fetching trace").LineHeight(16)).
			Gap(8).
			Align(fairy.Center).
			FontSize(12).
			Color(t.MutedForeground)
	default:
		// A pre block draws no empty line for the response's final newline.
		trace = fairy.Text(strings.TrimSuffix(d.trace, "\n")).Font(monoFont).FontSize(12).LineHeight(16).Wrap().WFull()
	}
	runtimeError := settingsGroup("Runtime error",
		fairy.Row(
			fairy.Text("Throw an intentional render error to exercise the app error boundary.").
				FontSize(12).LineHeight(16).Color(t.MutedForeground).Grow(1).MinW(0).Wrap(),
			kit.Button("Throw error", func() { d.throwing = true }, kit.Destructive).Shrink(0),
		).Gap(24).Align(fairy.Center).PadXY(0, 12),
	).WFull()
	card := settingsGroup("cdn-cgi/trace",
		fairy.Row(
			fairy.Text("Cloudflare trace for the app HTTP client against the Nahida API origin.").
				FontSize(12).LineHeight(16).Color(t.MutedForeground).Grow(1).MinW(0).Wrap(),
			kit.Button("Refresh", s.loadCDNTrace, kit.Outline, kit.Small, kit.Loading(d.traceBusy),
				kit.Disabled(d.traceBusy)).Shrink(0),
		).Gap(24).Align(fairy.Center).PadXY(0, 12),
		trace.PadXY(0, 12),
	).WFull()

	return fairy.Col(
		fairy.Row(fairy.Text("Development").Font(monoFont).FontSize(12).LineHeight(16).Medium()).
			H(40).
			Shrink(0).
			WFull().
			PadXY(16, 0).
			Align(fairy.Center).
			BorderEdges(0, 0, 1, 0, t.Border),
		fairy.Col(fairy.Col(runtimeError, card).WFull().MaxW(576).Pad(16).Gap(24)).
			Key("development.scroll").
			Grow(1).
			MinH(0).
			WFull().
			ScrollY().
			Align(fairy.Center),
	).Grow(1).WFull().MinH(0)
}

func (s *Shell) loadCDNTrace() {
	d := &s.development
	if d.traceBusy || s.deps.CDNTrace == nil {
		return
	}
	d.traceBusy = true
	go func() {
		trace, err := s.deps.CDNTrace.Get(context.Background())
		if err != nil {
			err = infra.ReportError(
				s.deps.Log,
				err,
				"CDNTrace.Get",
				infra.Diagnostic{Operation: "development.cdn-trace"},
			)
		}
		fairy.Go(func() {
			d.trace, d.traceErr, d.traceBusy, d.traceLoaded = trace, err, false, true
		})
	}()
}
