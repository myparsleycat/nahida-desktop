export type TouchZoneStrengthPreset = "light" | "normal" | "strong";
export type TouchPhysicsPreset = "soft" | "normal" | "firm" | "custom";

export type TouchProfileAdvancedSettings = {
    radius: number;
    strength: number;
    damping: number;
    spring: number;
    maxOffset: number;
    falloff: number;
};

export type TouchZoneSettings = {
    /** Multiplier applied to the generated vertex mask for this zone. */
    maskStrength: number;
    /** Exponent applied to the generated vertex mask to flatten or concentrate its influence. */
    maskCurve: number;
    strengthPreset: TouchZoneStrengthPreset;
    physicsPreset: TouchPhysicsPreset;
    advanced: TouchProfileAdvancedSettings;
};

export const TOUCH_PROFILE_MASK_STRENGTH_RANGE = { min: 0, max: 2, step: 0.05 } as const;
export const TOUCH_PROFILE_MASK_CURVE_RANGE = { min: 0, max: 2, step: 0.05 } as const;

export const TOUCH_PROFILE_SETTING_RANGES = {
    radius: { min: 0.02, max: 1, step: 0.01 },
    strength: { min: 0.1, max: 3, step: 0.05 },
    damping: { min: 0.01, max: 0.99, step: 0.01 },
    spring: { min: 0.01, max: 1, step: 0.01 },
    maxOffset: { min: 0.005, max: 0.3, step: 0.005 },
    falloff: { min: 0.1, max: 4, step: 0.1 },
} as const;

export const DEFAULT_TOUCH_PROFILE_ADVANCED: TouchProfileAdvancedSettings = {
    radius: 0.2,
    strength: 1.15,
    damping: 0.86,
    spring: 0.176,
    maxOffset: 0.065,
    falloff: 1.8,
};

export const TOUCH_PHYSICS_PRESETS: Record<
    Exclude<TouchPhysicsPreset, "custom">,
    TouchProfileAdvancedSettings
> = {
    soft: {
        radius: 0.24,
        strength: 0.9,
        damping: 0.92,
        spring: 0.12,
        maxOffset: 0.045,
        falloff: 1.35,
    },
    normal: { ...DEFAULT_TOUCH_PROFILE_ADVANCED },
    firm: {
        radius: 0.16,
        strength: 1.35,
        damping: 0.8,
        spring: 0.26,
        maxOffset: 0.09,
        falloff: 2.2,
    },
};

export const TOUCH_ZONE_STRENGTH_MULTIPLIERS: Record<TouchZoneStrengthPreset, number> = {
    light: 0.75,
    normal: 1,
    strong: 1.3,
};

export function createDefaultTouchZoneSettings(): TouchZoneSettings {
    return {
        maskStrength: 1,
        maskCurve: 1,
        strengthPreset: "normal",
        physicsPreset: "normal",
        advanced: { ...DEFAULT_TOUCH_PROFILE_ADVANCED },
    };
}
