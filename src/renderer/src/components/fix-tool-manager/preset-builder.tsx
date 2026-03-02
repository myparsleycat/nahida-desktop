import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { cn } from "@renderer/lib/utils";
import { Reorder } from "framer-motion";
import { GripVertical, Save, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import execIcon from "@/renderer/assets/img/document-executable-svgrepo-com.svg";
import pythonIcon from "@/renderer/assets/img/python-svgrepo-com.svg";
import { Script } from "../tools/fix-tool-manger";

interface PresetBuilderProps {
  insertedPresetTools: Script[];
  setInsertedPresetTools: (tools: Script[]) => void;
  onRemoveScript: (id: string) => void;
  onReorderScripts: (scripts: Script[]) => void;
}

export function PresetBuilder({
  insertedPresetTools,
  setInsertedPresetTools,
  onRemoveScript,
  onReorderScripts,
}: PresetBuilderProps) {
  const { t } = useTranslation();
  const [presetName, setPresetName] = useState("");

  const handleSavePreset = async () => {
    try {
      await window.api.invoke("ftm:createPreset", {
        name: presetName,
        scriptIds: insertedPresetTools.map((script) => script.id),
      });

      setPresetName("");
      setInsertedPresetTools([]);
      toast.success(t("page.tools.fix-tool-manager.builder.right.#.handleSavePreset.success"));
    } catch (error) {
      const msg = error instanceof Error ? error.message : "Unknown error";
      if (msg.includes("already exists")) {
        toast.warning(
          t("page.tools.fix-tool-manager.builder.right.#.handleSavePreset.alreadyExists"),
        );
      } else {
        toast.error(t("page.tools.fix-tool-manager.builder.right.#.handleSavePreset.error"), {
          description: msg,
        });
      }
    }
  };

  return (
    <div className="border rounded-lg bg-card overflow-hidden flex flex-col h-full min-h-0">
      <div className="flex flex-col gap-2 p-3 border-b">
        <div className="flex items-center justify-between">
          <div className="flex-1">
            <p className="text-base font-semibold">
              {t("page.tools.fix-tool-manager.builder.right.title")}
            </p>
            <p className="text-sm text-muted-foreground">
              {t("page.tools.fix-tool-manager.builder.right.description")}
            </p>
          </div>
          <Button
            onClickPromise={handleSavePreset}
            size="sm"
            disabled={!presetName.trim() || insertedPresetTools.length < 1}
          >
            <Save className="h-4 w-4" />
            {t("g.save")}
          </Button>
        </div>

        <div className="flex flex-row space-x-4 items-center">
          <label htmlFor="preset-name" className="text-sm text-foreground text-nowrap">
            {t("page.tools.fix-tool-manager.builder.right.presetName")}
          </label>
          <Input
            id="preset-name"
            type="text"
            value={presetName}
            onChange={(e) => {
              setPresetName(e.target.value);
            }}
            disabled={insertedPresetTools.length < 1}
            placeholder={t("page.tools.fix-tool-manager.builder.right.enterPresetName")}
          />
        </div>
      </div>

      <ScrollArea className="flex-1 overflow-hidden">
        <div className="p-3">
          {insertedPresetTools.length === 0 ? (
            <div className="border-2 border-dashed border-border rounded-lg p-12 text-center">
              <p className="text-sm text-muted-foreground">
                {t("page.tools.fix-tool-manager.builder.right.noScripts.title")}
              </p>
              <p className="text-xs text-muted-foreground mt-1">
                {t("page.tools.fix-tool-manager.builder.right.noScripts.description")}
              </p>
            </div>
          ) : (
            <Reorder.Group
              axis="y"
              values={insertedPresetTools}
              onReorder={onReorderScripts}
              className="flex flex-col space-y-2"
            >
              {insertedPresetTools.map((script, index) => (
                <Reorder.Item
                  key={script.id}
                  value={script}
                  className={cn(
                    "group grid grid-cols-[auto_1fr_auto] items-center gap-3 p-4 rounded-lg border border-border bg-card cursor-grab active:cursor-grabbing transition-colors",
                    "hover:border-accent/40 hover:bg-card/80",
                  )}
                >
                  <div className="flex items-center gap-2 shrink-0 pointer-events-none">
                    <GripVertical className="h-4 w-4 text-muted-foreground" />
                    <span className="text-xs font-medium text-muted-foreground min-w-5">
                      {index + 1}
                    </span>
                  </div>

                  <div className="flex flex-row space-x-2 items-center min-w-0">
                    {script.type === "python" ? (
                      <img src={pythonIcon} alt="python" className="w-6 h-6" />
                    ) : (
                      <img src={execIcon} alt="python" className="w-6 h-6 dark:invert" />
                    )}

                    <p className="font-medium text-sm text-foreground truncate min-w-0">
                      {script.name}
                    </p>
                  </div>

                  <Button
                    size="icon"
                    variant="outline"
                    onClick={() => onRemoveScript(script.id)}
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
