import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { cn } from "@renderer/lib/utils";
import type { FixTool } from "@renderer/routes/tools/fix-tool-manger";
import { Reorder } from "framer-motion";
import { GripVertical, Save, X } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import execIcon from "@/renderer/assets/img/document-executable-svgrepo-com.svg";
import pythonIcon from "@/renderer/assets/img/python-svgrepo-com.svg";

interface PresetBuilderProps {
  insertedPresetTools: FixTool[];
  setInsertedPresetTools: (tools: FixTool[]) => void;
  onRemoveTool: (id: string) => void;
  onReorderTools: (tools: FixTool[]) => void;
}

export function PresetBuilder({
  insertedPresetTools,
  setInsertedPresetTools,
  onRemoveTool,
  onReorderTools,
}: PresetBuilderProps) {
  const [presetName, setPresetName] = useState("");

  const handleSavePreset = async () => {
    try {
      await window.api.invoke("ftm:createPreset", {
        name: presetName,
        toolIds: insertedPresetTools.map((tool) => tool.id),
      });

      setPresetName("");
      setInsertedPresetTools([]);
      toast.success("프리셋이 저장되었습니다");
    } catch (error) {
      const msg = error instanceof Error ? error.message : "Unknown error";
      if (msg.includes("already exists")) {
        toast.warning("이미 존재하는 프리셋 이름입니다");
      } else {
        toast.error(msg);
      }
    }
  };

  return (
    <div className="border rounded-lg bg-card overflow-hidden flex flex-col h-full">
      <div className="flex flex-col gap-2 p-3 border-b">
        <div className="flex items-center justify-between">
          <div className="flex-1">
            <p className="text-base font-semibold">Preset Configuration</p>
            <p className="text-sm text-muted-foreground">Drag to reorder tools in your preset</p>
          </div>
          <Button
            onClickPromise={handleSavePreset}
            size="sm"
            disabled={!presetName.trim() || insertedPresetTools.length < 1}
          >
            <Save className="h-4 w-4 mr-2" />
            Save
          </Button>
        </div>

        <div className="flex flex-row space-x-4 items-center">
          <label htmlFor="preset-name" className="text-sm text-foreground text-nowrap">
            Preset Name
          </label>
          <Input
            id="preset-name"
            type="text"
            value={presetName}
            onChange={(e) => {
              setPresetName(e.target.value);
            }}
            disabled={insertedPresetTools.length < 1}
            placeholder="Enter preset name"
          />
        </div>
      </div>

      <ScrollArea className="flex-1 overflow-hidden">
        <div className="p-3">
          {insertedPresetTools.length === 0 ? (
            <div className="border-2 border-dashed border-border rounded-lg p-12 text-center">
              <p className="text-sm text-muted-foreground">No tools added yet</p>
              <p className="text-xs text-muted-foreground mt-1">
                Add tools from the left panel to build your preset
              </p>
            </div>
          ) : (
            <Reorder.Group
              axis="y"
              values={insertedPresetTools}
              onReorder={onReorderTools}
              className="flex flex-col space-y-2"
            >
              {insertedPresetTools.map((tool, index) => (
                <Reorder.Item
                  key={tool.id}
                  value={tool}
                  className={cn(
                    "group grid grid-cols-[auto_1fr_auto] items-center gap-3 p-4 rounded-lg border border-border bg-card cursor-grab active:cursor-grabbing transition-colors",
                    "hover:bg-accent/30",
                  )}
                >
                  <div className="flex items-center gap-2 shrink-0 pointer-events-none">
                    <GripVertical className="h-4 w-4 text-muted-foreground" />
                    <span className="text-xs font-medium text-muted-foreground min-w-[20px]">
                      {index + 1}
                    </span>
                  </div>

                  <div className="flex flex-row space-x-2 items-center min-w-0">
                    {tool.type === "python" ? (
                      <img src={pythonIcon} alt="python" className="w-6 h-6" />
                    ) : (
                      <img src={execIcon} alt="python" className="w-6 h-6 dark:invert" />
                    )}

                    <p className="font-medium text-sm text-foreground truncate min-w-0">
                      {tool.name}
                    </p>
                  </div>

                  <Button
                    size="icon"
                    variant="outline"
                    onClick={() => onRemoveTool(tool.id)}
                    className="text-destructive hover:text-destructive pointer-events-auto"
                  >
                    <X className="h-4 w-4" />
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
