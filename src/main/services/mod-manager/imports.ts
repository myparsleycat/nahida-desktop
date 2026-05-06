import path from "node:path";
import type { ArchiveExtractPathMode, ResolvedArchiveExtractPathMode } from "@shared/mod";
import fse from "fs-extra";
import writeFileAtomic from "write-file-atomic";
import type { NahidaDesktop } from "../..";
import type { ModShaderFixesService } from "./shader-fixes";

export class ModImportsService {
    constructor(
        private readonly desktop: NahidaDesktop,
        private readonly shaderFixes: ModShaderFixesService,
    ) {}

    public async extractArchiveToGroup(
        archivePath: string,
        groupPath: string,
        mode?: ResolvedArchiveExtractPathMode,
    ): Promise<void> {
        const deleteAfterExtract = await this.desktop.setting.mod.getDeleteArchiveAfterExtract();
        const extractMode: ArchiveExtractPathMode =
            mode ?? (await this.desktop.setting.mod.getArchiveExtractPathMode());

        if (extractMode === "ask_every_time") {
            throw new Error("ARCHIVE_EXTRACT_MODE_PROMPT_REQUIRED");
        }

        const flattenSingleRoot = extractMode !== "keep_archive_root";

        try {
            const finalTargetPath = await this.desktop.service.archive.extract(
                archivePath,
                groupPath,
                {
                    flattenSingleRoot,
                },
            );

            this.desktop.logger.info(
                `Extracted archive ${archivePath} to ${finalTargetPath}`,
                "Mod:extractArchiveToGroup",
            );

            if (deleteAfterExtract) {
                await fse.remove(archivePath);
            }
        } catch (error) {
            this.desktop.logger.error(error, `Mod:extractArchiveToGroup:${archivePath}`);
            throw error;
        }
    }

    public async copyFolderToGroup(
        folderPath: string,
        groupPath: string,
        move: boolean,
    ): Promise<void> {
        try {
            const folderName = path.basename(folderPath);
            const targetPath = path.join(groupPath, folderName);

            const exists = await fse.pathExists(targetPath);
            if (exists) {
                throw new Error(`ALREADY_EXISTS:${folderName}`);
            }

            if (move) {
                await fse.move(folderPath, targetPath);
                this.desktop.logger.info(
                    `Moved folder ${folderPath} to ${targetPath}`,
                    "Mod:copyFolderToGroup",
                );
            } else {
                await fse.copy(folderPath, targetPath);
                await this.shaderFixes.deleteModManifest(targetPath);
                this.desktop.logger.info(
                    `Copied folder ${folderPath} to ${targetPath}`,
                    "Mod:copyFolderToGroup",
                );
            }
        } catch (error) {
            this.desktop.logger.error(error, `Mod:copyFolderToGroup:${folderPath}`);
            throw error;
        }
    }

    public async pastePreview(
        modPath: string,
        data: string,
        type: "url" | "base64" | "path",
        existingPreviewPath?: string,
    ): Promise<void> {
        try {
            let buffer: Buffer;
            let extension = ".png";

            if (type === "url") {
                const response = await fetch(data);
                if (!response.ok) {
                    throw new Error(`Failed to download image: ${response.statusText}`);
                }
                const contentType = response.headers.get("content-type");
                if (contentType) {
                    const ext = contentType.split("/")[1];
                    if (ext) extension = `.${ext}`;
                }
                const arrayBuffer = await response.arrayBuffer();
                buffer = Buffer.from(arrayBuffer);
            } else if (type === "base64") {
                const matches = data.match(/^data:image\/([a-zA-Z0-9]+);base64,(.+)$/);
                if (matches) {
                    extension = `.${matches[1]}`;
                    buffer = Buffer.from(matches[2], "base64");
                } else {
                    buffer = Buffer.from(data, "base64");
                }
            } else if (type === "path") {
                extension = path.extname(data);
                buffer = await fse.readFile(data);
            } else {
                throw new Error(`Invalid paste type: ${type}`);
            }

            const normalizedModPath = path.resolve(modPath);
            const fileName = `preview${extension.toLowerCase()}`;
            const filePath = path.join(normalizedModPath, fileName);

            await writeFileAtomic(filePath, buffer);

            const normalizedExistingPreviewPath = existingPreviewPath
                ? path.resolve(existingPreviewPath)
                : null;
            const existingEntries = await fse.readdir(normalizedModPath);
            const stalePreviewPaths = existingEntries
                .filter((entry) => /^preview\.[^.]+$/i.test(entry))
                .map((entry) => path.join(normalizedModPath, entry))
                .filter((entryPath) => entryPath !== filePath);

            if (
                normalizedExistingPreviewPath &&
                normalizedExistingPreviewPath.startsWith(`${normalizedModPath}${path.sep}`) &&
                /^preview\.[^.]+$/i.test(path.basename(normalizedExistingPreviewPath)) &&
                normalizedExistingPreviewPath !== filePath &&
                !stalePreviewPaths.includes(normalizedExistingPreviewPath)
            ) {
                stalePreviewPaths.push(normalizedExistingPreviewPath);
            }

            await Promise.all(stalePreviewPaths.map((entryPath) => fse.remove(entryPath)));
            this.desktop.logger.info(`Saved preview media atomically to ${filePath}`, "Mod:pastePreview");
        } catch (error) {
            this.desktop.logger.error(error, `Mod:pastePreview:${modPath}`);
            throw error;
        }
    }
}
