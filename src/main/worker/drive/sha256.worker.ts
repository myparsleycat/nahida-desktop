import { createHash } from "node:crypto";
import fs from "node:fs";
import { parentPort } from "node:worker_threads";

const port = parentPort;
if (!port) throw new Error("IllegalState");

async function calculateSHA256(filePath: string): Promise<string> {
    return new Promise((resolve, reject) => {
        const hash = createHash("sha256");
        const stream = fs.createReadStream(filePath);

        stream.on("data", (chunk) => {
            hash.update(chunk);
        });

        stream.on("end", () => {
            resolve(hash.digest("hex"));
        });

        stream.on("error", reject);
    });
}

port.on("message", async (e: any) => {
    const { files } = e;
    const hashes: [string, string][] = [];

    try {
        for (let i = 0; i < files.length; i++) {
            const { FID, path } = files[i];

            const hash = await calculateSHA256(path);
            hashes.push([FID, hash]);

            files[i] = null;

            port.postMessage({ type: "progress", fileIndex: i });
        }

        port.postMessage({ type: "complete", hashes });
    } catch (error) {
        port.postMessage({ type: "error", error: (error as Error).message });
    } finally {
        files.length = 0;
    }
});
