import { FixToolList } from "@renderer/components/fix-tool-manager/fix-tool-list";
import { PresetBuilder } from "@renderer/components/fix-tool-manager/preset-builder";
import { PresetViewer } from "@renderer/components/fix-tool-manager/preset-viewer";
import { cn } from "@renderer/lib/utils";
import { getFixToolScripts } from "@renderer/wails/fix-tools";
import { ArrowRightIcon } from "lucide-react";
import { useState } from "react";

const getScriptsFn = getFixToolScripts;
export type Script = Awaited<ReturnType<typeof getScriptsFn>>[number];

export default function FixToolManager() {
  const [tab, setTab] = useState<"presets" | "builder">("presets");
  const [insertedPresetTools, setInsertedPresetTools] = useState<Script[]>([]);

  const handleAddScript = (script: Script) => {
    setInsertedPresetTools((prev) => {
      if (prev.some((t) => t.id === script.id)) {
        return prev;
      }
      return [...prev, script];
    });
  };

  const handleRemoveScript = (id: string) => {
    setInsertedPresetTools((prev) => prev.filter((script) => script.id !== id));
  };

  const handleReorderScripts = (scripts: Script[]) => {
    setInsertedPresetTools(scripts);
  };

  return (
    <div className="flex h-full min-h-0 flex-col p-4">
      <div className="mb-2 flex h-10 w-min shrink-0 space-x-2 rounded-xl bg-card p-1.5">
        <button
          onClick={() => setTab("presets")}
          className={cn("flex items-center rounded-lg p-2", tab === "presets" && "outline")}
        >
          <span className="text-sm font-medium">Presets</span>
        </button>

        <button
          onClick={() => setTab("builder")}
          className={cn("flex items-center rounded-lg p-2", tab === "builder" && "outline")}
        >
          <span className="text-sm font-medium">Builder</span>
        </button>
      </div>

      {tab === "presets" ? (
        <div className="min-h-0 flex-1">
          <PresetViewer />
        </div>
      ) : (
        <div className="grid min-h-0 flex-1 grid-cols-[1fr_2rem_1fr] gap-x-3">
          <div className="min-h-0">
            <FixToolList insertedPresetTools={insertedPresetTools} onAddScript={handleAddScript} />
          </div>
          <div className="flex items-center justify-center">
            <ArrowRightIcon className="h-4 w-4" />
          </div>
          <div className="min-h-0">
            <PresetBuilder
              insertedPresetTools={insertedPresetTools}
              setInsertedPresetTools={setInsertedPresetTools}
              onRemoveScript={handleRemoveScript}
              onReorderScripts={handleReorderScripts}
            />
          </div>
        </div>
      )}
    </div>
  );
}
