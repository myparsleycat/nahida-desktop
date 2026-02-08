import { NahidaDesktop } from "..";
import path from "node:path";
import fse from "fs-extra";
import { app } from "electron";
import koffi from "koffi";

try {
    koffi.proto("void ProgressCallback(double percent, const char* message)");
} catch (e) {
    // ignore duplicate type error
}

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
                `Extractor library not found at: ${extractorPath}. Please ensure the application is built correctly.`,
            );
        }

        return new Promise<string>((resolve, reject) => {
            try {
                const lib = koffi.load(extractorPath);

                const ExtractArchive = lib.func(
                    "const char* ExtractArchive(const char* archive, const char* output, ProgressCallback* cb)",
                );

                const callback = koffi.register((percent, message) => {
                    if (onProgress) {
                        onProgress(percent, message);
                    }
                }, koffi.pointer("ProgressCallback"));

                ExtractArchive.async(archivePath, targetDir, callback, (err, res) => {
                    koffi.unregister(callback);
                    lib.unload();

                    if (err) {
                        reject(new Error(`FFI Call Failed: ${err.message}`));
                        return;
                    }

                    if (res) {
                        try {
                            const result = JSON.parse(res);
                            if (result.success) {
                                resolve(result.data);
                            } else {
                                reject(new Error(result.data));
                            }
                        } catch (parseError: any) {
                            reject(
                                new Error(
                                    `Failed to parse extractor result: ${parseError.message}`,
                                ),
                            );
                        }
                    } else {
                        reject(new Error("Extraction returned empty result"));
                    }
                });
            } catch (error: any) {
                reject(new Error(`Failed to initiate extraction: ${error.message}`));
            }
        });
    }

    private getExtractorPath(): string {
        const ext = "dll";
        if (app.isPackaged) {
            return path.join(app.getAppPath(), "..", "lib", `extractor.${ext}`);
        }
        return path.join(app.getAppPath(), "build", "extractor", `extractor.${ext}`);
    }
}

export default ArchiveService;
