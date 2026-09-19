export type GameBananaAuthErrorCode =
    | "GAMEBANANA_AUTH_REQUIRED"
    | "GAMEBANANA_LOGIN_INIT_FAILED"
    | "GAMEBANANA_AUTH_CHECK_FAILED"
    | "GAMEBANANA_AUTH_FAILED"
    | "GAMEBANANA_LOGIN_CANCELLED"
    | "GAMEBANANA_AUTO_LOGIN_UNSUPPORTED"
    | "GAMEBANANA_SERVER_UNREACHABLE";

export function getGameBananaAuthErrorCode(error: unknown): GameBananaAuthErrorCode {
    if (!(error instanceof Error)) {
        return "GAMEBANANA_AUTH_FAILED";
    }

    switch (error.message) {
        case "GAMEBANANA_AUTH_REQUIRED":
        case "GAMEBANANA_LOGIN_INIT_FAILED":
        case "GAMEBANANA_AUTH_CHECK_FAILED":
        case "GAMEBANANA_LOGIN_CANCELLED":
        case "GAMEBANANA_AUTO_LOGIN_UNSUPPORTED":
        case "GAMEBANANA_SERVER_UNREACHABLE":
            return error.message;
        default:
            return "GAMEBANANA_AUTH_FAILED";
    }
}

export function isManualRmcPrimaryAction(code: string | null): boolean {
    return code === "GAMEBANANA_AUTO_LOGIN_UNSUPPORTED";
}

// EnsureSession is the explicit sign-in entry point. Data requests never open
// the login window, so a renderer action that needs an account calls this and
// reports the outcome from its own UI.
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
