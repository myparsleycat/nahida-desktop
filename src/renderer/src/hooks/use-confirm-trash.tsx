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
import {
  useCallback,
  useMemo,
  useState,
  type ComponentProps,
  type MouseEvent,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

interface ConfirmTrashOptions {
  path: string;
  title: ReactNode;
  description: ReactNode;
  onSuccess?: () => void | Promise<void>;
  onError?: (error: unknown) => void | Promise<void>;
  onOpenChange?: (open: boolean) => void;
  loadingMessage?: string;
  successMessage?: string;
  errorMessage?: string;
  actionLabel?: string;
  contentProps?: ComponentProps<typeof AlertDialogContent>;
}

export function useConfirmTrash() {
  const { t } = useTranslation();
  const [pending, setPending] = useState<ConfirmTrashOptions | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);

  const closeDialog = useCallback(() => {
    if (isDeleting) return;
    pending?.onOpenChange?.(false);
    setPending(null);
  }, [isDeleting, pending]);

  const confirmTrash = useCallback((options: ConfirmTrashOptions) => {
    options.onOpenChange?.(true);
    setPending(options);
  }, []);

  const handleConfirm = useCallback(
    async (e: MouseEvent<HTMLButtonElement>) => {
      e.stopPropagation();

      if (!pending) return;

      const promise = window.api.invoke("util:fs:trash", pending.path);
      setIsDeleting(true);
      toast.promise(promise, {
        loading: pending.loadingMessage ?? t("page.mod.toast.trash-loading"),
        success: pending.successMessage ?? t("page.mod.toast.trash-success"),
        error: pending.errorMessage ?? t("page.mod.toast.trash-error"),
      });

      try {
        await promise;
        await pending.onSuccess?.();
        pending.onOpenChange?.(false);
        setPending(null);
      } catch (error) {
        await pending.onError?.(error);
      } finally {
        setIsDeleting(false);
      }
    },
    [pending, t],
  );

  const dialog = useMemo(
    () => (
      <AlertDialog
        open={!!pending}
        onOpenChange={(open) => {
          if (!open) {
            closeDialog();
          }
        }}
      >
        <AlertDialogContent
          {...pending?.contentProps}
          onClick={(e) => {
            pending?.contentProps?.onClick?.(e);
            e.stopPropagation();
          }}
        >
          <AlertDialogHeader>
            <AlertDialogTitle>{pending?.title}</AlertDialogTitle>
            <AlertDialogDescription>{pending?.description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isDeleting}>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={handleConfirm} disabled={isDeleting}>
              {pending?.actionLabel ?? t("g.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    ),
    [closeDialog, handleConfirm, isDeleting, pending, t],
  );

  return {
    confirmTrash,
    confirmTrashDialog: dialog,
    isDeleting,
  };
}
