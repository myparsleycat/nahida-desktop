import React from "react";
import { ArrowUpFromLine, ArrowDownToLine, HardDrive, Clock } from "lucide-react";
import { cn } from "@renderer/lib/utils";
import { TransferStatsProps } from "./types";
import { useTranslation } from "react-i18next";

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
    <div className="grid grid-cols-4 gap-3 lg:grid-cols-4 min-w-0 w-full">
      <StatCard
        icon={<ArrowUpFromLine className="h-5 w-5 text-success" />}
        label={t("page.transfer.stats_label.upload")}
        value={uploadSpeed}
      />
      <StatCard
        icon={<ArrowDownToLine className="h-5 w-5 text-info" />}
        label={t("page.transfer.stats_label.download")}
        value={downloadSpeed}
      />
      <StatCard
        icon={<HardDrive className="h-5 w-5 text-muted-foreground" />}
        label={t("page.transfer.stats_label.total_transferred")}
        value={totalTransferred}
      />
      <StatCard
        icon={<Clock className="h-5 w-5 text-warning" />}
        label={t("page.transfer.stats_label.active_transfers")}
        value={activeTransfers.toString()}
      />
    </div>
  );
}
