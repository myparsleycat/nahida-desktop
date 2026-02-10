import { extractArchive } from "@native/extractor";
import fse from "fs-extra";
import type { NahidaDesktop } from "..";

export class ArchiveService {
    constructor(desktop: NahidaDesktop) {}

    async extract(
        archivePath: string,
        targetDir: string,
        _onProgress?: (percent: number, message: string) => void,
    ): Promise<string> {
        await fse.ensureDir(targetDir);

        try {
            const extractedPath = await extractArchive(archivePath, targetDir);

            if (_onProgress) {
                _onProgress(100, "Extraction complete");
            }

            return extractedPath;
        } catch (error: any) {
            throw new Error(`Failed to extract archive: ${error.message}`);
        }
    }
}

export default ArchiveService;
