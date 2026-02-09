import { NahidaDesktop } from "..";
import fse from "fs-extra";
import { extractArchive } from "@native/extractor";

export class ArchiveService {
    private readonly desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    async extract(
        archivePath: string,
        targetDir: string,
        _onProgress?: (percent: number, message: string) => void,
    ): Promise<string> {
        await fse.ensureDir(targetDir);

        try {
            await extractArchive(archivePath, targetDir);

            if (_onProgress) {
                _onProgress(100, "Extraction complete");
            }

            return targetDir;
        } catch (error: any) {
            throw new Error(`Failed to extract archive: ${error.message}`);
        }
    }
}

export default ArchiveService;
