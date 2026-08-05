import {
    TOUCH_PROFILE_MASK_CURVE_RANGE,
    TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE,
    TOUCH_PROFILE_MASK_STRENGTH_RANGE,
    TOUCH_PROFILE_SETTING_RANGES,
    TOUCH_ZONE_STRENGTH_MULTIPLIERS,
    type TouchProfileAdvancedSettings,
    type TouchZoneSettings,
    type TouchZoneStrengthPreset,
} from "@shared/touch-profile-settings";

import { DEFAULT_TOUCH_JIGGLE_PARAMS, type TouchJiggleParams } from "./touch-profile-types";

const RELEASE_DAMPING_DELTA =
    DEFAULT_TOUCH_JIGGLE_PARAMS.releaseDamping - DEFAULT_TOUCH_JIGGLE_PARAMS.grabDamping;
const RELEASE_SPRING_RATIO =
    DEFAULT_TOUCH_JIGGLE_PARAMS.releaseSpring / DEFAULT_TOUCH_JIGGLE_PARAMS.grabSpring;

export function normalizeTouchZoneSettings(input: unknown): TouchZoneSettings {
    if (!isRecord(input)) throw new Error("Touch zone settings must be an object");

    const strengthPreset = input.strengthPreset;
    if (!isTouchZoneStrengthPreset(strengthPreset)) {
        throw new Error(`Invalid touch strength preset: ${String(strengthPreset)}`);
    }

    const physicsPreset = input.physicsPreset;
    if (!isTouchPhysicsPreset(physicsPreset)) {
        throw new Error(`Invalid touch physics preset: ${String(physicsPreset)}`);
    }

    const maskStrength = input.maskStrength ?? 1;
    if (
        typeof maskStrength !== "number" ||
        !Number.isFinite(maskStrength) ||
        maskStrength < TOUCH_PROFILE_MASK_STRENGTH_RANGE.min ||
        maskStrength > TOUCH_PROFILE_MASK_STRENGTH_RANGE.max
    ) {
        throw new Error("Touch mask strength out of range");
    }

    const maskCurve = input.maskCurve ?? 1;
    if (
        typeof maskCurve !== "number" ||
        !Number.isFinite(maskCurve) ||
        maskCurve < TOUCH_PROFILE_MASK_CURVE_RANGE.min ||
        maskCurve > TOUCH_PROFILE_MASK_CURVE_RANGE.max
    ) {
        throw new Error("Touch mask curve out of range");
    }

    const maskRadiusScale = input.maskRadiusScale ?? 1;
    if (
        typeof maskRadiusScale !== "number" ||
        !Number.isFinite(maskRadiusScale) ||
        maskRadiusScale < TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.min ||
        maskRadiusScale > TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.max
    ) {
        throw new Error("Touch mask radius scale out of range");
    }

    if (!isRecord(input.advanced)) {
        throw new Error("Touch advanced settings must be an object");
    }
    const advancedInput = input.advanced;

    const advanced = Object.fromEntries(
        Object.keys(TOUCH_PROFILE_SETTING_RANGES).map((key) => {
            const value = advancedInput[key];
            const range =
                TOUCH_PROFILE_SETTING_RANGES[key as keyof typeof TOUCH_PROFILE_SETTING_RANGES];
            if (typeof value !== "number" || !Number.isFinite(value)) {
                throw new Error(`Invalid touch advanced setting: ${key}`);
            }
            if (value < range.min || value > range.max) {
                throw new Error(`Touch advanced setting out of range: ${key}`);
            }
            return [key, value];
        }),
    ) as unknown as TouchProfileAdvancedSettings;

    return { maskStrength, maskCurve, maskRadiusScale, strengthPreset, physicsPreset, advanced };
}

export function resolveTouchJiggleParams(settings: TouchZoneSettings, objectId: number) {
    const advanced = settings.advanced;
    const strength = advanced.strength * TOUCH_ZONE_STRENGTH_MULTIPLIERS[settings.strengthPreset];

    return {
        ...DEFAULT_TOUCH_JIGGLE_PARAMS,
        objectId,
        radius: advanced.radius,
        strength,
        falloff: advanced.falloff,
        grabDamping: advanced.damping,
        grabSpring: advanced.spring,
        releaseDamping: Math.min(0.99, advanced.damping + RELEASE_DAMPING_DELTA),
        releaseSpring: advanced.spring * RELEASE_SPRING_RATIO,
        maxOffset: advanced.maxOffset,
    } satisfies TouchJiggleParams;
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isTouchZoneStrengthPreset(value: unknown): value is TouchZoneStrengthPreset {
    return value === "light" || value === "normal" || value === "strong";
}

function isTouchPhysicsPreset(value: unknown): value is "soft" | "normal" | "firm" | "custom" {
    return value === "soft" || value === "normal" || value === "firm" || value === "custom";
}
