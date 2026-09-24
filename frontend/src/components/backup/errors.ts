import { toErrorMessage } from "@shared/utils";
import type { TFunction } from "i18next";

const KNOWN_ERRORS = [
    "BACKUP_RUNNING",
    "BACKUP_NOT_LOGGED_IN",
    "BACKUP_NO_TARGETS",
    "BACKUP_TARGET_MISSING",
    "BACKUP_UNREADABLE_FILE",
    "BACKUP_NOT_DIRECTORY",
    "BACKUP_UPLOAD_FAILED",
    "BACKUP_INVALID_DEVICE_NAME",
    "RESTORE_DESTINATION_NOT_EMPTY",
    "RESTORE_HASH_MISMATCH",
] as const;

// backupErrorMessage names a backup failure in the user's language when the
// service answered one of its own codes, and shows the raw message otherwise.
export function backupErrorMessage(t: TFunction, error: unknown) {
    const message = typeof error === "string" ? error : toErrorMessage(error);
    const code = KNOWN_ERRORS.find((known) => message.includes(known));
    return code ? t(`page.backup.error.${code}`) : message;
}
