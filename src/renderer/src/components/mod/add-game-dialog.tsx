// oxlint-disable react/no-children-prop
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
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
import { useForm } from "@tanstack/react-form";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { FolderOpen, Plus, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Alert, AlertDescription } from "../ui/alert";
import { NteBootstrapProgressView } from "./nte-bootstrap-progress";

const NO_IMPORTER_VALUE = "__none__";

interface AddGameDialogProps {
  onPickFolder: () => Promise<string | null>;
  isAddingGame: boolean;
  onAddGame: (
    name: string,
    path: string,
    importer: string | null,
    linkedModFolderPath?: string | null,
    gameInstallPath?: string | null,
    gameExecutablePath?: string | null,
  ) => void;
}

interface NteResolution {
  gameRootPath: string;
  executablePath: string;
  modFolderPath: string;
  linkedModFolderPath: string;
}

export function AddGameDialog({ isAddingGame, onPickFolder, onAddGame }: AddGameDialogProps) {
  const formId = "add-game-dialog-form";
  const { t } = useTranslation();
  const navi = useNavigate();
  const [nteResolution, setNteResolution] = useState<NteResolution | null>(null);
  const [isResolvingNte, setIsResolvingNte] = useState(false);
  const [selectedImporter, setSelectedImporter] = useState(NO_IMPORTER_VALUE);
  const isNteSelected = isNteImporter(selectedImporter);

  const isOpen = useModStore((s) => s.isAddGameDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsAddGameDialogOpen);
  const { data: xxmiData } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => window.api.invoke("xxmi:getXXMIData"),
  });

  const enabledImporters = xxmiData?.enabledImporters ?? [];
  const importers = [...enabledImporters, { key: NTE_IMPORTER_KEY }];
  const isXXMIConfigured = !!xxmiData?.xxmiPath;

  const form = useForm({
    defaultValues: {
      name: "",
      path: "",
      customModFolderPath: "",
      importer: NO_IMPORTER_VALUE,
    },
    onSubmit: async ({ value }) => {
      const importer = value.importer === NO_IMPORTER_VALUE ? null : value.importer;
      const isNte = isNteImporter(importer);
      const name = isNte ? t("page.mod.dialog.add-game.nte_game_name") : value.name.trim();
      const path = value.path.trim();
      const customModFolderPath = value.customModFolderPath.trim();

      if (!name) {
        toast.warning(t("page.mod.dialog.add-game.#.0"));
        return;
      }

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

        onAddGame(
          name,
          customModFolderPath || resolution.modFolderPath,
          importer,
          customModFolderPath ? resolution.linkedModFolderPath : null,
          resolution.gameRootPath,
          resolution.executablePath,
        );
        return;
      }

      onAddGame(name, path, importer);
    },
  });

  useEffect(() => {
    if (!isOpen) {
      form.reset();
      setNteResolution(null);
      setSelectedImporter(NO_IMPORTER_VALUE);
    }
  }, [form, isOpen]);

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

  const handleOpenChange = (open: boolean) => {
    setIsOpen(open);
    if (!open) {
      form.reset();
      setNteResolution(null);
      setSelectedImporter(NO_IMPORTER_VALUE);
    }
  };

  const handleOpenXXMISettings = () => {
    handleOpenChange(false);
    void navi({ to: "/setting/xxmi" });
  };

  const handleImporterChange = (value: string, onChange: (value: string) => void) => {
    const wasNte = isNteImporter(selectedImporter);
    const nextIsNte = isNteImporter(value);
    onChange(value);
    setSelectedImporter(value);
    setNteResolution(null);

    if (wasNte && !nextIsNte) {
      form.setFieldValue("path", "");
      form.setFieldValue("customModFolderPath", "");
    }

    if (nextIsNte) {
      form.setFieldValue("name", t("page.mod.dialog.add-game.nte_game_name"));
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

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button variant="outline" size="icon">
          <Plus className="size-4" />
        </Button>
      </DialogTrigger>
      <DialogContent className="w-100">
        <DialogHeader>
          <DialogTitle>{t("page.mod.dialog.add-game.title")}</DialogTitle>
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
            name="name"
            validators={{
              onChange: ({ value }) =>
                value.trim() ? undefined : t("page.mod.dialog.add-game.#.0"),
            }}
            children={(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {t("page.mod.dialog.add-game.name_input_placeholder")}
                </FieldLabel>
                <Input
                  id={field.name}
                  value={field.state.value}
                  readOnly={isNteSelected}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                />
                {field.state.meta.isTouched && !field.state.meta.isValid ? (
                  <FieldError>{field.state.meta.errors.join(", ")}</FieldError>
                ) : null}
              </Field>
            )}
          />

          <form.Field
            name="path"
            validators={{
              onChange: ({ value }) =>
                value.trim() ? undefined : t("page.mod.dialog.add-game.#.1"),
            }}
            children={(field) => (
              <Field>
                <FieldLabel htmlFor={field.name}>
                  {isNteSelected
                    ? t("page.mod.dialog.add-game.nte_install_path")
                    : t("page.mod.dialog.add-game.path_input_placeholder")}
                </FieldLabel>
                <div className="flex gap-2">
                  <Input
                    id={field.name}
                    value={field.state.value}
                    readOnly={!isNteSelected}
                    hideFocusRing
                    onBlur={field.handleBlur}
                    onChange={(e) => {
                      field.handleChange(e.target.value);
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

          {isNteSelected && (
            <form.Field
              name="customModFolderPath"
              children={(field) => (
                <Field>
                  <FieldLabel>{t("page.mod.dialog.add-game.nte_custom_mod_folder")}</FieldLabel>
                  <div className="flex gap-2">
                    <Input
                      id={field.name}
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

          <NteBootstrapProgressView active={isAddingGame && isNteSelected} />

          <form.Field
            name="importer"
            children={(field) => (
              <Field>
                <FieldLabel>{t("page.mod.dialog.edit-game.importer_label")}</FieldLabel>
                <Select
                  value={field.state.value}
                  onValueChange={(value) => handleImporterChange(value, field.handleChange)}
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
                {!isXXMIConfigured && !isNteImporter(field.state.value) && (
                  <Alert>
                    <AlertDescription>
                      <div className="flex flex-col gap-3">
                        <span>{t("page.mod.dialog.add-game.xxmi_path_required")}</span>
                        <Button
                          type="button"
                          variant="outline"
                          className="w-fit"
                          onClick={handleOpenXXMISettings}
                        >
                          {t("page.mod.dialog.add-game.open_xxmi_settings")}
                        </Button>
                      </div>
                    </AlertDescription>
                  </Alert>
                )}
              </Field>
            )}
          />
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
              <Button
                form={formId}
                type="submit"
                disabled={!canSubmit || isSubmitting || isAddingGame}
              >
                {t("g.add")}
              </Button>
            )}
          />
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
