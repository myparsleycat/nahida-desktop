import { toErrorMessage } from "@shared/utils";

export const ELEVATED_HELPER_REQUIRED = "ELEVATED_HELPER_REQUIRED";

export const ELEVATED_HELPER_STATUS_EVENT = "elevated:status";

export const ELEVATED_HELPER_ACTIVITY_ID = "elevated-helper";

export type ElevatedHelperStatus = {
    enabled: boolean;
    running: boolean;
};

export function isElevatedHelperRequiredError(error: unknown): boolean {
    return toErrorMessage(error).includes(ELEVATED_HELPER_REQUIRED);
}
