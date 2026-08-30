import { afterEach, describe, expect, it, vi } from "vitest";

import {
    createTouchSettingsBatch,
    type TouchZoneSettingsChange,
} from "./touch-profile-settings-batch";

const settings = (maskStrength: number) => ({
    maskStrength,
    maskCurve: 1,
    maskRadiusScale: 1,
    maskCoreAttenuation: "off" as const,
    strengthPreset: "normal" as const,
    physicsPreset: "normal" as const,
    advanced: { radius: 1, strength: 1, damping: 1, spring: 1, maxOffset: 1, falloff: 1 },
});

const change = (zoneId: string, maskStrength: number): TouchZoneSettingsChange => ({
    componentId: "body",
    zoneId,
    settings: settings(maskStrength),
});

afterEach(() => {
    vi.useRealTimers();
});

describe("touch settings batch", () => {
    it("trailing-debounces and coalesces each zone to its latest settings", async () => {
        vi.useFakeTimers();
        const save = vi.fn(async () => ({ previewChanged: true }));
        const preview = vi.fn();
        const batch = createTouchSettingsBatch({
            debounceMs: 120,
            save,
            onPendingChange: vi.fn(),
            onPreviewRequired: preview,
            onError: vi.fn(),
        });

        batch.enqueue([change("left", 0.5)], true);
        await vi.advanceTimersByTimeAsync(80);
        batch.enqueue([change("left", 0.8), change("right", 0.6)], true);
        await vi.advanceTimersByTimeAsync(119);
        expect(save).not.toHaveBeenCalled();
        await vi.advanceTimersByTimeAsync(1);

        expect(save).toHaveBeenCalledOnce();
        expect(save.mock.calls[0]?.[0]).toEqual([change("left", 0.8), change("right", 0.6)]);
        expect(preview).toHaveBeenCalledOnce();
    });

    it("allows one mutation in flight and immediately flushes one coalesced pending batch", async () => {
        vi.useFakeTimers();
        let releaseFirst: ((value: { previewChanged: boolean }) => void) | undefined;
        const first = new Promise<{ previewChanged: boolean }>((resolve) => {
            releaseFirst = resolve;
        });
        const save = vi
            .fn<(changes: TouchZoneSettingsChange[]) => Promise<{ previewChanged: boolean }>>()
            .mockImplementationOnce(() => first)
            .mockResolvedValue({ previewChanged: true });
        const preview = vi.fn();
        const batch = createTouchSettingsBatch({
            debounceMs: 120,
            save,
            onPendingChange: vi.fn(),
            onPreviewRequired: preview,
            onError: vi.fn(),
        });

        batch.enqueue([change("left", 0.5)], true);
        await vi.advanceTimersByTimeAsync(120);
        batch.enqueue([change("left", 0.7)], true);
        batch.enqueue([change("left", 0.9)], true);
        await vi.advanceTimersByTimeAsync(120);
        expect(save).toHaveBeenCalledOnce();

        releaseFirst?.({ previewChanged: true });
        await vi.runAllTimersAsync();
        await Promise.resolve();

        expect(save).toHaveBeenCalledTimes(2);
        expect(save.mock.calls[1]?.[0]).toEqual([change("left", 0.9)]);
        expect(preview).toHaveBeenCalledOnce();
    });
});
