import {
  ImageIcon,
  FolderIcon,
  FileCogIcon,
  TrashIcon,
  TerminalSquareIcon,
  ClipboardIcon,
} from "lucide-react";
import { Preview } from "./preview";
import { Input } from "@renderer/components/ui/input";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@renderer/components/ui/alert-dialog";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@renderer/components/ui/context-menu";
import type { ModInfo } from "@renderer/types/mod";
import { cn } from "@renderer/lib/utils";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Separator } from "@renderer/components/ui/separator";
import { Button } from "@renderer/components/ui/button";
import { useRouteContext } from "@tanstack/react-router";
import { useModStore } from "@renderer/store/mod";
import { toast } from "sonner";

interface ModCardProps {
  mod: ModInfo;
  onToggle: (mod: ModInfo) => void;
  onToggleKeyUpdate: (
    modPath: string,
    iniFileName: string,
    sectionName: string,
    variable: string,
    value: string,
  ) => void;
}

const getModColorClass = (isEnabled: boolean) => {
  if (isEnabled) {
    return "dark:bg-[#0d430d] bg-[#048117]";
  } else {
    return "dark:bg-[#58151b] bg-[#af2938]";
  }
};

const getToggleBoxColorClass = (isEnabled: boolean) => {
  if (isEnabled) {
    return "dark:bg-[#0f4d0f] bg-[#008a1c]";
  } else {
    return "dark:bg-[#63181e] bg-[#781d26]";
  }
};

const getToggleInputColorClass = (isEnabled: boolean) => {
  if (isEnabled) {
    return "dark:bg-[#115a11] bg-[#00941e]";
  } else {
    return "dark:bg-[#731c23] bg-[#781d26]";
  }
};

export function ModCard({ mod, onToggle, onToggleKeyUpdate }: ModCardProps) {
  const { queryClient } = useRouteContext({ from: "/mod/" });
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const selectedGroupPath = selectedGroup?.path;

  const handlePaste = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      const files = await window.api.invoke("util:getClipboardFiles");
      if (files.length > 0) {
        const filePath = files[0];
        if (filePath.match(/\.(jpeg|jpg|gif|png|webp|bmp)$/i)) {
          const promise = window.api.invoke("mod:pastePreview", mod.path, filePath, "path");
          toast.promise(promise, {
            loading: "Copying preview...",
            success: "Preview updated",
            error: "Failed to copy preview",
          });
          promise.then(() => {
            queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
          });
          return;
        }
      }

      const text = await navigator.clipboard.readText();
      if (text && text.startsWith("http")) {
        if (text.match(/\.(jpeg|jpg|gif|png|webp|bmp)$/i)) {
          const promise = window.api.invoke("mod:pastePreview", mod.path, text, "url");
          toast.promise(promise, {
            loading: "Downloading preview...",
            success: "Preview updated",
            error: "Failed to download preview",
          });
          promise.then(() => {
            queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
          });
          return;
        }
      }

      const items = await navigator.clipboard.read();
      for (const item of items) {
        if (item.types.includes("image/png") || item.types.includes("image/jpeg")) {
          const blob = await item.getType(item.types.find((t) => t.startsWith("image/"))!);
          const reader = new FileReader();
          reader.onloadend = () => {
            const base64data = reader.result as string;
            const promise = window.api.invoke("mod:pastePreview", mod.path, base64data, "base64");
            toast.promise(promise, {
              loading: "Saving preview...",
              success: "Preview updated",
              error: "Failed to save preview",
            });
            promise.then(() => {
              queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
            });
          };
          reader.readAsDataURL(blob);
          return;
        }
      }

      toast.warning("클립보드에 이미지, 이미지 URL, 또는 이미지 파일이 없습니다.");
    } catch (error) {
      console.error(error);
      toast.error("클립보드를 읽는데 실패했습니다.");
    }
  };

  const handleDelete = (e: React.MouseEvent) => {
    e.stopPropagation();
    const promise = window.api.invoke("util:fs:trash", mod.path);
    toast.promise(promise, {
      loading: "휴지통으로 이동 중...",
      success: "삭제 완료",
      error: "삭제 실패",
    });
    promise.then(() => {
      queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
    });
  };

  return (
    <div
      className={cn(
        "rounded-sm overflow-hidden border cursor-pointer shadow-lg p-1 h-[400px]",
        getModColorClass(mod.isEnabled),
      )}
      onClick={() => onToggle(mod)}
      draggable={false}
    >
      <div className="flex items-center justify-between pb-1">
        <span className="text-sm truncate font-semibold">
          {mod.name.replace(/disabled/gi, "").trim()}
        </span>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            className="size-7 hover:bg-accent/20"
            onClick={(e) => {
              e.stopPropagation();
              window.api.invoke("util:openCmd", mod.path);
            }}
          >
            <TerminalSquareIcon />
          </Button>

          <AlertDialog>
            <AlertDialogTrigger asChild onClick={(e) => e.stopPropagation()}>
              <Button variant="ghost" size="icon" className="size-7 hover:bg-accent/20">
                <TrashIcon />
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent onClick={(e) => e.stopPropagation()}>
              <AlertDialogHeader>
                <AlertDialogTitle>이 모드를 삭제할까요?</AlertDialogTitle>
                <AlertDialogDescription>
                  삭제된 모드는 휴지통에서 복원할 수 있어요
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>취소</AlertDialogCancel>
                <AlertDialogAction onClick={handleDelete}>삭제</AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>

          <Button
            variant="ghost"
            size="icon"
            className="size-7 hover:bg-accent/20"
            onClick={(e) => {
              e.stopPropagation();
              window.api.invoke("util:openPath", mod.path);
            }}
          >
            <FolderIcon />
          </Button>
        </div>
      </div>

      <div className="flex flex-row h-[calc(100%-2.5rem)] space-x-1.5">
        <ModPreview mod={mod} onPaste={handlePaste} />

        {mod.inis.length > 0 && (
          <>
            <Separator orientation="vertical" />
            <ModIniList mod={mod} onToggleKeyUpdate={onToggleKeyUpdate} />
          </>
        )}
      </div>
    </div>
  );
}

function ModPreview({ mod, onPaste }: { mod: ModInfo; onPaste: (e: React.MouseEvent) => void }) {
  const { queryClient } = useRouteContext({ from: "/mod/" });
  const selectedGroup = useModStore((s) => s.selectedGroup);

  const previewContent = (
    <Preview
      path={mod.preview}
      alt={mod.name}
      objectFit="contain"
      className="absolute inset-0"
      fallback={
        <div className="flex flex-col items-center justify-center gap-2">
          <ImageIcon className="w-12 h-12 text-muted-foreground/50" />
          <div className="flex flex-col items-center gap-1">
            <span className="text-sm text-muted-foreground">No Preview</span>
            <Button variant="outline" size="sm" className="h-7 text-xs gap-1" onClick={onPaste}>
              <ClipboardIcon className="w-3 h-3" />
              Paste
            </Button>
          </div>
        </div>
      }
    />
  );

  const handleDelete = () => {
    if (!mod.preview) return;
    const promise = window.api.invoke("util:fs:trash", mod.preview);
    toast.promise(promise, {
      loading: "휴지통으로 이동 중...",
      success: "삭제 완료",
      error: "삭제 실패",
    });
    promise.then(() => {
      queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroup?.path] });
    });
  };

  return (
    <div className="flex-1 p-2 flex items-center justify-center relative overflow-hidden">
      {mod.preview ? (
        <ContextMenu>
          <ContextMenuTrigger>{previewContent}</ContextMenuTrigger>
          <ContextMenuContent onClick={(e) => e.stopPropagation()}>
            <ContextMenuItem onClick={() => window.api.invoke("util:openExternal", mod.preview!)}>
              <ImageIcon />
              뷰어로 열기
            </ContextMenuItem>
            <ContextMenuItem onClick={handleDelete}>
              <TrashIcon />
              프리뷰 삭제
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ) : (
        previewContent
      )}
    </div>
  );
}

function ModIniList({
  mod,
  onToggleKeyUpdate,
}: {
  mod: ModInfo;
  onToggleKeyUpdate: ModCardProps["onToggleKeyUpdate"];
}) {
  return (
    <ScrollArea className="w-[160px] flex flex-col gap-2 overflow-y-auto">
      <div
        className={cn("p-1.5 rounded space-y-2 w-[160px]", getToggleBoxColorClass(mod.isEnabled))}
      >
        {mod.inis.map((ini, iniIdx) => {
          const iniToggleKeys = mod.toggleKeys.filter((tk) => tk.iniFileName === ini.name);

          return (
            <div key={iniIdx} className="space-y-1">
              <div className="flex items-center justify-between gap-1">
                <p className="text-xs truncate opacity-80 whitespace-normal" title={ini.name}>
                  {ini.name}
                </p>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-7 shrink-0"
                  onClick={(e) => {
                    e.stopPropagation();
                    window.api.invoke("util:openPath", ini.path);
                  }}
                >
                  <FileCogIcon />
                </Button>
              </div>

              {iniToggleKeys.length > 0 && (
                <div className="space-y-1 pt-1">
                  {iniToggleKeys.map((toggleKey, idx) => (
                    <div key={idx} className="space-y-1">
                      <span className="text-sm">{toggleKey.sectionName}</span>
                      {toggleKey.key && (
                        <div className="flex items-center gap-1">
                          <span className="text-sm">key:</span>
                          <Input
                            key={`key-${toggleKey.sectionName}-${toggleKey.key}`}
                            className={cn(
                              "h-7 text-sm border-white/30",
                              getToggleInputColorClass(mod.isEnabled),
                            )}
                            defaultValue={toggleKey.key}
                            onClick={(e) => e.stopPropagation()}
                            onBlur={(e) => {
                              const newValue = e.target.value;
                              if (newValue !== toggleKey.key) {
                                onToggleKeyUpdate(
                                  mod.path,
                                  toggleKey.iniFileName,
                                  toggleKey.sectionName,
                                  "key",
                                  newValue,
                                );
                              }
                            }}
                          />
                        </div>
                      )}
                      {toggleKey.back && (
                        <div className="flex items-center gap-1">
                          <span className="text-sm">back:</span>
                          <Input
                            key={`back-${toggleKey.sectionName}-${toggleKey.back}`}
                            className={cn("h-6 text-sm", getToggleInputColorClass(mod.isEnabled))}
                            defaultValue={toggleKey.back}
                            onClick={(e) => e.stopPropagation()}
                            onBlur={(e) => {
                              const newValue = e.target.value;
                              if (newValue !== toggleKey.back) {
                                onToggleKeyUpdate(
                                  mod.path,
                                  toggleKey.iniFileName,
                                  toggleKey.sectionName,
                                  "back",
                                  newValue,
                                );
                              }
                            }}
                          />
                        </div>
                      )}
                      {toggleKey.variable && (
                        <div className="flex items-center gap-1">
                          <span className="text-sm">variable:</span>
                          <span className="text-sm">{toggleKey.values.length}</span>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </ScrollArea>
  );
}
