import { spawn } from "node:child_process";

export type ElevatedFileCopy = {
    sourcePath: string;
    targetPath: string;
};

export type ElevatedFileCopyWithBackup = ElevatedFileCopy & {
    backupPath: string;
    existed: boolean;
};

export type ElevatedRollbackFile = {
    targetPath: string;
    backupPath: string;
    existed: boolean;
};

export function isFsPermissionError(error: unknown) {
    const code = (error as NodeJS.ErrnoException | undefined)?.code;
    return code === "EPERM" || code === "EACCES";
}

export function toPowerShellString(value: string) {
    return `'${value.replaceAll("'", "''")}'`;
}

export function toPowerShellBoolean(value: boolean) {
    return value ? "$true" : "$false";
}

export function encodePowerShellCommand(command: string) {
    return Buffer.from(command, "utf16le").toString("base64");
}

export async function runPowerShell(
    command: string,
    spawnErrorPrefix = "ELEVATED_PS_SPAWN_FAILED",
) {
    return await new Promise<number>((resolve, reject) => {
        const child = spawn(
            "powershell.exe",
            [
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-EncodedCommand",
                encodePowerShellCommand(command),
            ],
            { windowsHide: true },
        );
        child.on("error", (error) => reject(new Error(`${spawnErrorPrefix}:${String(error)}`)));
        child.on("close", (code) => resolve(code ?? 1));
    });
}

export async function runElevatedPowerShell(
    command: string,
    spawnErrorPrefix = "ELEVATED_PS_SPAWN_FAILED",
) {
    return await runPowerShell(
        `
$ErrorActionPreference = 'Stop'
try {
$Process = Start-Process -FilePath 'powershell.exe' -Verb RunAs -Wait -PassThru -WindowStyle Hidden -ArgumentList @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-EncodedCommand', '${encodePowerShellCommand(command)}')
if ($null -eq $Process -or $null -eq $Process.ExitCode) { exit 1 }
exit $Process.ExitCode
} catch {
    exit 1
}
`,
        spawnErrorPrefix,
    );
}

function toPowerShellSimpleCopyArray(fileCopies: readonly ElevatedFileCopy[]) {
    return `@(\n${fileCopies
        .map(
            (fileCopy) =>
                `[pscustomobject]@{ SourcePath = ${toPowerShellString(fileCopy.sourcePath)}; TargetPath = ${toPowerShellString(fileCopy.targetPath)} }`,
        )
        .join("\n")}\n)`;
}

function toPowerShellCopyWithBackupArray(fileCopies: readonly ElevatedFileCopyWithBackup[]) {
    return `@(\n${fileCopies
        .map(
            (fileCopy) =>
                `[pscustomobject]@{ SourcePath = ${toPowerShellString(fileCopy.sourcePath)}; TargetPath = ${toPowerShellString(fileCopy.targetPath)}; BackupPath = ${toPowerShellString(fileCopy.backupPath)}; Existed = ${toPowerShellBoolean(fileCopy.existed)} }`,
        )
        .join("\n")}\n)`;
}

function toPowerShellRollbackArray(fileCopies: readonly ElevatedRollbackFile[]) {
    return `@(\n${fileCopies
        .map(
            (fileCopy) =>
                `[pscustomobject]@{ TargetPath = ${toPowerShellString(fileCopy.targetPath)}; BackupPath = ${toPowerShellString(fileCopy.backupPath)}; Existed = ${toPowerShellBoolean(fileCopy.existed)} }`,
        )
        .join("\n")}\n)`;
}

function toPowerShellPathArray(paths: readonly string[]) {
    return `@(\n${paths.map((filePath) => toPowerShellString(filePath)).join("\n")}\n)`;
}

export async function copyFilesElevated(
    fileCopies: readonly ElevatedFileCopy[],
    errorPrefix = "ELEVATED_COPY_FAILED",
) {
    if (fileCopies.length === 0) return;

    const exitCode = await runElevatedPowerShell(`
$ErrorActionPreference = 'Stop'
$Copies = ${toPowerShellSimpleCopyArray(fileCopies)}

foreach ($Copy in $Copies) {
    [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($Copy.TargetPath)) | Out-Null
    Copy-Item -LiteralPath $Copy.SourcePath -Destination $Copy.TargetPath -Force
}
`);
    if (exitCode === 0) return;
    throw new Error(`${errorPrefix}:${exitCode}`);
}

export async function copyFilesWithBackupElevated(
    fileCopies: readonly ElevatedFileCopyWithBackup[],
    errorPrefix = "ELEVATED_COPY_FAILED",
) {
    if (fileCopies.length === 0) return;

    const exitCode = await runElevatedPowerShell(`
$ErrorActionPreference = 'Stop'
$Copies = ${toPowerShellCopyWithBackupArray(fileCopies)}

foreach ($Copy in $Copies) {
    [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($Copy.TargetPath)) | Out-Null
    if ($Copy.Existed -and -not (Test-Path -LiteralPath $Copy.BackupPath)) {
        [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($Copy.BackupPath)) | Out-Null
        Copy-Item -LiteralPath $Copy.TargetPath -Destination $Copy.BackupPath -Force
    }
    Copy-Item -LiteralPath $Copy.SourcePath -Destination $Copy.TargetPath -Force
}
`);
    if (exitCode === 0) return;
    throw new Error(`${errorPrefix}:${exitCode}`);
}

export async function rollbackFilesElevated(
    fileCopies: readonly ElevatedRollbackFile[],
    errorPrefix = "ELEVATED_ROLLBACK_FAILED",
) {
    if (fileCopies.length === 0) return;

    const exitCode = await runElevatedPowerShell(`
$ErrorActionPreference = 'Stop'
$Copies = ${toPowerShellRollbackArray(fileCopies)}

foreach ($Copy in $Copies) {
    if ($Copy.Existed) {
        if (Test-Path -LiteralPath $Copy.BackupPath) {
            [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($Copy.TargetPath)) | Out-Null
            Copy-Item -LiteralPath $Copy.BackupPath -Destination $Copy.TargetPath -Force
        }
    } elseif (Test-Path -LiteralPath $Copy.TargetPath) {
        Remove-Item -LiteralPath $Copy.TargetPath -Force
    }
}
`);
    if (exitCode === 0) return;
    throw new Error(`${errorPrefix}:${exitCode}`);
}

export async function removeFilesElevated(
    paths: readonly string[],
    errorPrefix = "ELEVATED_REMOVE_FAILED",
) {
    if (paths.length === 0) return;

    const exitCode = await runElevatedPowerShell(`
$ErrorActionPreference = 'Stop'
$Paths = ${toPowerShellPathArray(paths)}

foreach ($PathValue in $Paths) {
    if (Test-Path -LiteralPath $PathValue) {
        Remove-Item -LiteralPath $PathValue -Force
    }
}
`);
    if (exitCode === 0) return;
    throw new Error(`${errorPrefix}:${exitCode}`);
}
