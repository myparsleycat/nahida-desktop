// oxlint-disable react/no-children-prop
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Button } from "@renderer/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { Field, FieldError } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { useDialogStore, useSelectionStore } from "@renderer/store/drive";
import type { Content } from "@shared/types";
import { useForm } from "@tanstack/react-form";
import { useMutation } from "@tanstack/react-query";
import { useLocation, useRouteContext } from "@tanstack/react-router";
import { t } from "i18next";
import { Loader2Icon } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const ValidateName = (name: string) => {
  if (!name.trim()) {
    return t("#.ValidateName.0");
  } else if (name[0] === " ") {
    return t("#.ValidateName.1");
  } else if (name[name.length - 1] === " ") {
    return t("#.ValidateName.2");
  } else if (name.length <= 0 || name.length > 255) {
    return t("#.ValidateName.3");
  }

  return null;
};

function getRenameDefaultValues(item?: Content) {
  if (!item) {
    return {
      name: "",
      ext: "",
    };
  }

  if (!item.isDir && item.name.includes(".")) {
    return {
      name: item.name.split(".").slice(0, -1).join("."),
      ext: `.${item.name.split(".").pop()}`,
    };
  }

  return {
    name: item.name,
    ext: "",
  };
}

export function RenameDialog() {
  const { t } = useTranslation();
  const dialog = useDialogStore();
  const selection = useSelectionStore();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const location = useLocation();

  const id = location.pathname.split("/").pop() || "";
  const selectedItem = selection.selectedItems[0];
  const defaultRenameValues = getRenameDefaultValues(selectedItem);

  const mutation = useMutation({
    mutationKey: ["drive", "rename", id],
    mutationFn: async ({ item, rename }: { item: Content; rename: string }) => {
      const data = await window.api.invoke("drive:patch:rename", item.id, rename);
      return data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries();
      dialog.setOpen("renameDialog", false);
      return t("page.drive.dialog.rename.#.toast-promise.success");
    },
    onError: (err) => {
      if (err.message.includes("INVALID_WINDOWS_FILENAME")) {
        toast.warning(
          selection.selectedItems[0].isDir
            ? t("page.drive.dialog.common.invalid_dir_name")
            : t("page.drive.dialog.common.invalid_file_name"),
        );
      } else {
        toast.error("Rename Error", {
          description: err.message,
        });
      }
    },
  });

  const form = useForm({
    defaultValues: defaultRenameValues,
    onSubmit: async ({ value }) => {
      if (!selectedItem) {
        return;
      }

      const rename = `${value.name}${value.ext}`;
      const validateResult = ValidateName(rename);
      if (validateResult) {
        toast.warning(t("page.drive.dialog.rename.#.rename.1"), {
          description: validateResult,
        });
        return;
      }

      await mutation.mutateAsync({
        item: selectedItem,
        rename,
      });
    },
  });

  useEffect(() => {
    form.reset(defaultRenameValues);
  }, [defaultRenameValues, form]);

  if (selectedItem) {
    return (
      <Dialog
        open={dialog.renameDialog.open}
        onOpenChange={(v) => {
          if (!v) {
            form.reset(defaultRenameValues);
          }
          dialog.setOpen("renameDialog", v);
        }}
      >
        <DialogContent aria-describedby={undefined}>
          <DialogHeader>
            <DialogTitle>{t("page.drive.dialog.rename.title")}</DialogTitle>
          </DialogHeader>
          <form
            className="flex flex-col space-y-4"
            autoComplete="off"
            onSubmit={(e) => {
              e.preventDefault();
              void form.handleSubmit();
            }}
          >
            <form.Field
              name="name"
              validators={{
                onChange: ({ value }) =>
                  value.trim() ? undefined : t("page.drive.dialog.rename.#.rename.0"),
              }}
              children={(field) => (
                <Field>
                  <div className="flex flex-row gap-x-4">
                    <Input
                      className="h-10"
                      id={field.name}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder={t("page.drive.dialog.rename.name_input_placeholder")}
                      maxLength={200}
                      required
                    />
                    {!selectedItem.isDir && (
                      <form.Field
                        name="ext"
                        children={(extField) => (
                          <Input
                            className="h-10 w-1/4"
                            value={extField.state.value}
                            onBlur={extField.handleBlur}
                            onChange={(e) => extField.handleChange(e.target.value)}
                            placeholder={t("page.drive.dialog.rename.extension")}
                            maxLength={50}
                          />
                        )}
                      />
                    )}
                  </div>
                  {field.state.meta.isTouched && !field.state.meta.isValid ? (
                    <FieldError>{field.state.meta.errors.join(", ")}</FieldError>
                  ) : null}
                </Field>
              )}
            />

            <div className="flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2">
              <Button
                type="button"
                variant="outline"
                onClick={async (e) => {
                  e.preventDefault();
                  form.reset(defaultRenameValues);
                  dialog.setOpen("renameDialog", false, undefined);
                }}
              >
                {t("g.cancel")}
              </Button>
              <form.Subscribe
                selector={(state) => [state.canSubmit, state.isSubmitting]}
                children={([canSubmit, isSubmitting]) => (
                  <Button type="submit" disabled={!canSubmit || mutation.isPending || isSubmitting}>
                    {t("g.confirm")}
                  </Button>
                )}
              />
            </div>
          </form>
        </DialogContent>
      </Dialog>
    );
  } else {
    return null;
  }
}

export function NewDirectoryDialog({ contents }: { contents: Content[] }) {
  const { t } = useTranslation();
  const dialog = useDialogStore();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const location = useLocation();

  const id = location.pathname.split("/").pop();

  const mutation = useMutation({
    mutationKey: ["akasha", "make_dir", id],
    mutationFn: async (name: string) => {
      if (!id) {
        toast.error("cannot get current id");
        return;
      }
      await window.api.invoke("drive:post:dir", id, name);
    },
    onSuccess: async () => {
      toast.success(t("page.drive.dialog.create_dir.#.toast-promise.success"));
      dialog.setOpen("createDirDialog", false);
      await queryClient.invalidateQueries();
    },
    onError: (err) => {
      if (err.message.includes("INVALID_WINDOWS_FILENAME")) {
        toast.warning(t("page.drive.dialog.common.invalid_dir_name"));
      } else {
        toast.error("New Directory Error", {
          description: err.message,
        });
      }
    },
  });
  const form = useForm({
    defaultValues: {
      name: "",
    },
    onSubmit: async ({ value }) => {
      const validateResult = ValidateName(value.name);
      if (validateResult) {
        toast.warning(t("page.drive.dialog.create_dir.#.0"), {
          description: validateResult,
        });
        return;
      }

      if (contents.some((item) => item.isDir && item.name === value.name)) {
        toast.warning(t("page.drive.dialog.create_dir.#.2"));
        return;
      }

      await mutation.mutateAsync(value.name);
    },
  });

  return (
    <Dialog
      open={dialog.createDirDialog.open}
      onOpenChange={(v) => {
        if (!v) {
          form.reset();
        }
        dialog.setOpen("createDirDialog", v);
      }}
    >
      <DialogContent aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>{t("page.drive.dialog.create_dir.title")}</DialogTitle>
        </DialogHeader>
        <form
          className="flex flex-col space-y-4"
          autoComplete="off"
          onSubmit={(e) => {
            e.preventDefault();
            void form.handleSubmit();
          }}
        >
          <form.Field
            name="name"
            validators={{
              onChange: ({ value }) =>
                value.trim() ? undefined : t("page.drive.dialog.create_dir.#.0"),
            }}
            children={(field) => (
              <Field>
                <Input
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={t("page.drive.dialog.create_dir.name_input_placeholder")}
                  maxLength={255}
                  required
                />
                {field.state.meta.isTouched && !field.state.meta.isValid ? (
                  <FieldError>{field.state.meta.errors.join(", ")}</FieldError>
                ) : null}
              </Field>
            )}
          />
          <div className="flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2">
            <Button
              type="button"
              variant="outline"
              onClick={(e) => {
                e.preventDefault();
                form.reset();
                dialog.setOpen("createDirDialog", false);
              }}
            >
              {t("g.cancel")}
            </Button>
            <form.Subscribe
              selector={(state) => [state.canSubmit, state.isSubmitting]}
              children={([canSubmit, isSubmitting]) => (
                <Button
                  type="submit"
                  className="flex items-center gap-2"
                  disabled={!canSubmit || mutation.isPending || isSubmitting}
                >
                  {mutation.isPending && <Loader2Icon className="animate-spin" />}
                  {t("g.confirm")}
                </Button>
              )}
            />
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function DeleteItemsDialog() {
  const { t } = useTranslation();
  const { deleteItemsDialog, setOpen } = useDialogStore();
  const { selectedItems, setSelectedItems } = useSelectionStore();
  const { queryClient } = useRouteContext({ from: "__root__" });

  const deleteMutation = useMutation({
    mutationKey: ["akasha", "drive", "delete-items", "delete"],
    mutationFn: async (ids: string[]) => {
      await window.api.invoke("drive:delete:items", ids, "delete");
    },
  });

  const handleDelete = async () => {
    if (selectedItems.length === 0) {
      setOpen("deleteItemsDialog", false);
      return;
    }

    await deleteMutation
      .mutateAsync(selectedItems.map((item) => item.id))
      .then(async () => {
        toast.success(t("page.drive.dialog.delete_items.#.toast.success"));
        setSelectedItems([]);
        setOpen("deleteItemsDialog", false);
        await queryClient.invalidateQueries();
      })
      .catch((err: string) => {
        toast.error(err);
      });
  };

  return (
    <AlertDialog
      open={deleteItemsDialog.open}
      onOpenChange={(nextOpen, eventDetails) => {
        if (deleteMutation.isPending && !nextOpen) {
          eventDetails.cancel();
          return;
        }

        setOpen("deleteItemsDialog", nextOpen);
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("page.drive.dialog.delete_items.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("page.drive.dialog.delete_items.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={handleDelete}
            disabled={deleteMutation.isPending}
          >
            {deleteMutation.isPending && <Loader2Icon className="animate-spin" />}
            {t("page.drive.dialog.delete_items.action")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function ConflictNameDialog() {
  const { t } = useTranslation();
  const { conflictNameDialog, setOpen, resolveDialog } = useDialogStore();
  const conflicts = conflictNameDialog.data?.conflicts ?? [];
  const preview = conflicts.slice(0, 6);
  const hiddenCount = Math.max(conflicts.length - preview.length, 0);

  return (
    <AlertDialog
      open={conflictNameDialog.open}
      onOpenChange={(open) => {
        setOpen("conflictNameDialog", open);
        if (!open) {
          resolveDialog("conflictNameDialog", "cancel");
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("page.drive.dialog.conflict_name.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("page.drive.dialog.conflict_name.description", { count: conflicts.length })}
          </AlertDialogDescription>
          <div className="mt-2 text-sm">
            {t("page.drive.dialog.conflict_name.option_suffix")}
            <br />
            {t("page.drive.dialog.conflict_name.option_skip")}

            {preview.length > 0 && (
              <div className="mt-2 w-full text-left text-xs text-muted-foreground">
                <p>{t("page.drive.dialog.conflict_name.preview_label")}</p>
                <p>{preview.join(", ")}</p>
                {hiddenCount > 0 && (
                  <p>{t("page.drive.dialog.conflict_name.preview_more", { count: hiddenCount })}</p>
                )}
              </div>
            )}
          </div>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel
            onClick={() => {
              setOpen("conflictNameDialog", false);
              resolveDialog("conflictNameDialog", "cancel");
            }}
          >
            {t("g.cancel")}
          </AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              setOpen("conflictNameDialog", false);
              resolveDialog("conflictNameDialog", "skip");
            }}
          >
            {t("page.drive.dialog.conflict_name.action_skip")}
          </AlertDialogAction>
          <AlertDialogAction
            onClick={() => {
              setOpen("conflictNameDialog", false);
              resolveDialog("conflictNameDialog", "suffix");
            }}
          >
            {t("page.drive.dialog.conflict_name.action_suffix")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
