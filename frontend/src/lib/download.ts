import { Drive } from "@bindings/drive";
import type { Content } from "@shared/types";
import { t } from "i18next";
import { toast } from "sonner";

export async function downloadItems(items: Content[]) {
    // A file the server has not put into object storage yet has nothing to
    // download; the rest of the selection still goes through.
    const stored = items.filter((item) => !item.storing);

    if (stored.length < items.length) {
        toast.info(t("page.drive.storing_download_excluded"));
    }

    if (stored.length === 0) return;

    await Drive.StartDownload({
        items: stored.map((item) => ({
            id: item.id,
            isDir: item.isDir,
            name: item.name,
            size: item.size,
        })),
        source: "drive",
    });
}
