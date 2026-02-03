import { parentPort, workerData } from "worker_threads";
import { decode } from "cbor-x";
import ky from "ky";
import { decompress } from "fzstd";
import { getHeaders } from "@main/internal/fetcher";

const port = parentPort;
if (!port) throw new Error("IllegalState");

export async function decompressData(base64String: string) {
    const binaryString = atob(base64String);
    const compressedData = new Uint8Array(binaryString.length);
    for (let i = 0; i < binaryString.length; i++) {
        compressedData[i] = binaryString.charCodeAt(i);
    }
    return decompress(compressedData);
}

const processStreamedData = async (jsonString: string) => {
    const payload: {
        type: "cbor" | "string";
        compressed: boolean;
        data: string;
    } = JSON.parse(jsonString);

    if (payload.compressed) {
        const decompressed = await decompressData(payload.data);
        if (payload.type === "cbor") {
            return decode(decompressed);
        }
        const decoder = new TextDecoder();
        return decoder.decode(decompressed);
    }

    return JSON.parse(payload.data);
};

port.on("message", async () => {
    const { url, token } = workerData;

    try {
        const resp = await ky.get(url, {
            headers: {
                ...(await getHeaders(url)),
                Accept: "text/event-stream",
            },
            credentials: "include",
            // @ts-expect-error - dispatcher is not in the type definition, but it's passed through to fetch.
            dispatcher: await getAgent(),
        });

        const reader = resp.body?.getReader();
        if (!reader) {
            throw new Error("Failed to get ReadableStream reader.");
        }

        const decoder = new TextDecoder();
        let buffer = "";

        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            buffer += decoder.decode(value, { stream: true });
            const parts = buffer.split("\n\n");
            buffer = parts.pop() || "";

            for (const part of parts) {
                let eventType = "message";
                let eventData = "";

                const lines = part.split("\n");
                for (const line of lines) {
                    if (line.startsWith("event: ")) {
                        eventType = line.substring(7).trim();
                    } else if (line.startsWith("data: ")) {
                        eventData = line.substring(6).trim();
                    }
                }

                if (eventData) {
                    try {
                        switch (eventType) {
                            case "dirs": {
                                const dirsChunk = await processStreamedData(eventData);
                                port.postMessage({
                                    type: "dirs",
                                    payload: dirsChunk,
                                });
                                break;
                            }
                            case "files": {
                                const filesChunk = await processStreamedData(eventData);
                                port.postMessage({
                                    type: "files",
                                    payload: filesChunk,
                                });
                                break;
                            }
                            case "metadata": {
                                const metadata = JSON.parse(eventData);
                                port.postMessage({
                                    type: "metadata",
                                    payload: metadata,
                                });
                                break;
                            }
                            case "complete": {
                                port.postMessage({ type: "complete" });
                                await reader.cancel();
                                return;
                            }
                            case "error": {
                                const data = JSON.parse(eventData);
                                port.postMessage({
                                    type: "error",
                                    payload: data.message || "An unknown server error occurred",
                                });
                                await reader.cancel();
                                return;
                            }
                        }
                    } catch (err) {
                        port.postMessage({
                            type: "error",
                            payload: `Failed to process ${eventType} data`,
                        });
                        await reader.cancel();
                        return;
                    }
                }
            }
        }
    } catch (err) {
        if (err instanceof Error && err.name !== "AbortError") {
            port.postMessage({
                type: "error",
                payload: "SSE connection error: " + err.message,
            });
        }
    }
});
