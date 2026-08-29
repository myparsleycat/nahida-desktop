import { cn } from "@renderer/lib/utils";
import { ArrowUpFromLine, ArrowDownToLine, HardDrive, Clock } from "lucide-react";
import React from "react";
import { useTranslation } from "react-i18next";

import { TransferStatsProps } from "./types";

interface StatCardProps {
  icon: React.ReactNode;
  label: string;
  value: string;
  subValue?: string;
  className?: string;
}

function StatCard({ icon, label, value, subValue, className }: StatCardProps) {
  return (
    <div
      className={cn(
        "flex items-center gap-3 rounded-lg border border-border bg-card p-4",
        className,
      )}
    >
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-secondary">
        {icon}
      </div>
      <div className="flex flex-col">
        <span className="text-xs text-muted-foreground">{label}</span>
        <span className="text-lg font-semibold text-foreground">{value}</span>
        {subValue && <span className="text-xs text-muted-foreground">{subValue}</span>}
      </div>
    </div>
  );
}

export function TransferStats({
  uploadSpeed,
  downloadSpeed,
  totalTransferred,
  activeTransfers,
}: TransferStatsProps) {
  const { t } = useTranslation();

  return (
    <div className="grid w-full min-w-0 grid-cols-4 gap-3 lg:grid-cols-4">
      <StatCard
        icon={<ArrowUpFromLine className="text-success h-5 w-5" />}
        label={t("page.transfer.stats_label.upload")}
        value={uploadSpeed}
      />
      <StatCard
        icon={<ArrowDownToLine className="text-info h-5 w-5" />}
        label={t("page.transfer.stats_label.download")}
        value={downloadSpeed}
      />
      <StatCard
        icon={<HardDrive className="h-5 w-5 text-muted-foreground" />}
        label={t("page.transfer.stats_label.total_transferred")}
        value={totalTransferred}
      />
      <StatCard
        icon={<Clock className="text-warning h-5 w-5" />}
        label={t("page.transfer.stats_label.active_transfers")}
        value={activeTransfers.toString()}
      />
    </div>
  );
}
