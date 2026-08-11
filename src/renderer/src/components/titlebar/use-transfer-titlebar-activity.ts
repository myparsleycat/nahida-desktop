import { buildTransferTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
import { useTitlebarActivity } from "@renderer/components/titlebar/use-titlebar-activity";
import { useGlobalStore } from "@renderer/store/global";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

export function useTransferTitlebarActivity() {
    const { t } = useTranslation();
    const transfers = useGlobalStore((state) => state.transfers);
    useTitlebarActivity(useMemo(() => buildTransferTitlebarActivity(transfers, t), [t, transfers]));
}
