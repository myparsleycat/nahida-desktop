import { useState } from "react";
import { toast } from "sonner";
import { Logger } from "@renderer/lib/logger";
import { QueryClient } from "@tanstack/react-query";
import path from "path-browserify";

const SUPPORTED_ARCHIVE_EXTENSIONS = [".zip", ".rar", ".7z"];

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

    const isDirectory = async (filePath: string): Promise<boolean> => {
        try {
            return true;
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
    const handleFilesDrop = async (files: File[], targetPath: string) => {
        if (!targetPath) {
            toast.error("대상 경로가 설정되지 않았습니다.");
            return;
        }

        for (const file of files) {
            try {
                const filePath = window.webUtils.getPathForFile(file);

                if (!filePath) {
                    toast.error("파일 경로를 확인할 수 없습니다.");
                    continue;
                }

                if (isArchive(filePath)) {
                    toast.promise(window.api.invoke("mod:extractArchive", filePath, targetPath), {
                        loading: `${file.name} 압축 해제 중...`,
                        success: () => {
                            queryClient.invalidateQueries({ queryKey: ["mods", game] });
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
                    });
                } else {
                    toast.promise(window.api.invoke("mod:copyFolder", filePath, targetPath), {
                        loading: `${file.name} 처리 중...`,
                        success: () => {
                            queryClient.invalidateQueries({ queryKey: ["mods", game] });
                            return `${file.name} 추가 완료`;
                        },
                        error: (error) => {
                            Logger.error(error, "ModDragDrop:copyFolder");
                            if (error.message?.includes("ALREADY_EXISTS")) {
                                const folderName = error.message.split(":")[1];
                                return `이미 존재하는 폴더입니다: ${folderName}`;
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
    };

    const handleDrop = async (e: React.DragEvent) => {
        console.log("handleDrop Triggered");

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

        await handleFilesDrop(files, groupPath);
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
