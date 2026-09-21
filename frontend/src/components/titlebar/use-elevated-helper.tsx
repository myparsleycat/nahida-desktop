import { Input } from "@bindings/platform";
import { buildElevatedHelperTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
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
import { Logger } from "@renderer/lib/logger";
import { setSetting } from "@renderer/lib/settings";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import {
  ELEVATED_HELPER_ACTIVITY_ID,
  ELEVATED_HELPER_STATUS_EVENT,
  type ElevatedHelperStatus,
  isElevatedHelperRequiredError,
} from "@shared/elevated-helper";
import { toErrorMessage } from "@shared/utils";
import { Events } from "@wailsio/runtime";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const emptyStatus: ElevatedHelperStatus = { enabled: false, running: false };

function asHelperStatus(value: unknown): ElevatedHelperStatus {
  if (typeof value !== "object" || value === null) return emptyStatus;
  const record = value as Record<string, unknown>;
  return {
    enabled: record.enabled === true,
    running: record.running === true,
  };
}

export function useElevatedHelper() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<ElevatedHelperStatus>(emptyStatus);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [starting, setStarting] = useState(false);
  const missingOpenedRef = useRef(false);
  const startingRef = useRef(false);

  const startHelper = useCallback(async () => {
    if (startingRef.current) return;
    startingRef.current = true;
    setStarting(true);
    try {
      await setSetting("general.elevatedHelperEnabled", true);
    } catch (error) {
      startingRef.current = false;
      setStarting(false);
      toast.error(toErrorMessage(error));
    }
  }, []);

  useEffect(() => {
    const apply = (next: ElevatedHelperStatus, settleStart = false) => {
      setStatus(next);
      if (next.running) {
        setDialogOpen(false);
      }
      if (settleStart && startingRef.current) {
        startingRef.current = false;
        setStarting(false);
      }
    };

    let receivedEvent = false;
    const offStatus = Events.On(ELEVATED_HELPER_STATUS_EVENT, (event) => {
      receivedEvent = true;
      apply(asHelperStatus(event.data), true);
    });
    const offAgent = Events.On("agent:update", (event) => {
      const update = event.data as { type?: string; payload?: { error?: unknown } };
      if (update.type !== "tool-end") return;
      if (!isElevatedHelperRequiredError(update.payload?.error)) return;
      setDialogOpen(true);
    });

    void Input.GetElevatedHelperStatus()
      .then((value) => {
        if (receivedEvent) return;
        apply(asHelperStatus(value));
      })
      .catch((error: unknown) => {
        Logger.error(error, "ElevatedHelper:status");
      });

    return () => {
      offStatus();
      offAgent();
    };
  }, []);

  useEffect(() => {
    const missing = status.enabled && !status.running;
    if (!missing) {
      missingOpenedRef.current = false;
      titlebarActivityStore.getState().removeActivity(ELEVATED_HELPER_ACTIVITY_ID);
      return;
    }

    const defaultOpen = !missingOpenedRef.current;
    missingOpenedRef.current = true;
    titlebarActivityStore.getState().upsertActivity(
      buildElevatedHelperTitlebarActivity({
        onStart: () => {
          void startHelper();
        },
        defaultOpen,
        starting,
        t,
      }),
    );
  }, [startHelper, starting, status.enabled, status.running, t]);

  const closeDialog = useCallback(() => {
    if (starting) return;
    setDialogOpen(false);
  }, [starting]);

  return useMemo(
    () => (
      <AlertDialog
        open={dialogOpen}
        onOpenChange={(open) => {
          if (!open) closeDialog();
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("titlebar.activity.elevatedHelper.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("titlebar.activity.elevatedHelper.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={starting}>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction disabled={starting} onClickPromise={startHelper}>
              {t("titlebar.activity.elevatedHelper.start")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    ),
    [closeDialog, dialogOpen, startHelper, starting, t],
  );
}
