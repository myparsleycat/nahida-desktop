import fse from "fs-extra";

export function disabledPathFor(iniPath: string, suffix: string) {
    return `${iniPath}.${suffix}`;
}

export async function isIniDisabled(iniPath: string, suffix: string) {
    const disabledPath = disabledPathFor(iniPath, suffix);
    if (!(await fse.pathExists(disabledPath))) return false;
    return !(await fse.pathExists(iniPath));
}

export async function isIniEnabled(iniPath: string, suffix: string) {
    if (!(await fse.pathExists(iniPath))) return false;
    return !(await fse.pathExists(disabledPathFor(iniPath, suffix)));
}

export async function renameIniDisable(iniPath: string, suffix: string) {
    if (await isIniDisabled(iniPath, suffix)) return;

    const target = disabledPathFor(iniPath, suffix);
    try {
        await fse.rename(iniPath, target);
    } catch (err) {
        const code = (err as NodeJS.ErrnoException).code;
        if (code === "EEXIST" || code === "EPERM") {
            if (await isIniDisabled(iniPath, suffix)) return;
        }
        throw err;
    }

    if (!(await isIniDisabled(iniPath, suffix))) {
        throw new Error(`Failed to disable ini: ${iniPath}`);
    }
}

export async function renameIniEnable(iniPath: string, suffix: string) {
    if (await isIniEnabled(iniPath, suffix)) return;

    const target = disabledPathFor(iniPath, suffix);
    try {
        await fse.rename(target, iniPath);
    } catch (err) {
        const code = (err as NodeJS.ErrnoException).code;
        if (code === "ENOENT" || code === "EPERM") {
            if (await isIniEnabled(iniPath, suffix)) return;
        }
        throw err;
    }

    if (!(await isIniEnabled(iniPath, suffix))) {
        throw new Error(`Failed to enable ini: ${iniPath}`);
    }
}
