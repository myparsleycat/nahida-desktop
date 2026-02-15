import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import type { FixTool } from "@renderer/routes/tools/fix-tool-manger";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { sortBy } from "es-toolkit";
import { Reorder } from "framer-motion";
import { Plus, Search, TrashIcon, Upload } from "lucide-react";
import path from "path-browserify";
import { useState } from "react";
import { toast } from "sonner";
import execIcon from "@/renderer/assets/img/document-executable-svgrepo-com.svg";
import pythonIcon from "@/renderer/assets/img/python-svgrepo-com.svg";

type FixToolListProps = {
  insertedPresetTools: FixTool[];
  onAddTool: (tool: FixTool) => void;
};

export function FixToolList({ insertedPresetTools, onAddTool }: FixToolListProps) {
  const [searchQuery, setSearchQuery] = useState("");
  const [isDragOver, setIsDragOver] = useState(false);
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: ["fix-tools"],
    queryFn: async () => {
      const tools = await window.api.invoke("ftm:getTools");
      return sortBy(tools, ["name"]);
    },
  });

  const filteredTools = query.data?.filter(
    (tool) =>
      tool.name.toLowerCase().includes(searchQuery.toLowerCase()) &&
      !insertedPresetTools.some((t) => t.id === tool.id),
  );

  const handleDragEnter = (e: React.DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) {
      e.preventDefault();
      e.stopPropagation();
      setIsDragOver(true);
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
        setIsDragOver(false);
      }
    }
  };

  const handleDragOver = (e: React.DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) {
      e.preventDefault();
      e.stopPropagation();
      setIsDragOver(true);
    }
  };

  const handleDrop = async (e: React.DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) {
      e.preventDefault();
      e.stopPropagation();
      setIsDragOver(false);

      const files = Array.from(e.dataTransfer.files);
      if (files.length > 0) {
        const savePromises = files.map(async (file) => {
          const filePath = window.webUtils.getPathForFile(file);
          const fileName = path.basename(filePath);

          if (!fileName.endsWith(".py") && !fileName.endsWith(".exe")) {
            return;
          }

          try {
            await window.api.invoke("ftm:saveTool", filePath);
          } catch (error) {
            console.error(`Failed to save tool: ${filePath}`, error);
            throw error;
          }
        });

        await Promise.allSettled(savePromises);
        queryClient.invalidateQueries({ queryKey: ["fix-tools"] });
      }
    }
  };

  return (
    <div
      onDragOver={handleDragOver}
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      className={`border rounded-lg bg-card transition-all overflow-hidden duration-200 flex flex-col h-full ${
        isDragOver ? "border-primary bg-primary/5 ring-2 ring-primary/20" : ""
      }`}
    >
      <div className="flex flex-col gap-2 p-3 border-b">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-base font-semibold">Available Fix Tools</p>
            <p className="text-sm text-muted-foreground">Select tools to add to your preset</p>
          </div>
          {isDragOver && (
            <Badge className="animate-pulse">
              <Upload className="h-3 w-3 mr-1" />
              Drop to Add
            </Badge>
          )}
        </div>

        <div className="relative shrink-0">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search tools..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-9"
          />
        </div>
      </div>

      <ScrollArea className="flex-1 overflow-hidden">
        <div className="flex flex-col space-y-2 p-3">
          {!filteredTools || filteredTools.length === 0 ? (
            <div
              className={`border-2 border-dashed rounded-lg p-12 text-center transition-colors ${
                isDragOver ? "border-primary bg-primary/10" : "border-border"
              }`}
            >
              <p className="text-sm text-muted-foreground font-medium">
                사용할 수 있는 스크립트가 없습니다
              </p>
              <p className="text-xs text-muted-foreground mt-1">
                파이썬 코드 또는 실행 파일을 여기로 드래그 드랍해 저장할 수 있습니다
              </p>
            </div>
          ) : (
            <Reorder.Group
              axis="y"
              values={filteredTools}
              className="flex flex-col space-y-2"
              onReorder={() => {}}
            >
              {filteredTools.map((tool) => (
                <Reorder.Item
                  key={tool.id}
                  value={tool}
                  className="group grid grid-cols-[1fr_auto_auto] items-center gap-2 p-3 rounded-lg border border-border bg-card hover:bg-accent/50 transition-colors"
                >
                  <div className="flex flex-row space-x-2 items-center">
                    {tool.type === "python" ? (
                      <img src={pythonIcon} alt="python" className="w-6 h-6" />
                    ) : (
                      <img src={execIcon} alt="python" className="w-6 h-6 dark:invert" />
                    )}

                    <p className="font-medium text-sm text-foreground truncate min-w-0">
                      {tool.name}
                    </p>
                  </div>

                  <Button size="icon" variant="outline" onClick={() => onAddTool(tool)}>
                    <Plus className="h-4 w-4" />
                  </Button>

                  <Button
                    size="icon"
                    variant="destructive"
                    onClick={() =>
                      window.api
                        .invoke("ftm:deleteTool", tool.id)
                        .then(() => queryClient.invalidateQueries({ queryKey: ["fix-tools"] }))
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
