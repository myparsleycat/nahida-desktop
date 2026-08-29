import { ArrowDownToLine, ArrowUpFromLine, Filter } from "lucide-react";
import { useTranslation } from "react-i18next";

import { TransferTabType } from "./types";

interface TransferEmptyStateProps {
  activeTab: TransferTabType;
  hasSearchQuery: boolean;
}

// oxlint-disable-next-line no-unused-vars
export function TransferEmptyState({ activeTab, hasSearchQuery }: TransferEmptyStateProps) {
  const { t } = useTranslation();

  return (
    <div className="flex w-full max-w-full flex-col items-center justify-center rounded-lg border border-dashed border-border py-12 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-secondary">
        {activeTab === "uploads" ? (
          <ArrowUpFromLine className="h-6 w-6 text-muted-foreground" />
        ) : activeTab === "downloads" ? (
          <ArrowDownToLine className="h-6 w-6 text-muted-foreground" />
        ) : (
          <Filter className="h-6 w-6 text-muted-foreground" />
        )}
      </div>
      <p className="mt-4 text-sm font-medium text-foreground">
        {t("page.transfer.empty_state.all_tabs")}
      </p>
    </div>
  );
}
