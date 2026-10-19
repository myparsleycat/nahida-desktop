#define PHASE_STATE
#include "FluidSolver.hlsl"

// Shared-source build: collision-v2-native-depth-stance-fix

// Shared-source build: collision-v3-contact-randomized-splash

// Shared-source build: collision-v4-spatial-exclusion-no-proxy

// Shared-source build: collision-v5-recover-verified-contact-with-exclusion

// Shared build: pulse-v1

// Shared build: upward-emitter-spray-and-pulse-cone-v3

// Shared build: fixed-pitch-stronger-wide-pulse-v4

// Shared build: source-outward-4mm-v6

// Shared build: restore-forward-offset-lower-source-4mm-v7

// Shared build: world-emission-irregular-dense-taper-v8

// Shared build: emission-drain-impact-visibility-v9

// Shared build: drain-preserve-live-particles-v10

// Shared build: source-only-restart-no-historical-birth-v11

// Shared build: pulse-angle-random-hold-v13

// Shared build: rearward-angle-plus-5deg-v14

// Shared build: seven-level-launch-strength-v15

// Shared build: compliant-downward-slit-outlet-v16

// Shared build: fast-irregular-nozzle-flutter-v17

// Shared build: symmetric-sheet-underdamped-recoil-v18

// Shared build: visible-sheet-emergence-v19

// Shared build: 35mm-focus-then-two-axis-respray-v20

// Shared build: strength-dependent-focus-max-100mm-v21

// Shared build: speed-dependent-near-field-gravity-v22

// Shared build: postfocus-ballistic-momentum-v23

// Shared build: widening-postfocus-volume-recovery-v24

// Shared build: single-focus-decoupled-spray-fast-rounding-v25

// Shared build: preserve-incoming-focus-ray-v26

// Shared build: immutable-geometric-crossing-rays-v27

// Shared build: advected-parametric-leaf-tangent-rays-v28

// Shared build: smooth-leaf-envelope-common-origin-v29

// Shared build: unclipped-surface-double-density-small-radius-v30

// Shared build: independent-reserve-main-samples-extended-necks-v31

// strength range v33: 0.55 .. 2.8, reference 1.3

// strength v34: eight levels .28 .. 3.94, default 1.69

// v35 low-pressure breakup and independent peripheral droplets

// v36 intermittent minimum-pressure branch streams

// v37 whole-core minimum-flow redirection and dispersed intervals

// v39 minimum-flow direction transitions: 220 ms

// v40 visual size independent of mass, coherence-dependent impact splash

// v43 frequent ballistic full-dispersion intervals, independent of pulse sheet

// v44 independent ballistic scatter with explicit transverse velocity

// v45 halve full-dispersion polar angle, preserve launch speed

// v46 full-dispersion frequency halved: 4.8 second cycle

// v47 randomized full dispersion duration: 0.20 .. 1.40 seconds

// v48 pulses rapidly narrow minimum-flow rays toward the basic down axis

// v50 stop source emission at the exact final pulse end

// v52 staged diminishing shutdown and half-size terminal sputters

// v56 half the minimum-strength full-dispersion birth events

// v57 full-dispersion launch speed 70 percent

// v64 terminal shutdown sputters retain 20 percent of birth events

// v65 terminal sputter radii: 80%, optional 65%, 50%

// v67 low-2 and low-3 use low-1 pressure reference while pulsing

// v69 randomized shutdown counts: 1..3 pulses and 0..3 terminal sputters

// v70 tail launch speed +50%, radius 80/75/70% of baseline independent of setting

// v71 tail size 100/85/70%, single sputter starts at 100%

// v77 emit only from registered live mesh surfaces; native profiles are separate
