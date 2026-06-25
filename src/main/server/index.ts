import { isArrayBuffer } from "node:util/types";
import { serve } from "@hono/node-server";
import { createNodeWebSocket } from "@hono/node-ws";
import { appVersion } from "@main/const";
import { decode } from "cbor-x";
import { Hono } from "hono";
import { cors } from "hono/cors";
import { z } from "zod";
import { desktop } from "..";

const uploadTypes = z.enum(["live", "hui"]);

const linkData = z
    .object({
        linkId: z.string(),
        token: z.string(),
    })
    .optional();

const liveData = z.object({
    type: uploadTypes,
    id: z.string(),
    isDir: z.boolean().default(true),
    link: linkData,
    suggestedName: z.string().optional(),
    data: z
        .object({
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
        })
        .optional(),
});

const huiData = z.object({
    type: uploadTypes,
    title: z.string(),
    fileUrl: z.string(),
});

export type LinkData = z.infer<typeof linkData>;

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
                let downloadStatus: "started" | "canceled" | "error" = "started";

                try {
                    if (decoded.type === "live") {
                        const { id, isDir, link, suggestedName, data } = liveData.parse(decoded);

                        const isLoggedIn = await desktop.service.auth.isLoggedIn();
                        if (!link && !isLoggedIn) {
                            ws.send(`unauthorized`);
                            return;
                        }

                        downloadStatus = await desktop.service.drive.fn.startDownload({
                            id,
                            isDir,
                            link,
                            suggestedName: suggestedName ?? data?.root.name,
                            data,
                        });
                    } else if (decoded.type === "hui") {
                        const { title, fileUrl } = huiData.parse(decoded);
                        downloadStatus = await desktop.lib.customDownloader.HuiDownloader({
                            title,
                            fileUrl,
                        });
                    }
                } catch (err) {
                    downloadStatus = "error";
                    desktop.logger.error(err, "WebSocket:Download");
                }

                ws.send(`download ${downloadStatus}`);
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
