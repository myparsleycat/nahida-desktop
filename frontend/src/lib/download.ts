import { Drive } from "@bindings/drive";
import type { Content } from "@shared/types";

export async function downloadItems(items: Content[]) {
    if (items.length === 0) return;

    await Drive.StartDownload({
        items: items.map((item) => ({
            id: item.id,
            isDir: item.isDir,
            name: item.name,
            size: item.size,
        })),
        source: "drive",
    });
}
