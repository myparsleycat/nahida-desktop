import { Tools } from "@bindings/tools";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { FileDropTargetID, useWindowFileDrop } from "@renderer/wails/file-drop";
import { getFixToolScripts } from "@renderer/wails/fix-tools";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { sortBy } from "es-toolkit";
import { Reorder } from "framer-motion";
import { Plus, Search, TrashIcon, Upload } from "lucide-react";
import path from "path-browserify";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import execIcon from "@/renderer/assets/img/document-executable-svgrepo-com.svg";
import pythonIcon from "@/renderer/assets/img/python-svgrepo-com.svg";

import { Script } from "../tools/fix-tool-manger";

type FixToolListProps = {
  insertedPresetTools: Script[];
  onAddScript: (script: Script) => void;
};

export function FixToolList({ insertedPresetTools, onAddScript }: FixToolListProps) {
  const { t } = useTranslation();
  const [searchQuery, setSearchQuery] = useState("");
  const [isDragOver, setIsDragOver] = useState(false);
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: ["scripts"],
    queryFn: async () => {
      const scripts = await getFixToolScripts();
      return sortBy(scripts, ["name"]);
    },
  });

  const filteredScripts = query.data?.filter(
    (script) =>
      script.name.toLowerCase().includes(searchQuery.toLowerCase()) &&
      !insertedPresetTools.some((t) => t.id === script.id),
  );

  const handleDragEnter = (e: React.DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) {
      setIsDragOver(true);
    }
  };

  const handleDragLeave = (e: React.DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) {
      const rect = e.currentTarget.getBoundingClientRect();
      const x = e.clientX;
      const y = e.clientY;

      if (x <= rect.left || x >= rect.right || y <= rect.top || y >= rect.bottom) {
        setIsDragOver(false);
      }
    }
  };

  const handleDragOver = (e: React.DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) {
      setIsDragOver(true);
    }
  };

  useWindowFileDrop(({ paths, target }) => {
    if (target.id !== FileDropTargetID.fixToolList) {
      return;
    }
    setIsDragOver(false);

    const savePromises = paths.map(async (filePath) => {
      const fileName = path.basename(filePath);

      if (!fileName.endsWith(".py") && !fileName.endsWith(".exe")) {
        return;
      }

      try {
        await Tools.SaveScript(filePath);
      } catch (error) {
        console.error(`Failed to save script: ${filePath}`, error);
        throw error;
      }
    });

    void Promise.allSettled(savePromises).then(() => {
      void queryClient.invalidateQueries({ queryKey: ["scripts"] });
    });
  });

  const handleDrop = (e: React.DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) {
      setIsDragOver(false);
    }
  };

  return (
    <div
      id={FileDropTargetID.fixToolList}
      data-file-drop-target
      onDragOver={handleDragOver}
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      className={`flex h-full min-h-0 flex-col overflow-hidden rounded-lg border bg-card transition-all duration-200 ${
        isDragOver ? "border-primary bg-primary/5 ring-2 ring-primary/20" : ""
      }`}
    >
      <div className="flex flex-col gap-2 border-b p-3">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-base font-semibold">
              {t("page.tools.fix-tool-manager.builder.left.title")}
            </p>
            <p className="text-sm text-muted-foreground">
              {t("page.tools.fix-tool-manager.builder.left.description")}
            </p>
          </div>
          {isDragOver && (
            <Badge className="animate-pulse">
              <Upload className="mr-1 h-3 w-3" />
              {t("page.tools.fix-tool-manager.builder.left.dropToAdd")}
            </Badge>
          )}
        </div>

        <div className="relative shrink-0">
          <Search className="absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search scripts..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-9"
          />
        </div>
      </div>

      <ScrollArea className="flex-1 overflow-hidden">
        <div className="flex flex-col space-y-2 p-3">
          {!filteredScripts || filteredScripts.length === 0 ? (
            <div
              className={`rounded-lg border-2 border-dashed p-12 text-center transition-colors ${
                isDragOver ? "border-primary bg-primary/10" : "border-border"
              }`}
            >
              <p className="text-sm font-medium text-muted-foreground">
                {t("page.tools.fix-tool-manager.builder.left.noScripts.title")}
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                {t("page.tools.fix-tool-manager.builder.left.noScripts.description")}
              </p>
            </div>
          ) : (
            <Reorder.Group
              axis="y"
              values={filteredScripts}
              className="flex flex-col space-y-2"
              onReorder={() => {}}
            >
              {filteredScripts.map((script) => (
                <Reorder.Item
                  key={script.id}
                  value={script}
                  className="group grid grid-cols-[1fr_auto_auto] items-center gap-2 rounded-lg border border-border bg-card p-3 transition-colors hover:border-accent/40 hover:bg-card/80"
                >
                  <div className="flex min-w-0 flex-row items-center space-x-2">
                    {script.type === "python" ? (
                      <img src={pythonIcon} alt="python" className="h-6 w-6" />
                    ) : (
                      <img src={execIcon} alt="python" className="h-6 w-6 dark:invert" />
                    )}

                    <Tooltip disableHoverablePopup>
                      <TooltipTrigger className="min-w-0 truncate text-sm font-medium text-foreground">
                        {script.name}
                      </TooltipTrigger>
                      <TooltipContent>
                        <p className="text-wrap break-all">{script.name}</p>
                      </TooltipContent>
                    </Tooltip>
                  </div>

                  <Button size="icon" variant="outline" onClick={() => onAddScript(script)}>
                    <Plus className="h-4 w-4" />
                  </Button>

                  <Button
                    size="icon"
                    variant="destructive"
                    onClick={() =>
                      Tools.DeleteScript(script.id)
                        .then(() => queryClient.invalidateQueries({ queryKey: ["scripts"] }))
                        .catch((error) => {
                          toast.error(error.message);
                        })
                    }
                  >
                    <TrashIcon className="h-4 w-4" />
                  </Button>
                </Reorder.Item>
              ))}
            </Reorder.Group>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
