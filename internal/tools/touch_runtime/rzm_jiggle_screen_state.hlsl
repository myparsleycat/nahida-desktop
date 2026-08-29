// Single-source interaction runtime. Dispatched once after all component
// detectors are pinned; all jiggle passes consume this state next frame.

RWBuffer<float4> InteractionState : register(u0);
// Path Slide progress per zone (0..1 along that zone's authored path
// vector). A plain persistent buffer, not ini `persist` -- it naturally
// survives frame-to-frame like InteractionState itself does, but resets
// fresh each game/3DMigoto session rather than being saved to disk.
RWBuffer<float> PathProgressState : register(u1);
Buffer<float4> PinnedDetectInfo   : register(t67);
Texture1D<float4> IniParams       : register(t120);

#define CAPTURED_CURSOR    IniParams[67]
#define JIGGLE_PARAMS      IniParams[68]
#define CURRENT_CURSOR     IniParams[69]
#define PHYS_PARAMS        IniParams[70]
#define POLISH_PARAMS      IniParams[71]
#define JIGGLE_MULTIPLIERS IniParams[72]
#define JIGGLE_MULT_EXTRA  IniParams[73]
// .z = grab depth-pull multiplier: fraction of the 2D screen drag distance
// added as an extra pull along the frozen surface normal, so a drag looks
// like it's coming toward the camera instead of only sliding laterally.
#define DEPTH_PULL_MULT    JIGGLE_MULT_EXTRA.z
#define TIME_PARAMS        IniParams[76]
#define ZONE_STRENGTH_LOW  IniParams[79]
#define ZONE_STRENGTH_HIGH IniParams[80]
#define ZONE_STRENGTH_R2   IniParams[106]
#define ZONE_OFFSET_LOW    IniParams[81]
#define ZONE_OFFSET_HIGH   IniParams[82]
#define ZONE_OFFSET_R2     IniParams[109]
#define POKE_PARAMS        IniParams[84]
// 1.0 = grabbable (default), 0.0 = poke/pull only -- denies the LMB+RMB
// grab/drag combo outright for that zone; poke is untouched by this.
#define ZONE_GRABBABLE_LOW  IniParams[99]
#define ZONE_GRABBABLE_HIGH IniParams[100]
#define ZONE_GRABBABLE_R2   IniParams[112]
// 1.0 = this zone is a rigid Path Slide (see PathProgressState above and
// PathVectors.buf), 0.0 = normal Jiggle.
#define ZONE_PATH_MODE_LOW  IniParams[119]
#define ZONE_PATH_MODE_HIGH IniParams[120]
#define ZONE_PATH_MODE_R2   IniParams[121]
// 0 = inherit the global damping multiplier (JIGGLE_MULT_EXTRA.x), else
// overrides it for both grab and release physics while this zone is active.
#define ZONE_DAMPING_LOW    IniParams[122]
#define ZONE_DAMPING_HIGH   IniParams[123]
#define ZONE_DAMPING_R2     IniParams[124]
#define ZONE_SPRING_LOW     IniParams[125]
#define ZONE_SPRING_HIGH    IniParams[126]
#define ZONE_SPRING_R2      IniParams[127]
// .x = release damping multiplier immediately after a big/hard release
//      (fallback 1.05, i.e. more flexible than steady-state — how much
//      more scales with how far it was stretched at the moment of release).
// .y = per-step retention of the boost's excess above steady-state while it
//      decays back down (fallback 0.92 — lower decays back to steady-state
//      faster, higher lingers longer).
#define RELEASE_BOOST      IniParams[97]
// POKE_PARAMS.x = poke sign this frame: -1.0 = pull (into mesh, along -normal),
//                 +1.0 = push (out of mesh, along +normal), 0.0 = no poke.
//                 Set for exactly one frame by the ini on a lone LMB/RMB
//                 click-release (not part of the LMB+RMB grab combo).
// POKE_PARAMS.y = poke strength multiplier, fallback 1.0.
// POKE_PARAMS.z = simulated hold duration in frames (how long the poke's
//                 synthetic "grab" target stays active before releasing),
//                 fallback 8.0.
// POKE_PARAMS.w = charge sign, continuous (not a one-frame pulse): -1.0
//                 while LMB alone is held, +1.0 while RMB alone is held,
//                 0.0 otherwise. Drives freezing the hovered normal/basis
//                 at the moment the hold STARTS (see capture.w below).

#define SLOT_ID     0u
#define SLOT_HIT    1u
#define SLOT_NORMAL 5u
#define SLOT_RIGHT  8u
#define SLOT_DOWN   9u

float SafePositive(float v, float fallback) { return v > 0.0 ? v : fallback; }
float SafeNonZero(float v, float fallback) { return abs(v) > 1e-8 ? v : fallback; }

// The old solver implicitly used one step per rendered frame.  Keep its
// 60 Hz profile but measure elapsed time so 30/144 FPS have the same motion.
float SimulationStep()
{
    float dt = SafePositive(TIME_PARAMS.x, 1.0 / 60.0);
    float speed = SafePositive(TIME_PARAMS.y, 1.0);
    float maxStep = SafePositive(TIME_PARAMS.z, 2.0);
    return clamp(dt * 60.0 * speed, 0.05, maxStep);
}

float ZoneStrengthOverride(uint zone)
{
    float4 packed = zone < 4u ? ZONE_STRENGTH_LOW : zone < 8u ? ZONE_STRENGTH_HIGH : ZONE_STRENGTH_R2;
    return packed[zone & 3u];
}

bool ZoneGrabbable(uint zone)
{
    float4 packed = zone < 4u ? ZONE_GRABBABLE_LOW : zone < 8u ? ZONE_GRABBABLE_HIGH : ZONE_GRABBABLE_R2;
    return packed[zone & 3u] > 0.5;
}

bool ZoneIsPathSlide(uint zone)
{
    float4 packed = zone < 4u ? ZONE_PATH_MODE_LOW : zone < 8u ? ZONE_PATH_MODE_HIGH : ZONE_PATH_MODE_R2;
    return packed[zone & 3u] > 0.5;
}

float ZoneDampingOverride(uint zone)
{
    float4 packed = zone < 4u ? ZONE_DAMPING_LOW : zone < 8u ? ZONE_DAMPING_HIGH : ZONE_DAMPING_R2;
    return packed[zone & 3u];
}

float ZoneSpringOverride(uint zone)
{
    float4 packed = zone < 4u ? ZONE_SPRING_LOW : zone < 8u ? ZONE_SPRING_HIGH : ZONE_SPRING_R2;
    return packed[zone & 3u];
}

float ZoneOffsetOverride(uint zone)
{
    float4 packed = zone < 4u ? ZONE_OFFSET_LOW : zone < 8u ? ZONE_OFFSET_HIGH : ZONE_OFFSET_R2;
    return packed[zone & 3u];
}

float3 ClampLength(float3 v, float maxLen)
{
    float lenSq = dot(v, v);
    return lenSq > maxLen * maxLen && lenSq > 1e-12 ? v * (maxLen * rsqrt(lenSq)) : v;
}

// Progressive resistance toward maxLen instead of a hard clip: near-linear
// for a light drag, then increasingly stiff the closer dragDir gets to
// maxLen, asymptotically approaching (never quite reaching) it for a large
// drag. Mimics a material's stretch limit instead of a wall the target
// snaps against the instant it's reached.
float3 PullTowardLimit(float3 dragDir, float maxLen)
{
    float len = length(dragDir);
    if (len < 1e-6 || maxLen <= 0.0)
        return float3(0.0, 0.0, 0.0);

    float pulledLen = maxLen * (1.0 - exp(-len / maxLen));
    return dragDir * (pulledLen / len);
}

[numthreads(1, 1, 1)]
void main(uint3 dispatchThreadID : SV_DispatchThreadID)
{
    float4 current = InteractionState[0u];
    float4 previous = InteractionState[1u];
    float4 center = InteractionState[2u];
    float4 capture = InteractionState[3u];
    float4 filtered = InteractionState[4u];
    float4 prevFiltered = InteractionState[5u];
    float4 right = InteractionState[6u];
    float4 down = InteractionState[7u];
    float4 inputFlags = InteractionState[8u];
    float4 zoneState = InteractionState[9u];
    float4 screenAnchor = InteractionState[10u];
    float4 gizmoXY = InteractionState[11u];
    float4 gizmoZ = InteractionState[12u];
    float4 captureComponent = InteractionState[13u];
    float4 captureDepth = InteractionState[14u];

    float4 detected = PinnedDetectInfo[SLOT_ID];
    float4 detectedPayload = PinnedDetectInfo[SLOT_HIT];
    bool hasHit = detected.x >= 0.0 && detected.y < 1e30 && abs(detectedPayload.w - 7.0) < 0.5;

    // Zone-detection debounce: PinnedDetectInfo[7].w resolves to a small
    // 0..7 zone id in practice (despite being labelled "nearest vertex
    // index" in rzm_pin_detected.hlsl's own header), and it's a raw,
    // unsmoothed single-frame sample. A continuous grab tolerates one noisy
    // frame fine — you're actively watching/adjusting it — but a one-shot
    // poke has much less of a safety net: idle/breathing animation can
    // flicker this to a neighbouring zone for exactly one frame, and if that
    // lands on Stomach (max_offset as low as 0.01) instead of the intended
    // zone, it looks identical to "always maxes out". zoneState.y/.z track
    // this across every frame — not just capture/poke frames — so a poke's
    // (and a fresh grab's, see newCapture below) zone commit already
    // benefits from a couple of frames of hover history leading up to the
    // click instead of trusting a single instant. Computed before
    // newCapture so a fresh grab can be denied on a non-grabbable zone.
    float rawZoneThisFrame = clamp(round(PinnedDetectInfo[7u].w), 0.0, 11.0);
    if (hasHit)
    {
        if (abs(rawZoneThisFrame - zoneState.y) < 0.5)
            zoneState.z = min(zoneState.z + 1.0, 2.0);
        else
            zoneState.z = 0.0;
        zoneState.y = rawZoneThisFrame;
    }
    float debouncedZone = (hasHit && zoneState.z >= 1.0) ? rawZoneThisFrame : zoneState.x;

    bool held = CAPTURED_CURSOR.w > 0.5;
    bool pressed = held && inputFlags.x < 0.5;
    bool wasCapture = capture.w > 0.5;
    // Non-grabbable zones (e.g. a head-pat spot) deny the grab/drag combo
    // outright at the moment it would start; hasPoke/isCharging below are
    // untouched by this, so poke/pull still always works on these zones.
    //
    // Gated on rawZoneThisFrame, NOT the debounced/committed zone -- that
    // debounce exists to protect poke-strength selection from a one-frame
    // idle/breathing flicker mid-hover, but it cold-starts at zone 0 (Head)
    // and only trusts a fresh reading after 2 consecutive matching frames.
    // Using it here meant a press on the very first hover frame (a quick,
    // deliberate click-on-arrival) got checked against that stale zone-0
    // default instead of whatever was actually under the cursor, wrongly
    // denying real grabs. rawZoneThisFrame is recomputed fresh every frame
    // hasHit is true, so it's always the actual current zone at press time.
    bool newCapture = pressed && hasHit && ZoneGrabbable((uint)clamp(round(rawZoneThisFrame), 0.0, 11.0));
    bool lockedCapture = held && (wasCapture || newCapture);

    // Poke: a lone LMB/RMB click-release (never part of the LMB+RMB grab
    // combo — the ini only sets POKE_PARAMS.x for exactly one frame, and
    // never while a combo is/was active) now runs as a genuine short
    // SIMULATED grab instead of a bespoke one-frame velocity injection: same
    // anchor logic, same PullTowardLimit-clamped target, same follow filter,
    // same spring/damping selection, same release behaviour as a real grab
    // — just fed a frozen synthetic drag direction instead of live mouse
    // delta, held for a short fixed frame count instead of "however long the
    // mouse is down". The old velocity-injection approach bypassed all of
    // that proven pipeline and had an unresolved ~1-in-3 misfire rate that
    // neither hold-timer nor zone-debounce fixes touched — reusing the
    // grab's own math sidesteps whatever that was rather than patching it
    // blind again.
    float pokeSign = POKE_PARAMS.x;
    bool hasPoke = abs(pokeSign) > 0.5 && hasHit && !newCapture;

    // Charging: continuous (unlike hasPoke's one-frame pulse) — true for the
    // whole duration a lone LMB/RMB is held, before it's released. Used to
    // freeze the hovered normal/basis at the moment the hold STARTS rather
    // than only sampling it fresh at release, so the hand's rotation (which
    // reads this same frozen basis via the cursor-preview bridge) doesn't
    // fight live hover jitter from idle/breathing animation during the hold.
    //
    // capture.w is now a tri-state marker: 1.0 = real locked grab, 0.5 =
    // charging (frozen, not yet fired), 0.0 = idle/finalized. wasCapture's
    // "> 0.5" check safely excludes exactly 0.5, so charging never counts
    // as a real grab.
    float chargeSign = POKE_PARAMS.w;
    bool isCharging = abs(chargeSign) > 0.5 && hasHit && !newCapture && !lockedCapture;
    bool wasCharging = capture.w > 0.25 && capture.w <= 0.5;

    // zoneState.w packs (sign * remaining-hold-frames); 0 = no poke active.
    // A fresh ini pulse always (re)starts the window, even mid-poke.
    float pokeHoldFrames = SafePositive(POKE_PARAMS.z, 8.0);
    bool pokeWasActive = abs(zoneState.w) > 0.5;
    float pokeDirSign = hasPoke ? sign(pokeSign) : (zoneState.w != 0.0 ? sign(zoneState.w) : 0.0);
    float pokeCountdown = hasPoke ? pokeHoldFrames : max(abs(zoneState.w) - 1.0, 0.0);
    bool pokeActive = pokeCountdown > 0.5;

    if (newCapture)
    {
        center = float4(PinnedDetectInfo[SLOT_HIT].xyz, detected.x);
        capture = float4(PinnedDetectInfo[SLOT_NORMAL].xyz, 1.0);
        right = PinnedDetectInfo[SLOT_RIGHT];
        down = PinnedDetectInfo[SLOT_DOWN];
        zoneState.x = debouncedZone;
        screenAnchor = PinnedDetectInfo[10u];
        gizmoXY = PinnedDetectInfo[11u];
        gizmoZ = PinnedDetectInfo[12u];
        captureComponent = PinnedDetectInfo[13u];
        captureDepth = PinnedDetectInfo[14u];
        current = float4(0.0, 0.0, 0.0, 0.0);
        previous = current;
        filtered = current;
        prevFiltered = current;
    }
    else if (isCharging && !wasCharging)
    {
        // Lone hold just started: freeze the hovered normal/anchor/basis NOW
        // — this is the "time of click" reference the hand's rotation and
        // the eventual poke will both use for the whole hold, ignoring
        // whatever the live detector reports afterward until release. No
        // physics reset here: charging is purely an anchor freeze, the
        // actual velocity kick still only happens when the poke fires.
        float3 chargeNormalRaw = PinnedDetectInfo[SLOT_NORMAL].xyz;
        float chargeNormalLen = length(chargeNormalRaw);
        float3 chargeNormalUnit = chargeNormalLen > 1e-6 ? chargeNormalRaw / chargeNormalLen : float3(0.0, 0.0, 1.0);
        center = float4(PinnedDetectInfo[SLOT_HIT].xyz, detected.x);
        capture = float4(chargeNormalUnit, 0.5);
        right = PinnedDetectInfo[SLOT_RIGHT];
        down = PinnedDetectInfo[SLOT_DOWN];
        zoneState.x = debouncedZone;
        screenAnchor = PinnedDetectInfo[10u];
        gizmoXY = PinnedDetectInfo[11u];
        gizmoZ = PinnedDetectInfo[12u];
        captureComponent = PinnedDetectInfo[13u];
        captureDepth = PinnedDetectInfo[14u];
    }
    else if (hasPoke && !wasCapture)
    {
        // Reuse whatever was frozen at the moment charging started, instead
        // of resampling the normal fresh at release — matches whatever the
        // hand visually showed for the entire hold. Falls back to a fresh
        // sample only if a poke somehow fires without having charged first
        // (defensive; shouldn't normally happen given the ini gating).
        // Magnitude bakes in this frame's hold-time strength multiplier
        // (capture.xyz isn't read as a direction-only vector anywhere
        // downstream, only capture.w is), so a later frame's ini value
        // resetting to "resting" can't change the poke's strength mid-window.
        float3 pokeNormalUnit;
        if (wasCharging)
        {
            pokeNormalUnit = capture.xyz;
        }
        else
        {
            float3 pokeNormalRaw = PinnedDetectInfo[SLOT_NORMAL].xyz;
            float pokeNormalLen = length(pokeNormalRaw);
            pokeNormalUnit = pokeNormalLen > 1e-6 ? pokeNormalRaw / pokeNormalLen : float3(0.0, 0.0, 1.0);
            center = float4(PinnedDetectInfo[SLOT_HIT].xyz, detected.x);
            right = PinnedDetectInfo[SLOT_RIGHT];
            down = PinnedDetectInfo[SLOT_DOWN];
            zoneState.x = debouncedZone;
            screenAnchor = PinnedDetectInfo[10u];
            gizmoXY = PinnedDetectInfo[11u];
            gizmoZ = PinnedDetectInfo[12u];
            captureComponent = PinnedDetectInfo[13u];
            captureDepth = PinnedDetectInfo[14u];
        }
        float pokeStrengthMult = SafePositive(POKE_PARAMS.y, 1.0);
        capture = float4(pokeNormalUnit * pokeStrengthMult, 0.0);
        current = float4(0.0, 0.0, 0.0, 0.0);
        previous = current;
        filtered = current;
        prevFiltered = current;
    }

    // Poke drives the exact same target/follow/spring pipeline a real grab
    // does, so "simulateLocked" substitutes for lockedCapture everywhere
    // that pipeline branches on held-vs-released.
    bool simulateLocked = lockedCapture || pokeActive;

    bool basisValid = right.w > 0.5 && down.w > 0.5;
    float2 screenSize = max(CURRENT_CURSOR.zw, float2(1.0, 1.0));
    float screenReference = max(min(screenSize.x, screenSize.y), 1.0);
    float2 delta = (CURRENT_CURSOR.xy - CAPTURED_CURSOR.xy) / screenReference;
    delta *= float2(SafeNonZero(JIGGLE_MULT_EXTRA.y, 1.0), SafeNonZero(POLISH_PARAMS.z, 1.0));

    uint activeZone = (uint)clamp(round(zoneState.x), 0.0, 11.0);
    // Substitutes for the global damping multiplier (JIGGLE_MULT_EXTRA.x)
    // everywhere it's read below, when this zone has a nonzero override.
    float zoneDampingOverride = ZoneDampingOverride(activeZone);
    float globalDampingMult = zoneDampingOverride > 0.0 ? zoneDampingOverride : JIGGLE_MULT_EXTRA.x;
    float zoneSpringOverride = ZoneSpringOverride(activeZone);
    float globalSpringMult = SafePositive(JIGGLE_MULTIPLIERS.w, 1.0)
        * (zoneSpringOverride > 0.0 ? zoneSpringOverride : 1.0);
    float baseStrength = SafePositive(JIGGLE_PARAMS.y, 1.0);
    float zoneStrength = ZoneStrengthOverride(activeZone);
    if (zoneStrength > 0.0)
        baseStrength = zoneStrength;
    float strength = baseStrength * SafePositive(JIGGLE_MULTIPLIERS.z, 1.0);
    float dragScale = SafePositive(JIGGLE_PARAMS.w, 1.0);

    float radius = SafePositive(JIGGLE_PARAMS.x, 0.25) * SafePositive(JIGGLE_MULTIPLIERS.y, 1.0);
    float maxOffset = SafePositive(POLISH_PARAMS.x, radius * 2.0) * SafePositive(JIGGLE_MULTIPLIERS.y, 1.0);
    float zoneMaxOffset = ZoneOffsetOverride(activeZone);
    if (zoneMaxOffset > 0.0)
        maxOffset = zoneMaxOffset;

    // Progressive resistance: the raw linear drag target approaches maxOffset
    // asymptotically instead of being hard-clipped, so pulling further gets
    // progressively harder (stretch-limit feel) rather than hitting a wall.
    // A poke substitutes its frozen synthetic direction (capture.xyz,
    // strength multiplier already baked into its magnitude) for the
    // mouse-derived drag direction.
    //
    // Depth pull: add a fraction of however far you've dragged on screen as
    // an extra pull along the frozen grab normal (capture.xyz — same vector
    // a real grab already freezes at newCapture, unrelated to the poke's
    // reuse of that field), so the drag reads as coming toward the camera
    // instead of only sliding laterally across the screen plane.
    float depthPullMult = SafePositive(DEPTH_PULL_MULT, 0.5);
    float dragDistance2D = length(delta);
    float3 grabNormalUnit = (lockedCapture && length(capture.xyz) > 1e-6) ? normalize(capture.xyz) : float3(0.0, 0.0, 0.0);
    float3 rawDrag = (lockedCapture && basisValid)
        ? (right.xyz * delta.x + down.xyz * delta.y + grabNormalUnit * (dragDistance2D * depthPullMult)) * dragScale
        : (pokeActive ? capture.xyz * pokeDirSign : float3(0.0, 0.0, 0.0));
    float3 rawTarget = PullTowardLimit(rawDrag * strength, maxOffset);

    float follow = saturate(SafePositive(POLISH_PARAMS.w, 0.12));
    float grabDamping = saturate(SafePositive(PHYS_PARAMS.x, 0.86) * SafePositive(globalDampingMult, 1.0));
    float grabSpring = SafePositive(PHYS_PARAMS.y, 0.176) * globalSpringMult;
    float releaseSpring = SafePositive(PHYS_PARAMS.w, 0.055) * globalSpringMult;
    float step = SimulationStep();
    float previousStep = clamp(SafePositive(previous.w, 1.0), 0.05, 8.0);
    float previousTargetStep = clamp(SafePositive(prevFiltered.w, 1.0), 0.05, 8.0);

    float3 targetVelocity = (filtered.xyz - prevFiltered.xyz) / previousTargetStep;
    prevFiltered.xyz = filtered.xyz;
    float targetFollow = simulateLocked ? follow : follow * 0.55;
    float targetFollowStep = 1.0 - pow(saturate(1.0 - targetFollow), step);
    filtered.xyz = ClampLength(filtered.xyz + targetVelocity * (0.35 * step) + (rawTarget - filtered.xyz) * targetFollowStep, maxOffset);

    float3 velocity = (current.xyz - previous.xyz) / previousStep;
    bool justReleased = (!held && wasCapture) || (pokeWasActive && !pokeActive);
    if (justReleased) velocity *= SafePositive(POLISH_PARAMS.y, 1.10);

    // Release damping starts more flexible right after a big/hard release
    // and decays back to steady-state ($ll_jiggle_mult_damping) over a
    // short window instead of snapping straight to it — mimics a real
    // material relaxing loosely after being stretched far, rather than
    // instantly behaving as stiff as a barely-tugged release would.
    // inputFlags.w (always 0 before, never read elsewhere) persists the
    // current boosted multiplier across frames so it can decay smoothly.
    float steadyDampingMult = SafeNonZero(globalDampingMult, 1.0);
    float releaseDampingMult = inputFlags.w != 0.0 ? inputFlags.w : steadyDampingMult;
    if (justReleased)
    {
        // "Distance and click strength" both collapse to the same signal
        // here: how far it was stretched right up to the moment of release.
        float releaseIntensity = maxOffset > 0.0 ? saturate(length(current.xyz) / maxOffset) : 0.0;
        releaseDampingMult = lerp(steadyDampingMult, SafePositive(RELEASE_BOOST.x, 1.05), releaseIntensity);
    }
    else if (!simulateLocked)
    {
        float decayRetain = saturate(SafePositive(RELEASE_BOOST.y, 0.92));
        releaseDampingMult = steadyDampingMult + (releaseDampingMult - steadyDampingMult) * pow(decayRetain, step);
    }
    float releaseDamping = saturate(SafePositive(PHYS_PARAMS.z, 0.96) * releaseDampingMult);

    float damping = simulateLocked ? grabDamping : releaseDamping;
    float spring = simulateLocked ? grabSpring : releaseSpring;
    velocity *= pow(saturate(damping), step);
    float3 next = ClampLength(current.xyz + (velocity + (filtered.xyz - current.xyz) * (spring * step)) * step, maxOffset);
    // capture.w > 0.25 covers both charging (0.5) and a real locked grab
    // (1.0) using this frame's already-updated value — without it, a charge
    // sits at current=next=0 with no physics kick yet (that only starts
    // when the poke actually fires), which would otherwise look "dead" and
    // get wiped by the cleanup below on the very frame it was just frozen.
    bool alive = simulateLocked || capture.w > 0.25 || dot(next, next) > 2.5e-7 || dot(next - current.xyz, next - current.xyz) > 2.5e-7;

    if (!alive)
    {
        next = float3(0.0, 0.0, 0.0);
        filtered.xyz = next;
        prevFiltered.xyz = next;
        center.w = -1.0;
        screenAnchor = 0.0;
        gizmoXY = 0.0;
        gizmoZ = 0.0;
        captureComponent = -1.0;
        captureDepth = 0.0;
    }

    zoneState.w = pokeDirSign * pokeCountdown;

    InteractionState[0u] = float4(next, alive ? 1.0 : 0.0);
    InteractionState[1u] = float4(current.xyz, step);
    InteractionState[2u] = center;
    // Computed fresh from this frame's own booleans rather than "preserved"
    // from the local capture.w — an earlier version tried to preserve it
    // (force 1.0 only when locked, otherwise keep whatever was already
    // there), which meant a real grab's 1.0 never got reset back to 0 once
    // released (nothing else was writing 0 for that case), permanently
    // stuck wasCapture/alive true and made the hand stop responding to the
    // live cursor until the next click overwrote it. isCharging is itself
    // recomputed every frame from live input, so this can't get stuck.
    float captureStateOut = lockedCapture ? 1.0 : (isCharging ? 0.5 : 0.0);
    InteractionState[3u] = float4(capture.xyz, captureStateOut);
    InteractionState[4u] = float4(filtered.xyz, 0.0);
    InteractionState[5u] = float4(prevFiltered.xyz, step);
    InteractionState[6u] = right;
    InteractionState[7u] = down;
    // .y/.z/.w were always 0 (unused). .y/.z carry maxOffset and the current
    // stretch fraction (0..1) so the hand cursor shader (rzm_jiggle_hand.hlsl)
    // can react to how close the grab is to its stretch limit, without
    // duplicating this per-zone math on the CPU side. .w persists the
    // decaying release-damping multiplier across frames (see above).
    float stretchFraction = maxOffset > 0.0 ? saturate(length(next) / maxOffset) : 0.0;
    InteractionState[8u] = float4(held ? 1.0 : 0.0, maxOffset, stretchFraction, releaseDampingMult);
    // Path Slide progress: written only while this zone is both the live
    // active zone AND actively being dragged, so it's left untouched (holds
    // its last value) the instant the zone is released or a different zone
    // becomes active -- unlike Jiggle's offset, which always springs back.
    if (lockedCapture && ZoneIsPathSlide(activeZone))
    {
        uint pathCount;
        PathProgressState.GetDimensions(pathCount);
        if (activeZone < pathCount)
            PathProgressState[activeZone] = stretchFraction;
    }
    InteractionState[9u] = zoneState;
    InteractionState[10u] = screenAnchor;
    InteractionState[11u] = gizmoXY;
    InteractionState[12u] = gizmoZ;
    InteractionState[13u] = captureComponent;
    InteractionState[14u] = captureDepth;
}


