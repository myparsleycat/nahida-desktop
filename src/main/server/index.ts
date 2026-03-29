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

const linkData = z
    .object({
        linkId: z.string(),
        token: z.string(),
    })
    .optional();

const liveData = z.object({
    type: uploadTypes,
    id: z.string(),
    link: linkData,
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
                        const { id, link, suggestedName } = liveData.parse(decoded);

                        const isLoggedIn = await desktop.service.auth.isLoggedIn();
                        if (!link && !isLoggedIn) {
                            ws.send(`unauthorized`);
                            return;
                        }

                        downloadStatus = await desktop.service.drive.fn.startDownload({
                            id,
                            link,
                            suggestedName,
                        });
                    } else if (decoded.type === "gb") {
                        const { title, fileUrl, previewUrl } = gbData.parse(decoded);
                        downloadStatus = await desktop.lib.customDownloader.GBDownloader({
                            title,
                            fileUrl,
                            previewUrl,
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
