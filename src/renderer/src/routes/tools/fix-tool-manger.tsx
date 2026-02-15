import { FixToolList } from "@renderer/components/fix-tool-manager/fix-tool-list";
import { PresetBuilder } from "@renderer/components/fix-tool-manager/preset-builder";
import { PresetViewer } from "@renderer/components/fix-tool-manager/preset-viewer";
import { Titlebar } from "@renderer/components/titlebar";
import { cn } from "@renderer/lib/utils";
import { createFileRoute } from "@tanstack/react-router";
import { ArrowRightIcon } from "lucide-react";
import { useState } from "react";

export const Route = createFileRoute("/tools/fix-tool-manger")({
  component: RouteComponent,
});

const getScriptsFn = () => window.api.invoke("ftm:getScripts");
export type Script = Awaited<ReturnType<typeof getScriptsFn>>[number];

function RouteComponent() {
  const [tab, setTab] = useState<"presets" | "builder">("presets");
  const [insertedPresetTools, setInsertedPresetTools] = useState<Script[]>([]);

  const handleAddScript = (script: Script) => {
    if (insertedPresetTools.some((t) => t.id === script.id)) {
      return;
    }
    setInsertedPresetTools([...insertedPresetTools, script]);
  };

  const handleRemoveScript = (id: string) => {
    setInsertedPresetTools(insertedPresetTools.filter((script) => script.id !== id));
  };

  const handleReorderScripts = (scripts: Script[]) => {
    setInsertedPresetTools(scripts);
  };

  return (
    <div className="p-5 h-[calc(100vh-74px)]">
      <Titlebar title={{ text: "Fix Tool Manager", position: "center" }} />

      <div className="flex rounded-xl p-1.5 w-min bg-accent h-10 mb-2 space-x-2">
        <button
          onClick={() => setTab("presets")}
          className={cn("flex items-center p-2 rounded-lg", tab === "presets" && "outline")}
        >
          <span className="text-sm font-medium">Presets</span>
        </button>

        <button
          onClick={() => setTab("builder")}
          className={cn("flex items-center p-2 rounded-lg", tab === "builder" && "outline")}
        >
          <span className="text-sm font-medium">Builder</span>
        </button>
      </div>

      {tab === "presets" ? (
        <PresetViewer />
      ) : (
        <div className="grid grid-cols-[1fr_2rem_1fr] h-full gap-x-3">
          <FixToolList insertedPresetTools={insertedPresetTools} onAddScript={handleAddScript} />
          <div className="flex items-center justify-center">
            <ArrowRightIcon className="w-4 h-4" />
          </div>
          <PresetBuilder
            insertedPresetTools={insertedPresetTools}
            setInsertedPresetTools={setInsertedPresetTools}
            onRemoveScript={handleRemoveScript}
            onReorderScripts={handleReorderScripts}
          />
        </div>
      )}
    </div>
  );
}
