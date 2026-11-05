import { Mod } from "@bindings/mod";
import { Button } from "@renderer/components/ui/button";
import { Field, FieldLabel } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { toErrorMessage } from "@shared/utils";
import { FolderOpen, XIcon } from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export interface NteResolution {
  gameRootPath: string;
  executablePath: string;
  modFolderPath: string;
  linkedModFolderPath: string;
  requiresElevation?: boolean;
}

export function useNteInstallResolution(initialResolution: NteResolution | null = null) {
  const [resolution, setResolution] = useState(initialResolution);
  const [isResolving, setIsResolving] = useState(false);
  const [isNotFound, setIsNotFound] = useState(false);
  // Every path change starts a new request, so a slow lookup for an earlier path never lands.
  const requestRef = useRef(0);

  const reset = () => {
    requestRef.current += 1;
    setResolution(null);
    setIsResolving(false);
    setIsNotFound(false);
  };

  const resolve = async (installPath: string): Promise<NteResolution | null> => {
    const request = ++requestRef.current;
    setIsResolving(true);
    try {
      const next = await Mod.ResolveNteInstallPath(installPath);
      if (request !== requestRef.current) {
        return null;
      }
      setResolution(next);
      setIsNotFound(!next);
      return next;
    } catch (error) {
      if (request === requestRef.current) {
        toast.error(toErrorMessage(error));
      }
      return null;
    } finally {
      if (request === requestRef.current) {
        setIsResolving(false);
      }
    }
  };

  return { resolution, isResolving, isNotFound, reset, resolve };
}

export function resolveNteLinkedModFolderPath(
  resolution: NteResolution,
  customModFolderPath: string,
) {
  if (customModFolderPath) return resolution.linkedModFolderPath;
  return isSameNteResolutionPath(resolution.modFolderPath, resolution.linkedModFolderPath)
    ? null
    : resolution.linkedModFolderPath;
}

function isSameNteResolutionPath(left: string, right: string) {
  return (
    left.trim().replaceAll("/", "\\").toLowerCase() ===
    right.trim().replaceAll("/", "\\").toLowerCase()
  );
}

interface NteCustomModFolderFieldProps {
  id?: string;
  value: string;
  onBlur: () => void;
  onChange: (value: string) => void;
  onPickFolder: () => Promise<string | null>;
}

export function NteCustomModFolderField({
  id,
  value,
  onBlur,
  onChange,
  onPickFolder,
}: NteCustomModFolderFieldProps) {
  const { t } = useTranslation();

  const handlePick = async () => {
    const path = await onPickFolder();
    if (path) {
      onChange(path);
    }
  };

  return (
    <Field>
      <FieldLabel htmlFor={id}>{t("page.mod.dialog.add-game.nte_custom_mod_folder")}</FieldLabel>
      <div className="flex gap-2">
        <Input id={id} value={value} readOnly hideFocusRing onBlur={onBlur} />
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label={t("g.browse")}
          onClick={() => void handlePick()}
        >
          <FolderOpen className="size-4" />
        </Button>
        <Button
          type="button"
          variant="outline"
          size="icon"
          disabled={!value}
          aria-label={t("g.clear")}
          onClick={() => onChange("")}
        >
          <XIcon className="size-4" />
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        {t("page.mod.dialog.add-game.nte_custom_mod_folder_description")}
      </p>
    </Field>
  );
}
