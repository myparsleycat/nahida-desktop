import { Events } from "@wailsio/runtime";
import { useEffect, useRef } from "react";

export const FileDropTargetID = {
    modContent: "mod-content-file-drop",
    driveContent: "drive-content-file-drop",
    fixToolList: "fix-tool-list-file-drop",
} as const;

export const FILE_DROP_GROUP_PATH_ATTRIBUTE = "data-file-drop-group-path";

export type FileDropTargetDetails = {
    x: number;
    y: number;
    id: string;
    classList: string[];
    attributes: Record<string, string>;
};

export type WindowFileDrop = {
    paths: string[];
    target: FileDropTargetDetails;
};

export type WindowFileDropListener = (drop: WindowFileDrop) => void;

const listeners = new Set<WindowFileDropListener>();

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null;
}

function isString(value: unknown): value is string {
    return typeof value === "string";
}

function parseStringRecord(value: unknown): Record<string, string> | null {
    if (value === undefined) {
        return {};
    }
    if (!isRecord(value)) {
        return null;
    }

    const entries = Object.entries(value);
    if (entries.some(([, item]) => typeof item !== "string")) {
        return null;
    }
    return Object.fromEntries(entries) as Record<string, string>;
}

function parseWindowFileDrop(value: unknown): WindowFileDrop | null {
    if (!isRecord(value) || !Array.isArray(value.paths) || !value.paths.every(isString)) {
        return null;
    }
    if (!isRecord(value.target)) {
        return null;
    }

    const { target } = value;
    const attributes = parseStringRecord(target.attributes);
    if (
        typeof target.x !== "number" ||
        typeof target.y !== "number" ||
        typeof target.id !== "string" ||
        !Array.isArray(target.classList) ||
        !target.classList.every(isString) ||
        attributes === null
    ) {
        return null;
    }

    return {
        paths: value.paths,
        target: {
            x: target.x,
            y: target.y,
            id: target.id,
            classList: target.classList,
            attributes,
        },
    };
}

export function subscribeWindowFileDrops(listener: WindowFileDropListener): () => void {
    listeners.add(listener);
    return () => listeners.delete(listener);
}

export function useWindowFileDrop(listener: WindowFileDropListener): void {
    const listenerRef = useRef(listener);

    useEffect(() => {
        listenerRef.current = listener;
    }, [listener]);

    useEffect(() => {
        return subscribeWindowFileDrops((drop) => listenerRef.current(drop));
    }, []);
}

Events.On("window:files-dropped", (event) => {
    const drop = parseWindowFileDrop(event.data);
    if (!drop) {
        return;
    }

    for (const listener of listeners) {
        listener(drop);
    }
});
