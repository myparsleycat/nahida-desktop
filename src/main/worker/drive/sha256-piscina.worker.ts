import { createHash } from "node:crypto";
import fs from "node:fs";

export default function ({ path }: { path: string }) {
    return new Promise<string>((resolve, reject) => {
        const hash = createHash("sha256");
        const stream = fs.createReadStream(path);

        stream.on("data", (chunk) => {
            hash.update(chunk);
        });

        stream.on("end", () => {
            resolve(hash.digest("hex"));
        });

        stream.on("error", (err) => {
            reject(err);
        });
    });
}
