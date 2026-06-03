// oxlint-disable react/no-children-prop
import { Alert, AlertDescription } from "@renderer/components/ui/alert";
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
import { useForm } from "@tanstack/react-form";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { FolderOpen, Plus } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const NO_IMPORTER_VALUE = "__none__";

interface AddGameDialogProps {
  onPickFolder: () => Promise<string | null>;
  onAddGame: (name: string, path: string, importer: string | null) => void;
}

export function AddGameDialog({ onPickFolder, onAddGame }: AddGameDialogProps) {
  const formId = "add-game-dialog-form";
  const { t } = useTranslation();
  const navi = useNavigate();

  const isOpen = useModStore((s) => s.isAddGameDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsAddGameDialogOpen);
  const { data: xxmiData } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => window.api.invoke("xxmi:getXXMIData"),
  });

  const enabledImporters = xxmiData?.enabledImporters ?? [];
  const isXXMIConfigured = !!xxmiData?.xxmiPath;

  const form = useForm({
    defaultValues: {
      name: "",
      path: "",
      importer: NO_IMPORTER_VALUE,
    },
    onSubmit: async ({ value }) => {
      const name = value.name.trim();
      const path = value.path.trim();

      if (!name) {
        toast.warning(t("page.mod.dialog.add-game.#.0"));
        return;
      }

      if (!path) {
        toast.warning(t("page.mod.dialog.add-game.#.1"));
        return;
      }

      onAddGame(name, path, value.importer === NO_IMPORTER_VALUE ? null : value.importer);
    },
  });

  useEffect(() => {
    if (!isOpen) {
      form.reset();
    }
  }, [form, isOpen]);

  const handlePickFolder = async () => {
    const path = await onPickFolder();
    if (path) {
      form.setFieldValue("path", path);
    }
  };

  const handleOpenChange = (open: boolean) => {
    setIsOpen(open);
    if (!open) {
      form.reset();
    }
  };

  const handleOpenXXMISettings = () => {
    handleOpenChange(false);
    void navi({ to: "/setting/xxmi" });
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
                  {t("page.mod.dialog.add-game.path_input_placeholder")}
                </FieldLabel>
                <div className="flex gap-2">
                  <Input
                    id={field.name}
                    value={field.state.value}
                    readOnly
                    hideFocusRing
                    onBlur={field.handleBlur}
                  />
                  <Button type="button" variant="outline" size="icon" onClick={handlePickFolder}>
                    <FolderOpen className="size-4" />
                  </Button>
                </div>
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
                <Select value={field.state.value} onValueChange={field.handleChange}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder={t("g.select")} />
                  </SelectTrigger>
                  <SelectContent aria-describedby={undefined} position="popper">
                    <SelectGroup>
                      <SelectItem value={NO_IMPORTER_VALUE}>
                        {t("page.mod.dialog.edit-game.no_importer")}
                      </SelectItem>
                      {enabledImporters.map((importer) => (
                        <SelectItem key={importer.key} value={importer.key}>
                          {importer.key}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                {!isXXMIConfigured && (
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
              <Button form={formId} type="submit" disabled={!canSubmit || isSubmitting}>
                {t("g.add")}
              </Button>
            )}
          />
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
