import { ArrowUpFromLine, ArrowDownToLine, Filter } from "lucide-react";
import { TransferTabType } from "./types";

interface TransferEmptyStateProps {
  activeTab: TransferTabType;
  hasSearchQuery: boolean;
}

export function TransferEmptyState({ activeTab, hasSearchQuery }: TransferEmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center rounded-lg border border-dashed border-border py-12 text-center w-full max-w-full">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-secondary">
        {activeTab === "uploads" ? (
          <ArrowUpFromLine className="h-6 w-6 text-muted-foreground" />
        ) : activeTab === "downloads" ? (
          <ArrowDownToLine className="h-6 w-6 text-muted-foreground" />
        ) : (
          <Filter className="h-6 w-6 text-muted-foreground" />
        )}
      </div>
      <p className="mt-4 text-sm font-medium text-foreground">진행중인 전송이 없습니다</p>
    </div>
  );
}
