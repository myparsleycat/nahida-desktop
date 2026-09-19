import { GameBanana } from "@bindings/gamebanana";
import { runGameBananaEnsureSession } from "@renderer/lib/gamebanana-auth";
import type { GameBananaAuthErrorCode } from "@renderer/lib/gamebanana-auth";
import type { QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { toast } from "sonner";

export {
    getGameBananaAuthErrorCode,
    isManualRmcPrimaryAction,
    type GameBananaAuthErrorCode,
} from "@renderer/lib/gamebanana-auth";

export function showGameBananaAuthFailureToast(t: TFunction, code: GameBananaAuthErrorCode) {
    const copy = gameBananaAuthCopyKey(code);
    toast.error(t(copy.title), { description: t(copy.description) });
}

// Every renderer action that needs an account signs in through this helper, so
// the failure toast and the data refresh after a new session stay identical.
export async function signInGameBanana(t: TFunction, queryClient: QueryClient): Promise<boolean> {
    const result = await runGameBananaEnsureSession(() => GameBanana.EnsureSession());
    if (!result.ok) {
        if (!("stale" in result)) {
            showGameBananaAuthFailureToast(t, result.code);
        }
        return false;
    }
    await queryClient.invalidateQueries({ queryKey: ["gamebanana"] });
    return true;
}

export function gameBananaAuthCopyKey(code: string | null): {
    title: string;
    description: string;
} {
    switch (code) {
        case "GAMEBANANA_LOGIN_INIT_FAILED":
            return {
                title: "page.gamebanana.auth.init_failed_title",
                description: "page.gamebanana.auth.init_failed_description",
            };
        case "GAMEBANANA_AUTH_CHECK_FAILED":
            return {
                title: "page.gamebanana.auth.check_failed_title",
                description: "page.gamebanana.auth.check_failed_description",
            };
        case "GAMEBANANA_LOGIN_CANCELLED":
            return {
                title: "page.gamebanana.auth.cancelled_title",
                description: "page.gamebanana.auth.cancelled_description",
            };
        case "GAMEBANANA_AUTO_LOGIN_UNSUPPORTED":
            return {
                title: "page.gamebanana.auth.unsupported_title",
                description: "page.gamebanana.auth.unsupported_description",
            };
        case "GAMEBANANA_SERVER_UNREACHABLE":
            return {
                title: "page.gamebanana.auth.unreachable_title",
                description: "page.gamebanana.auth.unreachable_description",
            };
        default:
            return {
                title: "page.gamebanana.auth.required_title",
                description: "page.gamebanana.auth.required_description",
            };
    }
}
