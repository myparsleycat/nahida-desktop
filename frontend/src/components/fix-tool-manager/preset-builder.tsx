import { Tools } from "@bindings/tools";
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
      await Tools.CreatePreset({
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
    <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border bg-card">
      <div className="flex flex-col gap-2 border-b p-3">
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

        <div className="flex flex-row items-center space-x-4">
          <label htmlFor="preset-name" className="text-sm text-nowrap text-foreground">
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
            <div className="rounded-lg border-2 border-dashed border-border p-12 text-center">
              <p className="text-sm text-muted-foreground">
                {t("page.tools.fix-tool-manager.builder.right.noScripts.title")}
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
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
                    "group grid cursor-grab grid-cols-[auto_1fr_auto] items-center gap-3 rounded-lg border border-border bg-card p-4 transition-colors active:cursor-grabbing",
                    "hover:border-accent/40 hover:bg-card/80",
                  )}
                >
                  <div className="pointer-events-none flex shrink-0 items-center gap-2">
                    <GripVertical className="h-4 w-4 text-muted-foreground" />
                    <span className="min-w-5 text-xs font-medium text-muted-foreground">
                      {index + 1}
                    </span>
                  </div>

                  <div className="flex min-w-0 flex-row items-center space-x-2">
                    {script.type === "python" ? (
                      <img src={pythonIcon} alt="python" className="h-6 w-6" />
                    ) : (
                      <img src={execIcon} alt="python" className="h-6 w-6 dark:invert" />
                    )}

                    <p className="min-w-0 truncate text-sm font-medium text-foreground">
                      {script.name}
                    </p>
                  </div>

                  <Button
                    size="icon"
                    variant="outline"
                    onClick={() => onRemoveScript(script.id)}
                    className="pointer-events-auto text-destructive hover:text-destructive"
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
