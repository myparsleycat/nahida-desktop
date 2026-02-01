import { net, protocol } from "electron";
import { pathToFileURL } from "node:url";

export function registerLocalProtocal() {
    protocol.handle("local", async (request) => {
        const url = new URL(request.url);

        let fullPath = decodeURIComponent(url.pathname);

        if (url.host) {
            fullPath = url.host + ":" + fullPath;
        }

        if (fullPath.startsWith("/")) {
            fullPath = fullPath.slice(1);
        }

        const fileUrl = pathToFileURL(fullPath).href;

        try {
            const response = await net.fetch(fileUrl);
            return response;
        } catch (error) {
            return new Response("not found", { status: 404 });
        }
    });
}
