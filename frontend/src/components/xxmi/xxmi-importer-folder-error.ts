import { toErrorMessage } from "@shared/utils";

// Keep in sync with the importer folder errors in internal/xxmi.
const importerFolderErrorCodes = [
    "XXMI_IMPORTER_FILE_ACCESS_DENIED",
    "XXMI_IMPORTER_FOLDER_IN_USE",
    "XXMI_IMPORTER_FOLDER_IS_LAUNCHER",
] as const;

export function importerFolderError(error: unknown) {
    const message = toErrorMessage(error);
    const code = importerFolderErrorCodes.find((code) => message.includes(code));
    if (!code) return undefined;

    // A code may carry "CODE: detail"; a rollback failure joined to the error follows on the next line.
    const [detail = ""] = message
        .slice(message.indexOf(code) + code.length)
        .replace(/^:\s*/, "")
        .split("\n");
    return { code, detail: detail.trim() };
}
