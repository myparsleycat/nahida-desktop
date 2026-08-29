import { describe, expect, it } from "vitest";

import { gameBananaAuthCopyKey } from "../routes/gamebanana/-shared/auth-error";
import {
    getGameBananaAuthErrorCode,
    isManualRmcPrimaryAction,
    runGameBananaEnsureSession,
} from "./gamebanana-auth";

describe("getGameBananaAuthErrorCode", () => {
    it("classifies cancelled, unsupported, unreachable, and generic failures", () => {
        expect(getGameBananaAuthErrorCode(new Error("GAMEBANANA_LOGIN_CANCELLED"))).toBe(
            "GAMEBANANA_LOGIN_CANCELLED",
        );
        expect(getGameBananaAuthErrorCode(new Error("GAMEBANANA_AUTO_LOGIN_UNSUPPORTED"))).toBe(
            "GAMEBANANA_AUTO_LOGIN_UNSUPPORTED",
        );
        expect(getGameBananaAuthErrorCode(new Error("GAMEBANANA_SERVER_UNREACHABLE"))).toBe(
            "GAMEBANANA_SERVER_UNREACHABLE",
        );
        expect(getGameBananaAuthErrorCode(new Error("GAMEBANANA_AUTH_FAILED"))).toBe(
            "GAMEBANANA_AUTH_FAILED",
        );
        expect(getGameBananaAuthErrorCode(new Error("something else"))).toBe(
            "GAMEBANANA_AUTH_FAILED",
        );
        expect(getGameBananaAuthErrorCode("nope")).toBe("GAMEBANANA_AUTH_FAILED");
    });
});

describe("isManualRmcPrimaryAction", () => {
    it("keeps manual rmc as the main action when auto-login is unsupported", () => {
        expect(isManualRmcPrimaryAction("GAMEBANANA_AUTO_LOGIN_UNSUPPORTED")).toBe(true);
        expect(isManualRmcPrimaryAction("GAMEBANANA_AUTH_FAILED")).toBe(false);
        expect(isManualRmcPrimaryAction("GAMEBANANA_LOGIN_CANCELLED")).toBe(false);
    });
});

describe("runGameBananaEnsureSession", () => {
    it("calls EnsureSession once on retry", async () => {
        let calls = 0;
        const result = await runGameBananaEnsureSession(async () => {
            calls += 1;
        });
        expect(result).toEqual({ ok: true });
        expect(calls).toBe(1);
    });

    it("classifies a failed EnsureSession without retrying", async () => {
        let calls = 0;
        const result = await runGameBananaEnsureSession(async () => {
            calls += 1;
            throw new Error("GAMEBANANA_AUTO_LOGIN_UNSUPPORTED");
        });
        expect(result).toEqual({
            ok: false,
            code: "GAMEBANANA_AUTO_LOGIN_UNSUPPORTED",
        });
        expect(calls).toBe(1);
        expect(
            result.ok || !("code" in result) ? false : isManualRmcPrimaryAction(result.code),
        ).toBe(true);
    });

    it("does not treat a stale unmount result as an auth error", async () => {
        const result = await runGameBananaEnsureSession(
            async () => {
                throw new Error("GAMEBANANA_AUTH_FAILED");
            },
            () => false,
        );
        expect(result).toEqual({ ok: false, stale: true });
    });
});

describe("gameBananaAuthCopyKey", () => {
    it("maps each auth error to distinct copy", () => {
        expect(gameBananaAuthCopyKey("GAMEBANANA_LOGIN_CANCELLED").title).toContain("cancelled");
        expect(gameBananaAuthCopyKey("GAMEBANANA_AUTO_LOGIN_UNSUPPORTED").title).toContain(
            "unsupported",
        );
        expect(gameBananaAuthCopyKey("GAMEBANANA_SERVER_UNREACHABLE").title).toContain(
            "unreachable",
        );
        expect(gameBananaAuthCopyKey("GAMEBANANA_AUTH_FAILED").title).toContain("required");
    });
});
