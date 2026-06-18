// oxlint-disable react/no-children-prop
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Field, FieldError, FieldLabel } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { useModStore } from "@renderer/store/mod";
import { isNteImporter, NTE_IMPORTER_KEY } from "@shared/mod";
import type { GameConfig } from "@shared/types";
import { useForm } from "@tanstack/react-form";
import { ArrowDownIcon, ArrowUpIcon, FolderOpen, Trash2Icon, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const NO_IMPORTER_VALUE = "__none__";

interface EditGameDialogProps {
  games: GameConfig[];
  enabledImporters: Array<{ key: string }>;
  onPickFolder: () => Promise<string | null>;
  onUpdateGame: (
    game: string,
    updates: {
      modFolderPath: string;
      importer: string | null;
      linkedModFolderPath: string | null;
      gameInstallPath: string | null;
      gameExecutablePath: string | null;
    },
  ) => void;
  onDeleteGameClick: (game: string) => void;
  onReorderGames: (games: string[]) => void;
}

interface NteResolution {
  gameRootPath: string;
  executablePath: string;
  modFolderPath: string;
  linkedModFolderPath: string;
}

export function openEditGameDialog(
  game: GameConfig,
  setters: {
    setEditingGame: (game: GameConfig) => void;
    setIsEditGameDialogOpen: (open: boolean) => void;
  },
) {
  setters.setEditingGame(game);
  setters.setIsEditGameDialogOpen(true);
}

export function EditGameDialog({
  games,
  enabledImporters,
  onPickFolder,
  onUpdateGame,
  onDeleteGameClick,
  onReorderGames,
}: EditGameDialogProps) {
  const formId = "edit-game-dialog-form";
  const { t } = useTranslation();
  const [nteResolution, setNteResolution] = useState<NteResolution | null>(null);
  const [isResolvingNte, setIsResolvingNte] = useState(false);
  const [selectedImporter, setSelectedImporter] = useState(NO_IMPORTER_VALUE);
  const isNteSelected = isNteImporter(selectedImporter);
  const isOpen = useModStore((s) => s.isEditGameDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsEditGameDialogOpen);
  const editingGame = useModStore((s) => s.editingGame);
  const setEditingGame = useModStore((s) => s.setEditingGame);
  const currentGameIndex = editingGame
    ? games.findIndex((game) => game.game === editingGame.game)
    : -1;
  const canMoveUp = currentGameIndex > 0;
  const canMoveDown = currentGameIndex >= 0 && currentGameIndex < games.length - 1;
  const importers = [...enabledImporters, { key: NTE_IMPORTER_KEY }];

  const form = useForm({
    defaultValues: {
      path: "",
      customModFolderPath: "",
      importer: NO_IMPORTER_VALUE,
    },
    onSubmit: async ({ value }) => {
      if (!editingGame) {
        return;
      }

      const path = value.path.trim();
      const customModFolderPath = value.customModFolderPath.trim();
      const importer = value.importer === NO_IMPORTER_VALUE ? null : value.importer;
      const isNte = isNteImporter(importer);
      if (!path) {
        toast.warning(t("page.mod.dialog.add-game.#.1"));
        return;
      }

      if (isNte) {
        const resolution = nteResolution ?? (await resolveNtePath(path).catch(() => null));
        if (!resolution) {
          toast.warning(t("page.mod.dialog.add-game.nte_not_found"));
          return;
        }

        const linkedModFolderPath =
          resolution.linkedModFolderPath ??
          resolution.modFolderPath ??
          editingGame.linkedModFolderPath ??
          editingGame.modFolderPath ??
          path;

        onUpdateGame(editingGame.game, {
          modFolderPath: customModFolderPath || linkedModFolderPath,
          importer,
          linkedModFolderPath: customModFolderPath ? linkedModFolderPath : null,
          gameInstallPath: resolution.gameRootPath,
          gameExecutablePath: editingGame.gameExecutablePath ?? resolution.executablePath,
        });
        return;
      }

      onUpdateGame(editingGame.game, {
        modFolderPath: path,
        importer,
        linkedModFolderPath: null,
        gameInstallPath: null,
        gameExecutablePath: null,
      });
    },
  });

  useEffect(() => {
    if (!isOpen || !editingGame) {
      form.reset({
        path: "",
        customModFolderPath: "",
        importer: NO_IMPORTER_VALUE,
      });
      setNteResolution(null);
      setSelectedImporter(NO_IMPORTER_VALUE);
      return;
    }

    form.reset({
      path:
        editingGame.gameInstallPath ?? editingGame.linkedModFolderPath ?? editingGame.modFolderPath,
      customModFolderPath: editingGame.linkedModFolderPath ? editingGame.modFolderPath : "",
      importer: editingGame.importer ?? NO_IMPORTER_VALUE,
    });
    setSelectedImporter(editingGame.importer ?? NO_IMPORTER_VALUE);
    setNteResolution(
      isNteImporter(editingGame.importer)
        ? {
            gameRootPath: editingGame.gameInstallPath ?? "",
            executablePath: editingGame.gameExecutablePath ?? "",
            modFolderPath: editingGame.linkedModFolderPath ?? editingGame.modFolderPath,
            linkedModFolderPath: editingGame.linkedModFolderPath ?? editingGame.modFolderPath,
          }
        : null,
    );
  }, [editingGame, form, isOpen]);

  const handleOpenChange = (open: boolean) => {
    setIsOpen(open);
    if (!open) {
      setEditingGame(null);
      form.reset({
        path: "",
        customModFolderPath: "",
        importer: NO_IMPORTER_VALUE,
      });
      setNteResolution(null);
      setSelectedImporter(NO_IMPORTER_VALUE);
    }
  };

  const handlePickFolder = async () => {
    const path = await onPickFolder();
    if (!path) return;

    form.setFieldValue("path", path);
    setNteResolution(null);

    if (isNteSelected) {
      await resolveNtePath(path);
    }
  };

  const handlePickCustomModFolder = async () => {
    const path = await onPickFolder();
    if (path) {
      form.setFieldValue("customModFolderPath", path);
    }
  };

  const resolveNtePath = async (installPath: string) => {
    setIsResolvingNte(true);
    try {
      const resolution = await window.api.invoke("mod:resolveNteInstallPath", installPath);
      setNteResolution(resolution);
      if (!resolution) {
        toast.warning(t("page.mod.dialog.add-game.nte_not_found"));
      }
      return resolution;
    } finally {
      setIsResolvingNte(false);
    }
  };

  const handleMove = (direction: -1 | 1) => {
    if (!editingGame || currentGameIndex < 0) {
      return;
    }

    const targetIndex = currentGameIndex + direction;
    if (targetIndex < 0 || targetIndex >= games.length) {
      return;
    }

    const reorderedGames = [...games];
    const [movedGame] = reorderedGames.splice(currentGameIndex, 1);
    reorderedGames.splice(targetIndex, 0, movedGame);
    onReorderGames(reorderedGames.map((game) => game.game));
  };

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent className="w-100">
        <DialogHeader>
          <DialogTitle>{editingGame?.game}</DialogTitle>
        </DialogHeader>
        <form
          id={formId}
          className="space-y-4 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            e.stopPropagation();
            void form.handleSubmit();
          }}
        >
          <form.Field
            name="path"
            validators={{
              onChange: ({ value }) =>
                value.trim() ? undefined : t("page.mod.dialog.add-game.#.1"),
            }}
            children={(field) => (
              <Field>
                <FieldLabel>{t("page.mod.dialog.edit-game.path_label")}</FieldLabel>
                <div className="flex gap-2">
                  <Input
                    placeholder={
                      isNteSelected
                        ? t("page.mod.dialog.add-game.nte_install_path")
                        : t("page.mod.dialog.add-game.path_input_placeholder")
                    }
                    value={field.state.value}
                    readOnly={!isNteSelected}
                    onBlur={field.handleBlur}
                    onChange={(event) => {
                      field.handleChange(event.target.value);
                      setNteResolution(null);
                    }}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    disabled={isNteSelected && isResolvingNte}
                    onClick={() => void handlePickFolder()}
                  >
                    <FolderOpen className="size-4" />
                  </Button>
                </div>
                {nteResolution && isNteSelected ? (
                  <p className="text-xs text-muted-foreground break-all">
                    {nteResolution.modFolderPath}
                  </p>
                ) : null}
                {field.state.meta.isTouched && !field.state.meta.isValid ? (
                  <FieldError>{field.state.meta.errors.join(", ")}</FieldError>
                ) : null}
              </Field>
            )}
          />

          <form.Field
            name="importer"
            children={(field) => (
              <Field>
                <FieldLabel>{t("page.mod.dialog.edit-game.importer_label")}</FieldLabel>
                <Select
                  value={field.state.value}
                  onValueChange={(value) => {
                    const wasNte = isNteImporter(field.state.value);
                    const nextIsNte = isNteImporter(value);
                    field.handleChange(value);
                    setSelectedImporter(value);
                    setNteResolution(null);

                    if (wasNte && !nextIsNte) {
                      form.setFieldValue(
                        "path",
                        isNteImporter(editingGame?.importer)
                          ? ""
                          : (editingGame?.modFolderPath ?? ""),
                      );
                      form.setFieldValue("customModFolderPath", "");
                    }
                  }}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder={t("g.select")} />
                  </SelectTrigger>
                  <SelectContent aria-describedby={undefined} position="popper">
                    <SelectGroup>
                      <SelectItem value={NO_IMPORTER_VALUE}>
                        {t("page.mod.dialog.edit-game.no_importer")}
                      </SelectItem>
                      {importers.map((importer) => (
                        <SelectItem key={importer.key} value={importer.key}>
                          {importer.key}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            )}
          />

          {isNteSelected && (
            <form.Field
              name="customModFolderPath"
              children={(field) => (
                <Field>
                  <FieldLabel>{t("page.mod.dialog.add-game.nte_custom_mod_folder")}</FieldLabel>
                  <div className="flex gap-2">
                    <Input
                      value={field.state.value}
                      readOnly
                      hideFocusRing
                      onBlur={field.handleBlur}
                    />
                    <Button
                      type="button"
                      variant="outline"
                      size="icon"
                      onClick={handlePickCustomModFolder}
                    >
                      <FolderOpen className="size-4" />
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="icon"
                      disabled={!field.state.value}
                      onClick={() => field.handleChange("")}
                    >
                      <XIcon className="size-4" />
                    </Button>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {t("page.mod.dialog.add-game.nte_custom_mod_folder_description")}
                  </p>
                </Field>
              )}
            />
          )}

          <div className="space-y-2">
            <FieldLabel>{t("page.mod.dialog.edit-game.order_label")}</FieldLabel>
            <div className="flex items-center justify-between gap-2 rounded-lg border px-3 py-2">
              <span className="text-sm text-muted-foreground">
                {t("page.mod.dialog.edit-game.order_value", {
                  current: currentGameIndex + 1,
                  total: games.length,
                })}
              </span>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  disabled={!canMoveUp}
                  onClick={() => handleMove(-1)}
                >
                  <ArrowUpIcon className="size-4" />
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  disabled={!canMoveDown}
                  onClick={() => handleMove(1)}
                >
                  <ArrowDownIcon className="size-4" />
                </Button>
              </div>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("page.mod.dialog.edit-game.order_description")}
            </p>
          </div>

          <div className="flex justify-end items-center">
            <Button
              type="button"
              variant="outline"
              disabled={!editingGame}
              onClick={() => {
                if (editingGame) {
                  onDeleteGameClick(editingGame.game);
                }
              }}
            >
              <Trash2Icon className="size-4 text-destructive" />
              {t("page.mod.dialog.delete-game.title")}
            </Button>
          </div>
        </form>
        <DialogFooter>
          <DialogClose asChild>
            <Button type="button" variant="outline">
              {t("g.cancel")}
            </Button>
          </DialogClose>
          <form.Subscribe
            selector={(state) => [state.canSubmit, state.isSubmitting]}
            children={([canSubmit, isSubmitting]) => (
              <Button form={formId} type="submit" disabled={!canSubmit || isSubmitting}>
                {t("g.save")}
              </Button>
            )}
          />
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
