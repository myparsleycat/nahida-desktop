import { NahidaDesktop } from "..";
import path from "node:path";
import fse from "fs-extra";
import { app } from "electron";
import { GoProcess } from "../lib/go-process";

export class ArchiveService {
    private readonly desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    async extract(
        archivePath: string,
        targetDir: string,
        onProgress?: (percent: number, message: string) => void,
    ): Promise<string> {
        await fse.ensureDir(targetDir);

        const extractorPath = this.getExtractorPath();

        if (!fse.existsSync(extractorPath)) {
            throw new Error(
                `Extractor binary not found at: ${extractorPath}. Please ensure the application is built correctly.`,
            );
        }

        const goProcess = new GoProcess({
            path: extractorPath,
            args: [archivePath, targetDir],
        });

        if (onProgress) {
            goProcess.on("progress", (payload: { percent: number; message: string }) => {
                onProgress(payload.percent, payload.message);
            });
        }

        try {
            const result = await goProcess.start();
            return result as string;
        } catch (error: any) {
            if (error.code === "ENOENT") {
                throw new Error(`Extractor binary not found: ${extractorPath}`);
            }
            throw new Error(`Extraction failed: ${error.message}`);
        }
    }

    private getExtractorPath(): string {
        if (app.isPackaged) {
            return path.join(app.getAppPath(), "..", "extractor", "extractor.exe");
        }
        return path.join(app.getAppPath(), "build", "extractor", "extractor.exe");
    }
}

export default ArchiveService;
