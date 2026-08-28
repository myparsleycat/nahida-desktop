export function isTerminalFixerProgressCode(code: string) {
    return (
        code.includes("SUCCESS") ||
        code.includes("ALREADY") ||
        code.includes("ERR") ||
        code === "XXMI_OBFUSCATE_BACKUP_EXISTS"
    );
}
