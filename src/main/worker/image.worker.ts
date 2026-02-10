import { parentPort } from "node:worker_threads";
import { convertImage, type ResizeOptions } from "@native/image";

const port = parentPort;
if (!port) throw new Error("IllegalState");

port.on("message", async (e: { path: string; options: ResizeOptions }) => {
    const { path, options } = e;

    try {
        const resizedImg = await convertImage(path, options);
        port.postMessage({ type: "complete", resizedImg });
    } catch (error) {
        port.postMessage({ type: "error", error: (error as Error).message });
    }
});
