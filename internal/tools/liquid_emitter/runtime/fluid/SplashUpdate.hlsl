#define SPLASH_UPDATE
#include "Splash.hlsl"

// Shared-source build: collision-v2-native-depth-stance-fix

// Shared-source build: collision-v3-contact-randomized-splash

// Shared-source build: collision-v4-spatial-exclusion-no-proxy

// Shared-source build: collision-v5-recover-verified-contact-with-exclusion

// Shared build: pulse-v1

// Shared build: larger-emitter-drops-v2

// Shared build: upward-emitter-spray-and-pulse-cone-v3

// Shared build: fixed-pitch-stronger-wide-pulse-v4

// Shared build: emitter-socket-tightening-v5

// Shared build: source-outward-4mm-v6

// Shared build: restore-forward-offset-lower-source-4mm-v7

// Shared build: impact-drops-two-at-80percent-v8

// Shared build: full-size-impact-drops-v9

// Shared build: emission-drain-impact-visibility-v9

// Shared build: full-size-highlighted-pulse-drops-v12

// Shared build: seven-level-launch-strength-v15

// Shared build: compliant-downward-slit-outlet-v16

// Shared build: fast-irregular-nozzle-flutter-v17

// Shared build: symmetric-sheet-underdamped-recoil-v18

// Shared build: speed-dependent-near-field-gravity-v22

// Shared build: unclipped-surface-double-density-small-radius-v30

// Shared build: independent-reserve-main-samples-extended-necks-v31

// strength range v33: 0.55 .. 2.8, reference 1.3

// strength v34: eight levels .28 .. 3.94, default 1.69

// v35 low-pressure breakup and independent peripheral droplets

// v40 visual size independent of mass, coherence-dependent impact splash

// v42 strength-dependent floor splash attenuation

// v49 pulse peripheral cone angle 70 percent

// v50 stop source emission at the exact final pulse end

// v52 staged diminishing shutdown and half-size terminal sputters

// v58 floor impact splash velocity 70 percent

// v59 minimum floor splash response is 20 percent of maximum

// v60 low-pressure peripheral cone widens toward the existing maximum

// v61 halve the minimum peripheral cone angle

// v62 baseline peripheral spray: half low-1 angle and expected count

// v63 pulse peripheral angle 70 percent of previous

// v66 on-pulse peripheral angle/rate blend continuously toward baseline

// v67 low-2 and low-3 use low-1 pressure reference while pulsing

// v68 pulse peripheral azimuth centered around the main down axis

// v73 high-strength peripheral spray widens and increases toward max

// v74 high-strength maximum peripheral angle and rate halved

// v75 minimum-strength peripheral cone angle and rate halved

// v76 low-level peripheral angle/rate interpolate from baseline to minimum

// v77 emit only from registered live mesh surfaces; native profiles are separate
