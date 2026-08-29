import { Mod } from "@bindings/mod";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Field, FieldLabel } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { useGameMutations } from "@renderer/hooks/use-mod-mutations";
import { useModStore } from "@renderer/store/mod";
import { toErrorMessage } from "@shared/utils";
import { FolderOpen } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function NteLaunchDialog() {
  const { t } = useTranslation();
  const selectedGame = useModStore((s) => s.selectedGame);
  const isOpen = useModStore((s) => s.isNteLaunchDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsNteLaunchDialogOpen);
  const [executablePath, setExecutablePath] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const { setNteLauncherPathMutation } = useGameMutations();

  const handleOpenChange = (open: boolean) => {
    setIsOpen(open);
    if (!open) {
      setExecutablePath("");
      setIsSubmitting(false);
    }
  };

  const handlePickExecutable = async () => {
    const path = await Mod.PickExecutable();
    if (path) {
      setExecutablePath(path);
    }
  };

  const handleLaunch = async () => {
    const path = executablePath.trim();
    if (!path || !selectedGame) {
      toast.warning(t("page.mod.dialog.nte-launch.path_required"));
      return;
    }

    setIsSubmitting(true);
    try {
      await setNteLauncherPathMutation.mutateAsync({
        game: selectedGame,
        launcherPath: path,
      });
      await Mod.StartNteLauncher(selectedGame);
      handleOpenChange(false);
    } catch (error) {
      const errorMessage = toErrorMessage(error);

      if (errorMessage.includes("NTE_LAUNCHER_PATH_NOT_FOUND")) {
        toast.error(t("page.mod.hooks.use-mod-mutations.start-nte-launcher.not-found"));
        return;
      }

      if (errorMessage.includes("NTE_LAUNCHER_PATH_NOT_SET")) {
        toast.error(t("page.mod.hooks.use-mod-mutations.start-nte-launcher.not-set"));
        return;
      }

      toast.error(errorMessage || t("page.mod.hooks.use-mod-mutations.start-nte-launcher.failed"));
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent className="w-100">
        <DialogHeader>
          <DialogTitle>{t("page.mod.dialog.nte-launch.title")}</DialogTitle>
        </DialogHeader>

        <Field>
          <FieldLabel>{t("page.mod.dialog.nte-launch.path_label")}</FieldLabel>
          <div className="flex gap-2">
            <Input value={executablePath} readOnly hideFocusRing />
            <Button type="button" variant="outline" size="icon" onClick={handlePickExecutable}>
              <FolderOpen className="size-4" />
            </Button>
          </div>
        </Field>

        <DialogFooter>
          <DialogClose render={<Button type="button" variant="outline" disabled={isSubmitting} />}>
            {t("g.cancel")}
          </DialogClose>
          <Button
            type="button"
            disabled={!executablePath.trim() || isSubmitting}
            onClick={() => void handleLaunch()}
          >
            {t("page.mod.dialog.nte-launch.launch")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
