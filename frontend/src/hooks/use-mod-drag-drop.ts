import { Mod } from "@bindings/mod";
import { FS } from "@bindings/platform";
import { Logger } from "@renderer/lib/logger";
import { getSetting } from "@renderer/lib/settings";
import { modStore } from "@renderer/store/mod";
import { FileDropTargetID, useWindowFileDrop } from "@renderer/wails/file-drop";
import type { ArchiveExtractPathMode, ResolvedArchiveExtractPathMode } from "@shared/mod";
import type { QueryClient } from "@tanstack/react-query";
import path from "path-browserify";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const SUPPORTED_ARCHIVE_EXTENSIONS = [".zip", ".rar", ".7z"];
const SUPPORTED_IMAGE_EXTENSIONS = [
    ".png",
    ".jpg",
    ".jpeg",
    ".webp",
    ".avif",
    ".avifs",
    ".gif",
    ".bmp",
];

export function useModDragDrop(
    groupPath: string | undefined,
    queryClient: QueryClient,
    game: string,
) {
    const { t } = useTranslation();
    const [isDragging, setIsDragging] = useState(false);
    const [prevGroupPath, setPrevGroupPath] = useState(groupPath);

    if (groupPath !== prevGroupPath) {
        setPrevGroupPath(groupPath);
        setIsDragging(false);
    }
    const [archiveExtractDialogFileName, setArchiveExtractDialogFileName] = useState<string | null>(
        null,
    );
    const archivePromptQueueRef = useRef<
        {
            fileName: string;
            resolve: (mode: ResolvedArchiveExtractPathMode) => void;
        }[]
    >([]);
    const activeArchivePromptRef = useRef<{
        fileName: string;
        resolve: (mode: ResolvedArchiveExtractPathMode) => void;
    } | null>(null);

    const resolveArchivePrompt = (mode: ResolvedArchiveExtractPathMode) => {
        const activePrompt = activeArchivePromptRef.current;
        if (!activePrompt) {
            return;
        }

        activeArchivePromptRef.current = null;
        setArchiveExtractDialogFileName(null);
        activePrompt.resolve(mode);

        const nextPrompt = archivePromptQueueRef.current.shift();
        if (!nextPrompt) {
            return;
        }

        activeArchivePromptRef.current = nextPrompt;
        setArchiveExtractDialogFileName(nextPrompt.fileName);
    };

    const enqueueArchivePrompt = (fileName: string): Promise<ResolvedArchiveExtractPathMode> => {
        return new Promise((resolve) => {
            const request = { fileName, resolve };

            if (!activeArchivePromptRef.current) {
                activeArchivePromptRef.current = request;
                setArchiveExtractDialogFileName(fileName);
                return;
            }

            archivePromptQueueRef.current.push(request);
        });
    };

    const resolveArchiveExtractMode = async (
        fileName: string,
        filePath: string,
    ): Promise<ResolvedArchiveExtractPathMode> => {
        const mode = (await getSetting("mod.archiveExtractPathMode")) as ArchiveExtractPathMode;

        if (mode === "ask_every_time") {
            const hasSingleTopLevelDirectory = await Mod.HasSingleTopLevelDirectory(filePath);

            if (!hasSingleTopLevelDirectory) {
                return "flatten_single_root";
            }

            return enqueueArchivePrompt(fileName);
        }

        return mode;
    };

    useEffect(() => {
        return () => {
            if (activeArchivePromptRef.current) {
                activeArchivePromptRef.current.resolve("flatten_single_root");
                activeArchivePromptRef.current = null;
            }

            for (const pendingPrompt of archivePromptQueueRef.current) {
                pendingPrompt.resolve("flatten_single_root");
            }

            archivePromptQueueRef.current = [];
        };
    }, []);

    const isArchive = (filePath: string): boolean => {
        const ext = path.extname(filePath).toLowerCase();
        return SUPPORTED_ARCHIVE_EXTENSIONS.includes(ext);
    };

    const isImage = (filePath: string): boolean => {
        const ext = path.extname(filePath).toLowerCase();
        return SUPPORTED_IMAGE_EXTENSIONS.includes(ext);
    };

    const isDirectory = async (filePath: string): Promise<boolean> => {
        try {
            const metadata = await FS.GetPathMetadata(filePath);
            return metadata.isDirectory;
        } catch {
            return false;
        }
    };

    const handleDragEnter = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            if (!groupPath) {
                return;
            }
            setIsDragging(true);
        }
    };

    const handleDragLeave = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            const rect = e.currentTarget.getBoundingClientRect();
            const x = e.clientX;
            const y = e.clientY;

            if (x <= rect.left || x >= rect.right || y <= rect.top || y >= rect.bottom) {
                setIsDragging(false);
            }
        }
    };

    const handleDragOver = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            if (!groupPath) {
                return;
            }
            setIsDragging(true);
        }
    };

    const handlePathsDrop = async (
        paths: string[],
        targetPath: string,
        options: { allowImages?: boolean } = {},
    ) => {
        if (!targetPath) {
            toast.error(t("page.mod.drag_drop.target_path_not_set"));
            return;
        }

        const { allowImages = false } = options;
        const currentSelectedGroup = modStore.getState().selectedGroup;

        for (const filePath of paths) {
            try {
                const fileName = path.basename(filePath);

                const isDir = await isDirectory(filePath);
                const isArch = isArchive(filePath);
                const isImg = isImage(filePath);

                if (!isDir && !isArch && (!allowImages || !isImg)) {
                    const message = allowImages
                        ? t("page.mod.drag_drop.unsupported_file_with_images", {
                              fileName,
                          })
                        : t("page.mod.drag_drop.unsupported_file_archive_only", {
                              fileName,
                          });
                    toast.warning(message);
                    continue;
                }

                if (isArch) {
                    const extractMode = await resolveArchiveExtractMode(fileName, filePath);

                    toast.promise(Mod.ExtractArchiveToGroup(filePath, targetPath, extractMode), {
                        loading: t("page.mod.drag_drop.extract.loading", {
                            fileName,
                        }),
                        success: () => {
                            void queryClient.invalidateQueries({
                                queryKey: ["characters", game],
                            });
                            void queryClient.invalidateQueries({
                                queryKey: ["modGroup", currentSelectedGroup?.path],
                            });
                            return t("page.mod.drag_drop.extract.success", {
                                fileName,
                            });
                        },
                        error: (error) => {
                            Logger.error(error, "ModDragDrop:extractArchive");
                            if (error.message?.includes("ALREADY_EXISTS")) {
                                const folderName = error.message.split(":")[1];
                                return t("page.mod.drag_drop.already_exists_folder", {
                                    folderName,
                                });
                            }
                            return t("page.mod.drag_drop.extract.error", {
                                fileName,
                            });
                        },
                    });
                } else if (isDir || (allowImages && isImg)) {
                    toast.promise(Mod.CopyFolderToGroup(filePath, targetPath, false), {
                        loading: t("page.mod.drag_drop.copy.loading", {
                            fileName,
                        }),
                        success: () => {
                            void queryClient.invalidateQueries({
                                queryKey: ["characters", game],
                            });
                            void queryClient.invalidateQueries({
                                queryKey: ["modGroup", currentSelectedGroup?.path],
                            });
                            return t("page.mod.drag_drop.copy.success", {
                                fileName,
                            });
                        },
                        error: (error) => {
                            Logger.error(error, "ModDragDrop:copyFolder");
                            if (error.message?.includes("ALREADY_EXISTS")) {
                                const folderName = error.message.split(":")[1];
                                return t("page.mod.drag_drop.already_exists_item", {
                                    folderName,
                                });
                            }
                            return t("page.mod.drag_drop.copy.error", {
                                fileName,
                            });
                        },
                    });
                }
            } catch (error) {
                Logger.error(error, "ModDragDrop:handleDrop");
                toast.error(t("page.mod.drag_drop.processing_error"));
            }
        }
    };

    useWindowFileDrop(({ paths, target }) => {
        if (target.id !== FileDropTargetID.modContent || !groupPath) {
            return;
        }
        setIsDragging(false);
        void handlePathsDrop(paths, groupPath, { allowImages: false });
    });

    const handleDrop = (e: React.DragEvent) => {
        if (!e.dataTransfer?.types.includes("Files")) return;
        setIsDragging(false);
    };

    return {
        isDragging,
        handleDragEnter,
        handleDragLeave,
        handleDragOver,
        handleDrop,
        handlePathsDrop,
        archiveExtractDialogFileName,
        confirmArchiveExtractDialog: () => resolveArchivePrompt("flatten_single_root"),
        keepArchiveRootDialog: () => resolveArchivePrompt("keep_archive_root"),
        closeArchiveExtractDialog: () => resolveArchivePrompt("flatten_single_root"),
    };
}
