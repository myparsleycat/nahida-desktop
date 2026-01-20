import React from "react";
import { ArrowUpFromLine, ArrowDownToLine, HardDrive, Clock } from "lucide-react";
import { cn } from "@renderer/lib/utils";
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
  return (
    <div className="grid grid-cols-4 gap-3 lg:grid-cols-4 min-w-0 w-full">
      <StatCard
        icon={<ArrowUpFromLine className="h-5 w-5 text-success" />}
        label="업로드"
        value={uploadSpeed}
      />
      <StatCard
        icon={<ArrowDownToLine className="h-5 w-5 text-info" />}
        label="다운로드"
        value={downloadSpeed}
      />
      <StatCard
        icon={<HardDrive className="h-5 w-5 text-muted-foreground" />}
        label="전체 전송량"
        value={totalTransferred}
      />
      <StatCard
        icon={<Clock className="h-5 w-5 text-warning" />}
        label="진행중인 전송"
        value={activeTransfers.toString()}
      />
    </div>
  );
}
