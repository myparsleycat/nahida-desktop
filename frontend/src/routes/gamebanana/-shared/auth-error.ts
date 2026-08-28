export {
    getGameBananaAuthErrorCode,
    isManualRmcPrimaryAction,
    type GameBananaAuthErrorCode,
} from "@renderer/lib/gamebanana-auth";

export function gameBananaAuthCopyKey(code: string | null): {
    title: string;
    description: string;
} {
    switch (code) {
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
