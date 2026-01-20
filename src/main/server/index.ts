import { Hono } from "hono";
import { cors } from "hono/cors";
import { zValidator } from "@hono/zod-validator";
import { decode } from "cbor-x";
import { desktop } from "..";
import { z } from "zod";

const downloadFromLiveModFormSchema = z.object({
    id: z.string(),
    data: z.file(),
    suggestedName: z.string().optional(),
});

const downloadMetadataSchema = z.object({
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
});

const downloadFromGBFormSchema = z.object({
    title: z.string(),
    fileUrl: z.string(),
    previewUrl: z.string().optional().nullable(),
});

const downloadFromHuiFormSchema = z.object({
    title: z.string(),
    fileUrl: z.string(),
});

export const app = new Hono()
    .use(cors())
    .onError((err, ctx) => {
        desktop.logger.error(err, "HonoServer");
        return ctx.text("Custom Error Message", 500);
    })
    .post(
        "/download-from-live-mod",
        zValidator("form", downloadFromLiveModFormSchema),
        async (ctx) => {
            const { id, data, suggestedName } = ctx.req.valid("form");

            const arrbuf = await data.arrayBuffer();
            const decoded = decode(new Uint8Array(arrbuf));

            const metadata = downloadMetadataSchema.parse(decoded);
            await desktop.service.drive.fn.startDownload({ id, data: metadata, suggestedName });

            return ctx.newResponse(null, 204);
        },
    )
    .post("/download-from-gb", zValidator("json", downloadFromGBFormSchema), async (ctx) => {
        const { title, fileUrl, previewUrl } = ctx.req.valid("json");
        desktop.lib.customDownloader.GBDownloader({ title, fileUrl, previewUrl });
        return ctx.newResponse(null, 204);
    })
    .post("/download-from-hui", zValidator("json", downloadFromHuiFormSchema), async (ctx) => {
        const { title, fileUrl } = ctx.req.valid("json");
        desktop.lib.customDownloader.HuiDownloader({ title, fileUrl });
        return ctx.newResponse(null, 204);
    })
    .get("/ping", (ctx) => ctx.text("pong"));
