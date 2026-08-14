import { describe, expect, it, vi } from "vitest";

import type { FilesComponent } from "./upload";

import { UploadLib, hasSystemFileSegment, isSystemFile } from "./upload";

vi.mock("@native/fs", () => ({
    collectFiles: vi.fn(),
}));

vi.mock("@main/worker/drive/sha256-piscina.worker?modulePath", () => ({
    default: "mock-worker",
}));

vi.mock("fs-extra", () => ({
    default: {
        realpath: vi.fn(async (p: string) => p),
        stat: vi.fn(async (p: string) => ({
            isDirectory: () => p === "C:/folder",
            isFile: () => p !== "C:/folder",
            size: 10,
        })),
    },
}));

const { collectFiles } = await import("@native/fs");

function file(path: string, name: string): FilesComponent {
    return {
        FID: "fid",
        path,
        name,
        size: 10,
        parentPath: path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "",
        fullPath: `C:/${path}`,
    };
}

describe("isSystemFile", () => {
    it("matches macOS system files", () => {
        for (const name of [
            ".DS_Store",
            ".AppleDouble",
            ".Spotlight-V100",
            ".Trashes",
            ".fseventsd",
            ".TemporaryItems",
            ".apdisk",
            "__MACOSX",
            "._texture.png",
            "~",
        ]) {
            expect(isSystemFile(name)).toBe(true);
        }
    });

    it("matches Windows system files case-insensitively", () => {
        for (const name of [
            "Thumbs.db",
            "thumbs.db",
            "desktop.ini",
            "DESKTOP.INI",
            "ehthumbs.db",
            "ehthumbs_video.db",
        ]) {
            expect(isSystemFile(name)).toBe(true);
        }
    });

    it("does not match ordinary files", () => {
        for (const name of ["texture.png", "mod.ini", "Thumbs.db.txt", "notahidden.txt"]) {
            expect(isSystemFile(name)).toBe(false);
        }
    });
});

describe("hasSystemFileSegment", () => {
    it("matches paths containing system file segments", () => {
        for (const path of ["__MACOSX/texture.png", "mods/desktop.ini", "a/.DS_Store"]) {
            expect(hasSystemFileSegment(path)).toBe(true);
        }
    });

    it("does not match ordinary paths", () => {
        expect(hasSystemFileSegment("mods/my/texture.png")).toBe(false);
    });
});

describe("UploadLib.collect", () => {
    it("excludes system files from collected files and directories", async () => {
        vi.mocked(collectFiles).mockResolvedValue({
            files: [
                file("__MACOSX/texture.png", "texture.png"),
                file("mods/desktop.ini", "desktop.ini"),
                file("mods/real.png", "real.png"),
            ],
            directories: [
                { path: "__MACOSX", name: "__MACOSX", parentPath: "" },
                { path: "mods", name: "mods", parentPath: "" },
            ],
        });

        const lib = new UploadLib({} as never);
        const result = await lib.prepareUpload(["C:/folder"], []);

        expect(result.files.map((f) => f.path)).toEqual(["mods/real.png"]);
        expect(result.directories.map((d) => d.path)).toEqual(["mods"]);
    });
});
