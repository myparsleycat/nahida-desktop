import { useCallback } from "react";

import type { TransferItemProps } from "./types";

import { useTransferFilter } from "./hooks/use-transfer-filter";
import { TransferEmptyState } from "./transfer-empty-state";
import { TransferItem } from "./transfer-item";
import { TransferToolbar } from "./transfer-toolbar";

interface TransferListProps {
  transfers: TransferItemProps[];
  onPause?: (id: string) => void;
  onResume?: (id: string) => void;
  onCancel?: (id: string) => void;
  onRetry?: (id: string) => void;
  onPauseAll?: () => void;
  onResumeAll?: () => void;
  onClearCompleted?: () => void;
}

export function TransferList({
  transfers,
  onPause,
  onResume,
  onCancel,
  onRetry,
  onPauseAll,
  onResumeAll,
  onClearCompleted,
}: TransferListProps) {
  const {
    searchQuery,
    setSearchQuery,
    activeTab,
    setActiveTab,
    statusFilter,
    toggleStatusFilter,
    filteredTransfers,
    counts,
  } = useTransferFilter({ transfers });

  const handlePause = useCallback((id: string) => onPause?.(id), [onPause]);
  const handleResume = useCallback((id: string) => onResume?.(id), [onResume]);
  const handleCancel = useCallback((id: string) => onCancel?.(id), [onCancel]);
  const handleRetry = useCallback((id: string) => onRetry?.(id), [onRetry]);

  return (
    <div className="flex w-full max-w-full min-w-0 flex-col gap-4">
      <TransferToolbar
        activeTab={activeTab}
        onTabChange={setActiveTab}
        counts={counts}
        searchQuery={searchQuery}
        onSearchChange={setSearchQuery}
        statusFilter={statusFilter}
        onToggleStatusFilter={toggleStatusFilter}
        onPauseAll={onPauseAll}
        onResumeAll={onResumeAll}
        onClearCompleted={onClearCompleted}
      />

      <div className="flex w-full max-w-full min-w-0 flex-col gap-2">
        {filteredTransfers.length === 0 ? (
          <TransferEmptyState activeTab={activeTab} hasSearchQuery={!!searchQuery} />
        ) : (
          filteredTransfers.map((transfer) => (
            <TransferItem
              key={transfer.id}
              {...transfer}
              onPause={handlePause}
              onResume={handleResume}
              onCancel={handleCancel}
              onRetry={handleRetry}
            />
          ))
        )}
      </div>
    </div>
  );
}
