import { toErrorMessage } from "@shared/utils";

// Keep in sync with the importer folder errors in internal/xxmi.
const importerFolderErrorCodes = [
    "XXMI_IMPORTER_FOLDER_IN_USE",
    "XXMI_IMPORTER_FOLDER_IS_LAUNCHER",
] as const;

export function importerFolderErrorCode(error: unknown) {
    const message = toErrorMessage(error);
    return importerFolderErrorCodes.find((code) => message.includes(code));
}
