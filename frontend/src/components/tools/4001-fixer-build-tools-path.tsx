import { Tools } from "@bindings/tools";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { toErrorMessage } from "@shared/utils";
import { Loader2Icon, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

export function FourThousandOneFixerBuildToolsPath({ disabled }: { disabled: boolean }) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState("");
  const [savedPath, setSavedPath] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;

    void Tools.FourThousandOneFixerGetBuildToolsPath()
      .then((path) => {
        if (cancelled) return;
        const saved = path?.trim() ?? "";
        if (!saved) return;
        setSavedPath(saved);
        setDraft(saved);
      })
      .catch((loadError) => {
        console.error("tools:4001FixerGetBuildToolsPath failed", loadError);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const locked = savedPath !== "";

  const handleSave = async () => {
    const candidate = draft.trim();
    if (disabled || locked || busy || !candidate) return;

    setBusy(true);
    setError("");

    try {
      const result = await Tools.FourThousandOneFixerSetBuildToolsPath(candidate);
      if (result?.found) {
        setSavedPath(result.path);
        setDraft(result.path);
        return;
      }
      setError(t("page.tools.4001_fixer.build_tools_path_not_found"));
    } catch (saveError) {
      console.error(saveError);
      setError(toErrorMessage(saveError));
    } finally {
      setBusy(false);
    }
  };

  const handleClear = async () => {
    if (disabled || !locked || busy) return;

    setBusy(true);
    setError("");

    try {
      await Tools.FourThousandOneFixerClearBuildToolsPath();
      setSavedPath("");
      setDraft("");
    } catch (clearError) {
      console.error(clearError);
      setError(toErrorMessage(clearError));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-2 rounded-lg border bg-card p-4 transition-shadow duration-200 hover:shadow">
      <label className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
        {t("page.tools.4001_fixer.build_tools_path")}
      </label>
      <div className="flex items-center gap-2">
        <Input
          className="min-w-0 flex-1 font-mono text-xs"
          value={draft}
          disabled={locked || disabled || busy}
          placeholder={t("page.tools.4001_fixer.build_tools_path_placeholder")}
          aria-invalid={error ? true : undefined}
          onChange={(event) => {
            setDraft(event.target.value);
            if (error) setError("");
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter") void handleSave();
          }}
        />
        {locked ? (
          <Button
            type="button"
            size="icon"
            variant="outline"
            disabled={disabled || busy}
            aria-label={t("page.tools.4001_fixer.build_tools_path_clear")}
            onClick={() => void handleClear()}
          >
            {busy ? <Loader2Icon className="size-3.5 animate-spin" /> : <XIcon />}
          </Button>
        ) : (
          <Button
            type="button"
            variant="outline"
            disabled={disabled || busy || !draft.trim()}
            onClick={() => void handleSave()}
          >
            {busy ? (
              <Loader2Icon className="size-3.5 animate-spin" />
            ) : (
              t("page.tools.4001_fixer.build_tools_path_save")
            )}
          </Button>
        )}
      </div>
      {error ? (
        <p className="text-xs text-destructive">{error}</p>
      ) : (
        <p className="text-xs text-muted-foreground">
          {t("page.tools.4001_fixer.build_tools_path_hint")}
        </p>
      )}
    </div>
  );
}
