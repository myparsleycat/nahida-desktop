import {
  HelpCircle,
  ImageIcon,
  Trash2,
  Copy,
  FileText,
  FolderIcon,
  FileCogIcon,
  TrashIcon,
  TerminalIcon,
  TerminalSquareIcon,
} from "lucide-react";
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
} from "../ui/alert-dialog";
import type { ModInfo } from "@renderer/types/mod";
import { cn } from "@renderer/lib/utils";
import { ScrollArea } from "../ui/scroll-area";
import { Separator } from "../ui/separator";
import { Button } from "../ui/button";
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

export function ModCard({ mod, onToggle, onToggleKeyUpdate }: ModCardProps) {
  const { queryClient } = useRouteContext({ from: "/mod/" });
  const selectedGame = useModStore((s) => s.selectedGame);

  const getModColorClass = () => {
    if (mod.isEnabled) {
      return "dark:bg-[#0d430d] bg-[#048117]";
    } else {
      return "dark:bg-[#58151b] bg-[#af2938]";
    }
  };

  const getToggleInputColorClass = () => {
    if (mod.isEnabled) {
      return "dark:bg-[#0a3a0c] bg-[#007317]";
    } else {
      return "dark:bg-[#4d1319] bg-[#781d26]";
    }
  };

  return (
    <div
      className={cn(
        "rounded-sm overflow-hidden border cursor-pointer shadow-lg p-1 h-[400px]",
        getModColorClass(),
      )}
      onClick={() => onToggle(mod)}
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
                <AlertDialogAction
                  onClick={(e) => {
                    e.stopPropagation();
                    const promise = window.api.invoke("util:fs:trash", mod.path);
                    toast.promise(promise, {
                      loading: "휴지통으로 이동 중...",
                      success: "삭제 완료",
                      error: "삭제 실패",
                    });
                    promise.then(() => {
                      queryClient.invalidateQueries({ queryKey: ["mods", selectedGame] });
                    });
                  }}
                >
                  삭제
                </AlertDialogAction>
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
        <div className="flex-1 p-2 flex items-center justify-center relative overflow-hidden">
          {mod.preview ? (
            <>
              {mod.preview.toLowerCase().match(/\.(mp4|webm|avi|mkv|mov)$/) ? (
                <video
                  src={`local://${mod.preview}`}
                  className="absolute inset-0 w-full h-full object-contain"
                  autoPlay
                  loop
                  muted
                  playsInline
                />
              ) : (
                <img
                  src={`local://${mod.preview}`}
                  alt={mod.name}
                  className="absolute inset-0 w-full h-full object-contain"
                  loading="lazy"
                  decoding="async"
                />
              )}
            </>
          ) : (
            <div className="flex flex-col items-center justify-center gap-2">
              <ImageIcon className="w-12 h-12" />
              <span className="text-sm">No Preview</span>
            </div>
          )}
        </div>

        {mod.toggleKeys.length > 0 && (
          <>
            <Separator orientation="vertical" />
            <ScrollArea className="w-[160px] flex flex-col gap-2 overflow-y-auto">
              <div className={cn("p-1.5 rounded", mod.isEnabled ? "bg-[#194d19]" : "bg-[#612127]")}>
                {mod.ini && (
                  <div className="flex items-center justify-between mb-1 gap-1">
                    <span className="text-xs truncate opacity-80 flex-1" title={mod.ini.name}>
                      {mod.ini.name}
                    </span>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-7 shrink-0"
                      onClick={(e) => {
                        e.stopPropagation();
                        window.api.invoke("util:openPath", mod.ini!.path);
                      }}
                    >
                      <FileCogIcon />
                    </Button>
                  </div>
                )}
                {mod.toggleKeys.map((toggleKey, idx) => (
                  <div key={idx} className="space-y-1">
                    <span className="text-sm">{toggleKey.sectionName}</span>
                    {toggleKey.key && (
                      <div className="flex items-center gap-1">
                        <span className="text-sm">key:</span>
                        <Input
                          key={`key-${toggleKey.sectionName}-${toggleKey.key}`}
                          className={cn("h-7 text-sm", getToggleInputColorClass())}
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
                          className={cn("h-6 text-sm", getToggleInputColorClass())}
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
            </ScrollArea>
          </>
        )}
      </div>
    </div>
  );
}
