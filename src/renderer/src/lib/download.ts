import type { Content } from "@shared/types";
import { invoke } from "./ipc";

export async function downloadItems(items: Content[]) {
    if (items.length === 0) return;

    const onlyItem = items.length === 1 ? items[0] : null;

    if (onlyItem && onlyItem.isDir) {
        await invoke("drive:fn:startDownload", {
            id: onlyItem.id,
            isDir: true,
            suggestedName: onlyItem.name,
        });
        return;
    }

    if (items.length === 1 && !items[0].isDir) {
        await invoke("drive:fn:startDownload", {
            id: items[0].id,
            isDir: false,
            suggestedName: items[0].name,
        });
        return;
    }

    const result = await invoke("dialog:selectDirectory");
    if (result.canceled || !result.filePath) return;

    await Promise.all(
        items.map((item) =>
            invoke("drive:fn:startDownload", {
                id: item.id,
                isDir: item.isDir,
                suggestedName: item.name,
                targetPath: result.filePath,
            }),
        ),
    );
}
