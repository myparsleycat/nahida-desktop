package ui

import (
	"fmt"

	"github.com/myparsleycat/fairy"

	liquidemitter "nahida.live/desktop/internal/tools/liquid_emitter"
)

func (lt *liquidTool) emissionSection(
	look liquidemitter.Look,
	locked bool,
	update func(func(*liquidemitter.Look)),
) *fairy.El {
	prefix := liquidPrefix + "look.emission."
	return lt.section("emission", T(prefix+"title"), "", nil, func() *fairy.El {
		body := fairy.Col(liquidCheck(look.Emission.Enabled, locked, T(prefix+"enabled"), T(prefix+"hint"), func(on bool) {
			update(func(l *liquidemitter.Look) {
				if on {
					l.Emission = liquidemitter.DefaultEmissionSettings()
				} else {
					l.Emission.Enabled = false
				}
			})
		})).
			Gap(12)
		if !look.Emission.Enabled {
			return body
		}
		e := look.Emission
		slider := func(key, display string, value, low, high, step float64, set func(*liquidemitter.EmissionSettings, float64)) *fairy.El {
			return liquidRange(
				T(prefix+key),
				display,
				value,
				low,
				high,
				step,
				locked,
				func(v float64) { update(func(l *liquidemitter.Look) { set(&l.Emission, liquidRound(v, 2)) }) },
			)
		}
		body.Child(
			slider(
				"interval",
				fmt.Sprintf("×%.2f", e.Interval),
				e.Interval,
				.5,
				5,
				.05,
				func(e *liquidemitter.EmissionSettings, v float64) { e.Interval = v },
			),
			slider(
				"width",
				fmt.Sprintf("×%.2f", e.Width),
				e.Width,
				.1,
				2,
				.05,
				func(e *liquidemitter.EmissionSettings, v float64) { e.Width = v },
			),
			slider(
				"transparency",
				fmt.Sprintf("%.0f%%", e.Transparency*100),
				e.Transparency,
				0,
				1,
				.01,
				func(e *liquidemitter.EmissionSettings, v float64) { e.Transparency = v },
			),
		)
		if look.Nozzle == liquidemitter.NozzleSpray {
			body.Child(
				slider(
					"angle",
					fmt.Sprintf("%.0f°", e.SprayAngle),
					e.SprayAngle,
					0,
					200,
					1,
					func(e *liquidemitter.EmissionSettings, v float64) { e.SprayAngle = v },
				),
				slider(
					"ratio",
					fmt.Sprintf("%.0f%%", e.SprayRatio*100),
					e.SprayRatio,
					0,
					1,
					.01,
					func(e *liquidemitter.EmissionSettings, v float64) { e.SprayRatio = v },
				),
				liquidCheck(
					e.RandomSpeed,
					locked,
					T(prefix+"random_speed"),
					"",
					func(on bool) { update(func(l *liquidemitter.Look) { l.Emission.RandomSpeed = on }) },
				),
			)
		}
		return body
	})
}
