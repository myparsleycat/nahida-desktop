import type { Content } from "@shared/types";

import { invoke } from "./ipc";

export async function downloadItems(items: Content[]) {
    if (items.length === 0) return;

    await invoke("drive:fn:startDownload", {
        items: items.map((item) => ({
            id: item.id,
            isDir: item.isDir,
            name: item.name,
            size: item.size,
        })),
    });
}
