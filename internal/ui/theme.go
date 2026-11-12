package ui

import (
	"embed"
	"errors"
	"io/fs"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/platform"
)

//go:embed fonts/*.ttf
var fontFiles embed.FS

const fontFamily = "IBM Plex Sans KR"

// RegisterFonts registers the embedded UI font and returns the family to use: the system font when the
// embedded one fails to load, together with the error.
func RegisterFonts() (string, error) {
	paths, err := fs.Glob(fontFiles, "fonts/*.ttf")
	if err != nil {
		return "", err
	}
	var errs []error
	for _, path := range paths {
		data, err := fontFiles.ReadFile(path)
		if err == nil {
			_, err = fairy.RegisterFont(data)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return "", err
	}
	fairy.SetFontFallback(fallbackFamilies(platform.SystemLocale())...)
	return fontFamily, nil
}

// fallbackFamilies are the fonts for characters IBM Plex Sans KR lacks, such as kana and most hanzi. The React
// frontend listed sans-serif after it, which WebView2 resolves to Noto Sans KR on a Korean system, or Malgun
// Gothic where Noto Sans KR is not installed; DirectWrite's own fallback picks Malgun Gothic and Microsoft
// JhengHei instead. Other locales keep the system fallback, which follows the locale as the browser does.
func fallbackFamilies(locale string) []string {
	if language, _, _ := strings.Cut(locale, "-"); strings.EqualFold(language, "ko") {
		return []string{"Noto Sans KR", "Malgun Gothic"}
	}
	return nil
}

// Theme returns the kit theme, whose tokens are the React frontend's shadcn/ui tokens. Background is the
// window frame behind the title bar and navigation, Card the page area. With blur, popups are bg-popover/50
// over backdrop-blur-lg, as before the Mica backdrop; dialog scrims stay unblurred so only the dialog blurs.
// The light theme uses bg-popover/75 over a bg-black/20 scrim instead: a half-white dialog over the default
// scrim turns mid-grey, where muted text loses contrast and input borders show lighter than the panel.
func Theme(dark, blur bool, font string) kit.Theme {
	t := kit.Dark()
	if !dark {
		t = kit.Light()
		// Translucent black matches #e5e5e5 on white but stays darker than a tinted or blurred surface, where the
		// opaque token reads as a white outline; the dark theme's borders are translucent white for the same reason.
		t.Border, t.Input = fairy.RGBA(0, 0, 0, 0.1), fairy.RGBA(0, 0, 0, 0.1)
	}
	t.Font = font
	t.DialogTopInset = titleBarHeight
	if blur {
		t.SurfaceBlur, t.SurfaceAlpha = 16, 0.5
		if !dark {
			t.SurfaceAlpha = 0.75
			t.Scrim = fairy.RGBA(0, 0, 0, 0.2)
		}
	}
	return t
}

// frosted adds a backdrop blur of px to e while the blur setting is on.
func frosted(e *fairy.El, px float32) *fairy.El {
	if kit.T().SurfaceBlur > 0 {
		e.BackdropBlur(px)
	}
	return e
}

// glass fills e with on over a backdrop blur of px while the blur setting is on, and with off otherwise.
func glass(e *fairy.El, on, off fairy.Color, px float32) *fairy.El {
	if kit.T().SurfaceBlur > 0 {
		return e.Fill(on).BackdropBlur(px)
	}
	return e.Fill(off)
}

func stableScrollY(view *fairy.El) *fairy.El {
	ink := kit.T().MutedForeground
	return view.ScrollY().PadEdges(0, 10, 0, 0).ScrollBarStyle(fairy.ScrollBarStyle{
		Thickness: 6, Inset: 2, AlwaysVisible: true,
		Color: ink.Alpha(0.35), DragColor: ink.Alpha(0.7),
	})
}

// The colours below have no shadcn/ui token; they keep the values the screens were ported with.

func textDisabled(t *kit.Theme) fairy.Color {
	if t.Dark {
		return fairy.Hex(0x737373)
	}
	return fairy.Hex(0xa1a1a1)
}

// hoverFill is laid over a row or button while the pointer is on it, so it shows on any fill.
func hoverFill(t *kit.Theme) fairy.Color {
	if t.Dark {
		return fairy.RGBA(255, 255, 255, 0.06)
	}
	return fairy.RGBA(0, 0, 0, 0.05)
}

// opacity applies a CSS opacity modifier such as bg-input/30, which scales the colour's own alpha; the dark
// Input and Border tokens are already translucent.
func opacity(c fairy.Color, a float32) fairy.Color {
	return c.Alpha(c.A * a)
}

// inputFill is the fill of a shadcn/ui Input: dark:bg-input/30 over a transparent light field.
func inputFill(t *kit.Theme) fairy.Color {
	if t.Dark {
		return opacity(t.Input, 0.3)
	}
	return fairy.Transparent
}

func accentHover(t *kit.Theme) fairy.Color {
	if t.Dark {
		return t.Accent.Mix(fairy.Hex(0xffffff), 0.12)
	}
	return t.Accent.Mix(fairy.Hex(0x000000), 0.12)
}

func accentSoft(t *kit.Theme) fairy.Color {
	if t.Dark {
		return t.Accent.Alpha(0.2)
	}
	return t.Accent.Alpha(0.12)
}

func shadowColor(t *kit.Theme) fairy.Color {
	if t.Dark {
		return fairy.RGBA(0, 0, 0, 0.4)
	}
	return fairy.RGBA(0, 0, 0, 0.12)
}
