import { toErrorMessage } from "@shared/utils";
import type { TFunction } from "i18next";

type ErrorPresentation = {
    description: string;
    details?: string;
};

export function getGameBananaErrorPresentation(error: unknown, t: TFunction): ErrorPresentation {
    const message = toErrorMessage(error);

    if (!message) {
        return {
            description: t("page.gamebanana.error_description_unknown"),
        };
    }

    if (message === "GAMEBANANA_AUTH_FAILED") {
        return {
            description: t("page.gamebanana.error_description_auth"),
        };
    }

    if (message.startsWith("GAMEBANANA_HTTP_ERROR:")) {
        const [, status = "", statusText = ""] = message.split(":");
        return {
            description: t("page.gamebanana.error_description_http", {
                status,
                statusText: statusText ? ` ${statusText}` : "",
            }),
        };
    }

    if (message.startsWith("GAMEBANANA_SCHEMA_ERROR:")) {
        const [, context = "", ...detailParts] = message.split(":");
        const details = detailParts.join(":").trim();
        return {
            description: t("page.gamebanana.error_description_schema", {
                context: context.replaceAll("_", " "),
            }),
            details,
        };
    }

    return {
        description: t("page.gamebanana.error_description_unknown"),
        details: message,
    };
}
