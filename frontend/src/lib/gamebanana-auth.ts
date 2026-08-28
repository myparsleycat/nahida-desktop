export type GameBananaAuthErrorCode =
    | "GAMEBANANA_AUTH_FAILED"
    | "GAMEBANANA_LOGIN_CANCELLED"
    | "GAMEBANANA_AUTO_LOGIN_UNSUPPORTED"
    | "GAMEBANANA_SERVER_UNREACHABLE";

export function getGameBananaAuthErrorCode(error: unknown): GameBananaAuthErrorCode {
    if (!(error instanceof Error)) {
        return "GAMEBANANA_AUTH_FAILED";
    }

    switch (error.message) {
        case "GAMEBANANA_LOGIN_CANCELLED":
        case "GAMEBANANA_AUTO_LOGIN_UNSUPPORTED":
        case "GAMEBANANA_SERVER_UNREACHABLE":
            return error.message;
        default:
            return "GAMEBANANA_AUTH_FAILED";
    }
}

export function isManualRmcPrimaryAction(code: GameBananaAuthErrorCode | string | null): boolean {
    return code === "GAMEBANANA_AUTO_LOGIN_UNSUPPORTED";
}

export async function runGameBananaEnsureSession(
    ensureSession: () => Promise<unknown>,
    isCurrent: () => boolean = () => true,
): Promise<
    { ok: true } | { ok: false; code: GameBananaAuthErrorCode } | { ok: false; stale: true }
> {
    try {
        await ensureSession();
        if (!isCurrent()) {
            return { ok: false, stale: true };
        }
        return { ok: true };
    } catch (error) {
        if (!isCurrent()) {
            return { ok: false, stale: true };
        }
        return { ok: false, code: getGameBananaAuthErrorCode(error) };
    }
}
