import path from "node:path";
import fse from "fs-extra";
import { getArchiveRootName } from "./utils";

export async function moveWithOverwrite(sourcePath: string, destinationPath: string) {
    if (await fse.pathExists(destinationPath)) {
        await fse.remove(destinationPath);
    }

    await fse.move(sourcePath, destinationPath, { overwrite: true });
}

export async function finalizeStagedDownload(stagingPath: string, destinationDir: string) {
    await fse.ensureDir(destinationDir);

    const stagedEntries = await fse.readdir(stagingPath);
    if (stagedEntries.length === 0) {
        throw new Error("Downloaded file did not produce staged content.");
    }

    const destinationPaths: string[] = [];

    for (const entry of stagedEntries) {
        const sourcePath = path.join(stagingPath, entry);
        const destinationPath = path.join(destinationDir, entry);
        await moveWithOverwrite(sourcePath, destinationPath);
        destinationPaths.push(destinationPath);
    }

    return destinationPaths;
}

export async function applySelectedExtractedName(props: {
    extractedPath: string;
    stagingPath: string;
    requestedFileName: string;
    originalSuggestedFileName: string;
    sanitizeWindowsFilename: (name: string) => string;
}) {
    const {
        extractedPath,
        stagingPath,
        requestedFileName,
        originalSuggestedFileName,
        sanitizeWindowsFilename,
    } = props;

    if (requestedFileName === originalSuggestedFileName || extractedPath === stagingPath) {
        return extractedPath;
    }

    const stats = await fse.stat(extractedPath);
    const desiredName = stats.isDirectory()
        ? getArchiveRootName(requestedFileName, sanitizeWindowsFilename)
        : requestedFileName;

    if (!desiredName || path.basename(extractedPath) === desiredName) {
        return extractedPath;
    }

    const renamedPath = path.join(path.dirname(extractedPath), desiredName);
    await moveWithOverwrite(extractedPath, renamedPath);
    return renamedPath;
}
