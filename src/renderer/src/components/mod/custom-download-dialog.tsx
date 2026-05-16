// oxlint-disable react/no-children-prop
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Field, FieldError } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { useForm } from "@tanstack/react-form";
import { DownloadIcon, LoaderIcon, XIcon } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import isURL from "validator/lib/isURL";

interface CustomDownloadDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  groupName?: string;
  groupPath?: string;
}

export function CustomDownloadDialog({
  open,
  onOpenChange,
  groupName,
  groupPath,
}: CustomDownloadDialogProps) {
  const formId = "custom-download-dialog-form";
  const { t } = useTranslation();
  const form = useForm({
    defaultValues: {
      url: "",
    },
    onSubmit: async ({ value }) => {
      const trimmedUrl = value.url.trim();
      if (!groupPath || !trimmedUrl) {
        return;
      }

      if (!isURL(trimmedUrl)) {
        toast.warning(t("page.mod.content-header.download_dialog.invalid_url"));
        return;
      }

      try {
        await window.api.invoke("mod:downloadFromUrl", trimmedUrl, groupPath);
        onOpenChange(false);
      } catch (error) {
        const { code, message } = getErrorCode(error);

        if (code === "DOWNLOAD_URL_HTML_PAGE") {
          toast.warning(t("page.mod.content-header.download_dialog.file_only"));
          return;
        }

        toast.error(message);
      }
    },
  });

  useEffect(() => {
    if (!open) {
      form.reset();
    }
  }, [form, open]);

  const getErrorCode = (error: unknown) => {
    const message = error instanceof Error ? error.message : String(error);
    const matched = message.match(
      /\b(DOWNLOAD_URL_REQUIRED|INVALID_DOWNLOAD_URL|UNSUPPORTED_DOWNLOAD_URL_PROTOCOL|DOWNLOAD_URL_HTML_PAGE)\b/,
    );

    return {
      code: matched?.[1] ?? null,
      message,
    };
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{t("page.mod.content-header.download_dialog.title")}</DialogTitle>
          <DialogDescription>
            {t("page.mod.content-header.download_dialog.description")}
          </DialogDescription>
        </DialogHeader>

        <form
          id={formId}
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            e.stopPropagation();
            void form.handleSubmit();
          }}
        >
          <form.Field
            name="url"
            validators={{
              onChange: ({ value }) => {
                const trimmedValue = value.trim();
                if (!trimmedValue) {
                  return t("page.mod.content-header.download_dialog.invalid_url");
                }

                return isURL(trimmedValue)
                  ? undefined
                  : t("page.mod.content-header.download_dialog.invalid_url");
              },
            }}
            children={(field) => (
              <Field>
                <p className="text-sm font-medium">
                  {t("page.mod.content-header.download_dialog.url_label")}
                </p>
                <Input
                  type="url"
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={t("page.mod.content-header.download_dialog.url_placeholder")}
                />
                {field.state.meta.isTouched && !field.state.meta.isValid ? (
                  <FieldError>{field.state.meta.errors.join(", ")}</FieldError>
                ) : null}
              </Field>
            )}
          />

          <div className="space-y-2">
            <p className="text-sm font-medium">
              {t("page.mod.content-header.download_dialog.target_label")}
            </p>
            <Input
              readOnly
              hideFocusRing
              value={groupName || t("page.mod.content-header.download_dialog.no_target")}
            />
          </div>
        </form>
        <DialogFooter>
          <form.Subscribe
            selector={(state) => [state.canSubmit, state.isSubmitting]}
            children={([canSubmit, isSubmitting]) => (
              <>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  onClick={() => onOpenChange(false)}
                  disabled={isSubmitting}
                >
                  <XIcon />
                </Button>
                <Button
                  form={formId}
                  type="submit"
                  disabled={!groupPath || !canSubmit || isSubmitting}
                  size="icon"
                >
                  {isSubmitting ? (
                    <LoaderIcon className="size-4 animate-spin" />
                  ) : (
                    <DownloadIcon className="size-4" />
                  )}
                </Button>
              </>
            )}
          />
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
