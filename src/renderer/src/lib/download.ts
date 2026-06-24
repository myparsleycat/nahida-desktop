import type { Content } from "@shared/types";
import path from "path-browserify";
import { invoke } from "./ipc";

export async function downloadItems(items: Content[]) {
    if (items.length === 0) return;

    const onlyItem = items.length === 1 ? items[0] : null;

    if (onlyItem && onlyItem.isDir) {
        await invoke("drive:fn:startDownload", {
            id: onlyItem.id,
            suggestedName: onlyItem.name,
        });
        return;
    }

    if (items.length === 1 && !items[0].isDir) {
        const result = await invoke("dialog:saveFile", { suggestedName: items[0].name });
        if (result.canceled || !result.filePath) return;

        await invoke("drive:fn:startDownload", {
            id: items[0].id,
            suggestedName: path.basename(result.filePath),
            targetPath: path.dirname(result.filePath),
        });
        return;
    }

    const result = await invoke("dialog:selectDirectory");
    if (result.canceled || !result.filePath) return;

    await Promise.all(
        items.map((item) =>
            invoke("drive:fn:startDownload", {
                id: item.id,
                suggestedName: item.name,
                targetPath: result.filePath,
            }),
        ),
    );
}
