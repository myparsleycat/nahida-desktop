import { useState, useCallback } from "react";
import { toast } from "sonner";
import { Logger } from "@renderer/lib/logger";
import { QueryClient } from "@tanstack/react-query";
import path from "path-browserify";
import { useModStore, modStore } from "@renderer/store/mod";

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
    const [isDragging, setIsDragging] = useState(false);

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
            const metadata = await window.api.invoke("util:fs:metadata", filePath);
            return metadata.isDirectory;
        } catch {
            return false;
        }
    };

    const handleDragEnter = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            e.preventDefault();
            e.stopPropagation();
            setIsDragging(true);
        }
    };

    const handleDragLeave = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            e.preventDefault();
            e.stopPropagation();

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
            e.preventDefault();
            e.stopPropagation();
            setIsDragging(true);
        }
    };

    const handleFilesDrop = useCallback(
        async (files: File[], targetPath: string, options: { allowImages?: boolean } = {}) => {
            if (!targetPath) {
                toast.error("대상 경로가 설정되지 않았습니다.");
                return;
            }

            const { allowImages = false } = options;
            const currentSelectedGroup = modStore.getState().selectedGroup;

            for (const file of files) {
                try {
                    const filePath = window.webUtils.getPathForFile(file);

                    if (!filePath) {
                        toast.error("파일 경로를 확인할 수 없습니다.");
                        continue;
                    }

                    const isDir = await isDirectory(filePath);
                    const isArch = isArchive(filePath);
                    const isImg = isImage(filePath);

                    if (!isDir && !isArch && (!allowImages || !isImg)) {
                        const message = allowImages
                            ? `${file.name}은(는) 지원하는 파일 형식이 아닙니다. (압축 파일, 폴더, 이미지 파일만 가능)`
                            : `${file.name}은(는) 지원하는 파일 형식이 아닙니다. (압축 파일 또는 폴더만 가능)`;
                        toast.warning(message);
                        continue;
                    }

                    if (isArch) {
                        toast.promise(
                            window.api.invoke("mod:extractArchive", filePath, targetPath),
                            {
                                loading: `${file.name} 압축 해제 중...`,
                                success: () => {
                                    queryClient.invalidateQueries({
                                        queryKey: ["characters", game],
                                    });
                                    queryClient.invalidateQueries({
                                        queryKey: ["modGroup", currentSelectedGroup?.path],
                                    });
                                    return `${file.name} 압축 해제 완료`;
                                },
                                error: (error) => {
                                    Logger.error(error, "ModDragDrop:extractArchive");
                                    if (error.message?.includes("ALREADY_EXISTS")) {
                                        const folderName = error.message.split(":")[1];
                                        return `이미 존재하는 폴더입니다: ${folderName}`;
                                    }
                                    return `${file.name} 압축 해제 실패`;
                                },
                            },
                        );
                    } else if (isDir || (allowImages && isImg)) {
                        toast.promise(window.api.invoke("mod:copyFolder", filePath, targetPath), {
                            loading: `${file.name} 처리 중...`,
                            success: () => {
                                queryClient.invalidateQueries({
                                    queryKey: ["characters", game],
                                });
                                queryClient.invalidateQueries({
                                    queryKey: ["modGroup", currentSelectedGroup?.path],
                                });
                                return `${file.name} 추가 완료`;
                            },
                            error: (error) => {
                                Logger.error(error, "ModDragDrop:copyFolder");
                                if (error.message?.includes("ALREADY_EXISTS")) {
                                    const folderName = error.message.split(":")[1];
                                    return `이미 존재하는 항목입니다: ${folderName}`;
                                }
                                return `${file.name} 추가 실패`;
                            },
                        });
                    }
                } catch (error) {
                    Logger.error(error, "ModDragDrop:handleDrop");
                    toast.error("파일 처리 중 오류가 발생했습니다.");
                }
            }
        },
        [queryClient, game],
    );

    const handleDrop = async (e: React.DragEvent) => {
        if (!e.dataTransfer?.types.includes("Files")) return;

        e.preventDefault();
        e.stopPropagation();
        setIsDragging(false);

        if (!groupPath) {
            toast.error("그룹 경로가 설정되지 않았습니다.");
            return;
        }

        const files = Array.from(e.dataTransfer.files);
        if (files.length === 0) {
            return;
        }

        await handleFilesDrop(files, groupPath, { allowImages: false });
    };

    return {
        isDragging,
        handleDragEnter,
        handleDragLeave,
        handleDragOver,
        handleDrop,
        handleFilesDrop,
    };
}
