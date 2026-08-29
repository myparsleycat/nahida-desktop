import type { TitlebarActivity } from "@renderer/store/titlebar-activity";
import { isTerminalFixerProgressCode } from "@shared/4001-fixer";
import { getAggregateTransferProgress, isOpenTransferQueueStatus } from "@shared/transfer-progress";
import type {
    BisectSnapshot,
    FourThousandOneFixerProgressEvent,
    TextureResizeProgressEvent,
    TransferWithoutData,
} from "@shared/types";
import { formatSize } from "@shared/utils";
import { ArrowUpDownIcon, GitCompareIcon, ScalingIcon, WrenchIcon } from "lucide-react";

type Translate = {
    (key: string): string;
    (key: string, defaultValue: string): string;
};

type FixerTask = NonNullable<FourThousandOneFixerProgressEvent["task"]>;

function getActiveTransfers(transfers: TransferWithoutData[]) {
    return transfers.filter(
        (transfer) => isOpenTransferQueueStatus(transfer.status) || transfer.status === "paused",
    );
}

function isFinalizingTransfer(transfer: TransferWithoutData) {
    return isOpenTransferQueueStatus(transfer.status) && transfer.progress >= 100;
}

export function buildTransferTitlebarActivity(
    transfers: TransferWithoutData[],
    t: Translate,
): TitlebarActivity | null {
    const activeTransfers = getActiveTransfers(transfers);
    if (activeTransfers.length === 0) return null;

    const activeDownloads = activeTransfers.filter((transfer) => transfer.type === "download");
    const activeUploads = activeTransfers.filter((transfer) => transfer.type === "upload");
    const progressingTransfers = activeTransfers.filter((transfer) =>
        isOpenTransferQueueStatus(transfer.status),
    );
    const allFinalizing =
        progressingTransfers.length > 0 && progressingTransfers.every(isFinalizingTransfer);
    const allPaused = activeTransfers.every((transfer) => transfer.status === "paused");
    const label = allPaused
        ? t("titlebar.activity.transfer.paused")
        : allFinalizing
          ? t("titlebar.activity.transfer.finalizing")
          : activeDownloads.length > 0 && activeUploads.length === 0
            ? t("titlebar.activity.transfer.downloading")
            : activeUploads.length > 0 && activeDownloads.length === 0
              ? t("titlebar.activity.transfer.uploading")
              : t("titlebar.activity.transfer.transferring");

    const speed = activeTransfers
        .filter((transfer) => transfer.status === "progress" && !isFinalizingTransfer(transfer))
        .reduce((sum, transfer) => sum + transfer.speed, 0);
    const progress = getAggregateTransferProgress(transfers);
    const detailParts = [
        speed > 0 ? `${formatSize(speed)}/s` : null,
        progress !== null ? `${Math.round(progress)}%` : null,
    ].filter((part): part is string => part !== null);

    return {
        id: "transfer",
        label,
        status: allPaused ? "paused" : "running",
        icon: ArrowUpDownIcon,
        detail: detailParts.length > 0 ? detailParts.join(" · ") : undefined,
        progress: progress ?? undefined,
        order: 0,
        href: "/transfer",
    };
}

function getFixerTaskLabelKey(task: FixerTask) {
    switch (task) {
        case "build-dll":
            return "titlebar.activity.fixer4001.building";
        case "diversify-dll":
            return "titlebar.activity.fixer4001.diversifying";
        case "restore-dll":
            return "titlebar.activity.fixer4001.restoring";
    }
}

function resolveFixerStageTooltip(code: string, t: Translate) {
    if (!code) return undefined;
    if (code.startsWith("page.tools.")) return t(code, code);
    if (code.startsWith("XXMI_")) return t(`page.tools.4001_fixer.progress.${code}`, code);
    return code;
}

export function build4001FixerTitlebarActivity(
    task: FixerTask | null,
    code: string,
    t: Translate,
): TitlebarActivity | null {
    if (!task || isTerminalFixerProgressCode(code)) return null;

    return {
        id: "tools:4001-fixer",
        label: t(getFixerTaskLabelKey(task)),
        status: "running",
        icon: WrenchIcon,
        tooltip: resolveFixerStageTooltip(code, t),
        order: 10,
        href: "/tools",
    };
}

function isActiveBisectStatus(status: BisectSnapshot["status"]) {
    return status === "scanning" || status === "round" || status === "reverting";
}

export function buildModBisectTitlebarActivity(
    snapshot: BisectSnapshot | null,
    t: Translate,
): TitlebarActivity | null {
    if (!snapshot || !isActiveBisectStatus(snapshot.status)) return null;

    return {
        id: "tools:mod-bisect",
        label: t("titlebar.activity.modBisect.running"),
        status: "running",
        icon: GitCompareIcon,
        order: 20,
        href: "/tools",
    };
}

function getTextureResizerLabelKey(operation?: TextureResizeProgressEvent["operation"]) {
    switch (operation) {
        case "upscale":
            return "titlebar.activity.textureResizer.upscaling";
        case "upscale_and_convert":
            return "titlebar.activity.textureResizer.upscaling_and_converting";
        case "convert":
            return "titlebar.activity.textureResizer.converting";
        case "resize_and_convert":
            return "titlebar.activity.textureResizer.resizing_and_converting";
        case "resize":
        default:
            return "titlebar.activity.textureResizer.resizing";
    }
}

export function buildTextureResizerTitlebarActivity(
    event: TextureResizeProgressEvent | null,
    t: Translate,
): TitlebarActivity | null {
    if (!event || event.status !== "running") return null;

    const detail = event.fileName;

    return {
        id: "tools:texture-resizer",
        label: t(getTextureResizerLabelKey(event.operation)),
        status: "running",
        icon: ScalingIcon,
        detail: detail || undefined,
        order: 30,
        href: "/tools",
    };
}
