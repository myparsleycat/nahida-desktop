import { isArrayBuffer } from "node:util/types";
import { serve } from "@hono/node-server";
import { createNodeWebSocket } from "@hono/node-ws";
import { appVersion } from "@main/const";
import { decode } from "cbor-x";
import { Hono } from "hono";
import { cors } from "hono/cors";
import { z } from "zod";
import { desktop } from "..";

const uploadTypes = z.enum(["live", "gb", "hui"]);

const liveData = z.object({
    type: uploadTypes,
    id: z.string(),
    data: z.object({
        root: z.object({
            id: z.string(),
            parentId: z.string().nullable(),
            name: z.string(),
        }),
        files: z.array(
            z.object({
                id: z.string(),
                fileId: z.string(),
                parentId: z.string().nullable(),
                name: z.string(),
                size: z.number(),
                compAlg: z.enum(["gzip", "zstd"]).nullable(),
                url: z.string(),
            }),
        ),
        dirs: z.array(
            z.object({
                id: z.string(),
                parentId: z.string().nullable(),
                name: z.string(),
            }),
        ),
        totalBytes: z.number(),
    }),
    suggestedName: z.string().optional(),
});

const gbData = z.object({
    type: uploadTypes,
    title: z.string(),
    fileUrl: z.string(),
    previewUrl: z.string().optional().nullable(),
});

const huiData = z.object({
    type: uploadTypes,
    title: z.string(),
    fileUrl: z.string(),
});

export const app = new Hono()
    .use(cors())
    .onError((err, ctx) => {
        desktop.logger.error(err, "HonoServer");
        return ctx.text("Custom Error Message", 500);
    })
    .get("/version", async (ctx) => {
        return ctx.text(appVersion);
    })
    .get("/ping", (ctx) => ctx.text("pong"));

const { injectWebSocket, upgradeWebSocket } = createNodeWebSocket({ app });

app.get(
    "/ws",
    upgradeWebSocket(async (_ctx) => {
        return {
            onMessage: async (event, ws) => {
                if (!event.data) {
                    ws.send("invalid data");
                    return;
                } else if (!isArrayBuffer(event.data)) {
                    ws.send("invalid data");
                    return;
                }

                const decoded = decode(new Uint8Array(event.data));

                if (decoded.type === "live") {
                    const { id, data, suggestedName } = liveData.parse(decoded);
                    await desktop.service.drive.fn.startDownload({
                        id,
                        data,
                        suggestedName,
                    });
                } else if (decoded.type === "gb") {
                    const { title, fileUrl, previewUrl } = gbData.parse(decoded);
                    await desktop.lib.customDownloader.GBDownloader({
                        title,
                        fileUrl,
                        previewUrl,
                    });
                } else if (decoded.type === "hui") {
                    const { title, fileUrl } = huiData.parse(decoded);
                    await desktop.lib.customDownloader.HuiDownloader({ title, fileUrl });
                }

                ws.send("download started");
            },
            onClose: () => {},
        };
    }),
);

export async function startServer() {
    const server = serve({
        fetch: app.fetch,
        port: 1027,
    });

    injectWebSocket(server);
}
