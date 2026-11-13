package ui

import (
	"fmt"
	"math"
	"slices"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	liquidemitter "nahida.live/desktop/internal/tools/liquid_emitter"
)

// Slots.hlsl slotDrive, slotAmount and slotViscosity and the backend accept forces from 0.15 to 2.35, amounts from
// 0.02 to 2 and viscosities from 0.1 to 10. The amount and viscosity sliders move on the base-10 logarithm, so
// the thin flows below 1 get room and a viscosity of 1 sits in the middle.
const (
	liquidMinForce          = 0.15
	liquidMaxForce          = 2.35
	liquidMaxViscosityLog   = 1
	liquidDefaultAbsorption = 15
)

var (
	liquidMinAmountLog = math.Log10(0.02)
	liquidMaxAmountLog = math.Log10(2)
)

func liquidNozzleOf(look liquidemitter.Look) string {
	if slices.Contains(liquidNozzles, look.Nozzle) {
		return look.Nozzle
	}
	return liquidemitter.NozzleSpray
}

// liquidMaterialOf is the preset the look's scattering and roughness match, or "".
func liquidMaterialOf(look liquidemitter.Look) string {
	for _, name := range liquidMaterialNames {
		if preset := liquidMaterials[name]; preset == [2]float64{look.Scattering, look.Roughness} {
			return name
		}
	}
	return ""
}

func liquidFollowsRuntime(look liquidemitter.Look) bool {
	return look.Force == 1 && look.Amount == 1 && !look.CustomColor && look.Viscosity == 1 &&
		liquidNozzleOf(look) == liquidemitter.NozzleSpray && liquidMaterialOf(look) == "clear" && !look.Emission.Enabled
}

func liquidRound(value float64, digits int) float64 {
	scale := math.Pow(10, float64(digits))
	return math.Round(value*scale) / scale
}

func (lt *liquidTool) lookSection() *fairy.El {
	saved := lt.savedLook()
	if saved == nil {
		return nil
	}
	look := *saved
	if draft := lt.draftLook(); draft != nil {
		look = *draft
	}
	locked := !lt.editable() || lt.busy != ""
	runtimeLook := liquidFollowsRuntime(look)
	summary := "look.summary.custom"
	switch {
	case lt.lookDirty():
		summary = "look.summary.unsaved"
	case runtimeLook:
		summary = "look.summary.runtime"
	}
	update := func(change func(l *liquidemitter.Look)) {
		next := look
		change(&next)
		lt.setDraftLook(next)
	}
	nozzle := liquidNozzleOf(look)
	material := liquidMaterialOf(look)
	hint := func(name string) *fairy.El {
		return liquidHint(T(liquidPrefix + "look.hints." + nozzle + "." + name))
	}

	return lt.section("look", T(liquidPrefix+"look.title"), T(liquidPrefix+summary), nil, func() *fairy.El {
		t := kit.T()
		body := fairy.Col(
			liquidHint(T(liquidPrefix+"look.hint")),
			fairy.Col(
				fairy.Text(T(liquidPrefix+"look.nozzle.title")).FontSize(14).LineHeight(20).Medium(),
				liquidChoice(liquidNozzles, nozzle, func(value string) string {
					return T(liquidPrefix + "look.nozzle." + value)
				}, locked, func(value string) { update(func(l *liquidemitter.Look) { l.Nozzle = value }) }),
				liquidHint(T(liquidPrefix+"look.nozzle."+nozzle+"_hint")),
			).Gap(6),
			func() *fairy.El {
				if nozzle == liquidemitter.NozzleYogurt {
					return nil
				}
				return fairy.Col(liquidRange(T(liquidPrefix+"look.force"), fmt.Sprintf("×%.2f", look.Force), look.Force,
					liquidMinForce, liquidMaxForce, 0.05, locked, func(value float64) {
						update(func(l *liquidemitter.Look) { l.Force = liquidRound(value, 2) })
					}), hint("force")).Gap(4)
			}(),
			fairy.Col(liquidRange(T(liquidPrefix+"look.amount"), fmt.Sprintf("×%.2f", look.Amount),
				math.Log10(look.Amount), liquidMinAmountLog, liquidMaxAmountLog, 0.01, locked, func(value float64) {
					update(func(l *liquidemitter.Look) { l.Amount = liquidRound(math.Pow(10, value), 2) })
				}), hint("amount")).Gap(4),
			fairy.Col(liquidRange(T(liquidPrefix+"look.viscosity"), fmt.Sprintf("×%.2f", look.Viscosity),
				math.Log10(look.Viscosity), -liquidMaxViscosityLog, liquidMaxViscosityLog, 0.05, locked,
				func(value float64) {
					update(func(l *liquidemitter.Look) { l.Viscosity = liquidRound(math.Pow(10, value), 2) })
				}), hint("viscosity")).Gap(4),
			liquidCheck(look.CustomColor, locked, T(liquidPrefix+"look.custom_color"),
				T(liquidPrefix+"look.custom_color_hint"), func(on bool) {
					update(func(l *liquidemitter.Look) { l.CustomColor = on })
				}),
		).Gap(12)

		body.Child(lt.emissionSection(look, locked, update))
		if current := lt.current(); current != nil && !current.SurfaceTracked {
			body.Child(liquidHint(T(liquidPrefix + "look.surface_required")))
		}
		if look.CustomColor {
			tint := fairy.Color{
				R: float32(clamp01(look.Tint[0])), G: float32(clamp01(look.Tint[1])), B: float32(clamp01(look.Tint[2])),
				A: 1,
			}
			picker := kit.ColorPicker("liquid-emitter.tint", tint, func(c fairy.Color, _ bool) {
				update(func(l *liquidemitter.Look) {
					// Tint channels run from 0 to 1, as the runtime's own colour setting stores them.
					l.Tint = [3]float64{
						liquidRound(math.Round(float64(c.R)*255)/255, 3),
						liquidRound(math.Round(float64(c.G)*255)/255, 3),
						liquidRound(math.Round(float64(c.B)*255)/255, 3),
					}
				})
			}, kit.Label(T(liquidPrefix+"look.tint")), kit.Disabled(locked), kit.ColorSwatchField)
			body.Child(
				fairy.Row(fairy.Text(T(liquidPrefix+"look.tint")).FontSize(14).LineHeight(20).Grow(1).MinW(0), picker.W(64).Shrink(0)).
					Gap(8).
					Align(fairy.Center),
				func() *fairy.El {
					if nozzle == liquidemitter.NozzleYogurt {
						return nil
					}
					return fairy.Col(liquidRange(T(liquidPrefix+"look.absorption"), fmt.Sprintf("%g mm", look.AbsorptionMM),
						look.AbsorptionMM, 2, 60, 1, locked, func(value float64) {
							update(func(l *liquidemitter.Look) { l.AbsorptionMM = value })
						}), liquidHint(T(liquidPrefix+"look.absorption_hint"))).
						Gap(4)
				}(),
			)
		}

		if nozzle == liquidemitter.NozzleYogurt {
			return body.Child(
				kit.Button(T(liquidPrefix+"look.reset"), func() { update(resetLiquidLook) }, kit.Outline, kit.Small, kit.Disabled(locked)).
					WFull(),
			)
		}
		materialTitle := fairy.Row(fairy.Text(T(liquidPrefix + "look.material.title")).FontSize(14).LineHeight(20).Medium().Grow(1)).
			Align(fairy.Center)
		if material == "" {
			materialTitle.Child(fairy.Text(T(liquidPrefix + "look.material.preset.custom")).FontSize(12).LineHeight(16).
				Color(t.MutedForeground))
		}
		percent := func(value float64) string { return fmt.Sprintf("%.0f%%", math.Round(value*100)) }
		return body.Child(
			fairy.Col(
				materialTitle,
				liquidChoice(liquidMaterialNames, material, func(value string) string {
					return T(liquidPrefix + "look.material.preset." + value)
				}, locked, func(value string) {
					preset := liquidMaterials[value]
					update(func(l *liquidemitter.Look) { l.Scattering, l.Roughness = preset[0], preset[1] })
				}),
				liquidHint(T(liquidPrefix+"look.material.hint")),
			).Gap(6),
			fairy.Col(liquidRange(T(liquidPrefix+"look.material.scattering"), percent(look.Scattering),
				math.Round(look.Scattering*100), 0, 100, 1, locked, func(value float64) {
					update(func(l *liquidemitter.Look) { l.Scattering = liquidRound(value/100, 2) })
				}), liquidHint(T(liquidPrefix+"look.material.scattering_hint"))).Gap(4),
			fairy.Col(liquidRange(T(liquidPrefix+"look.material.roughness"), percent(look.Roughness),
				math.Round(look.Roughness*100), 0, 100, 1, locked, func(value float64) {
					update(func(l *liquidemitter.Look) { l.Roughness = liquidRound(value/100, 2) })
				}), liquidHint(T(liquidPrefix+"look.material.roughness_hint"))).Gap(4),
			kit.Button(T(liquidPrefix+"look.reset"), func() {
				update(resetLiquidLook)
			}, kit.Outline, kit.Small, kit.Disabled(locked || runtimeLook)).WFull(),
		)
	})
}

func resetLiquidLook(look *liquidemitter.Look) {
	tint, absorption := look.Tint, look.AbsorptionMM
	*look = liquidRuntimeLook
	look.Tint, look.AbsorptionMM = tint, absorption
}

func clamp01(value float64) float64 {
	return math.Min(math.Max(value, 0), 1)
}
