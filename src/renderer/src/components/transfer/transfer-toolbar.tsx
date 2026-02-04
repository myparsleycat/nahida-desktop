import {
  Search,
  Filter,
  ArrowUpFromLine,
  ArrowDownToLine,
  Pause,
  Play,
  BrushCleaningIcon,
} from "lucide-react";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@renderer/components/ui/tabs";
import { Badge } from "@renderer/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuCheckboxItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
  DropdownMenuLabel,
} from "@renderer/components/ui/dropdown-menu";
import { cn } from "@renderer/lib/utils";
import { TransferTabType, TransferStatus } from "./types";
import { useTranslation } from "react-i18next";

interface TransferToolbarProps {
  activeTab: TransferTabType;
  onTabChange: (val: TransferTabType) => void;
  counts: {
    total: number;
    uploads: number;
    downloads: number;
    active: number;
    paused: number;
    completed: number;
  };
  searchQuery: string;
  onSearchChange: (val: string) => void;
  statusFilter: TransferStatus[];
  onToggleStatusFilter: (status: TransferStatus) => void;
  onPauseAll?: () => void;
  onResumeAll?: () => void;
  onClearCompleted?: () => void;
}

export function TransferToolbar({
  activeTab,
  onTabChange,
  counts,
  searchQuery,
  onSearchChange,
  statusFilter,
  onToggleStatusFilter,
  onPauseAll,
  onResumeAll,
  onClearCompleted,
}: TransferToolbarProps) {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-4 w-full max-w-full min-w-0">
      <div className="flex flex-col gap-4 w-full max-w-full min-w-0 sm:flex-row sm:items-center sm:justify-between sm:flex-wrap">
        <Tabs
          value={activeTab}
          onValueChange={(v) => onTabChange(v as TransferTabType)}
          className="w-full sm:w-auto min-w-0 max-w-full"
        >
          <TabsList className="bg-secondary w-full sm:w-auto h-auto flex-wrap justify-start">
            <TabsTrigger value="all" className="gap-2 flex-1 sm:flex-none">
              {t("page.transfer.toolbar.all")}
              <Badge variant="secondary" className="ml-1 h-5 px-1.5 text-xs">
                {counts.total}
              </Badge>
            </TabsTrigger>
            <TabsTrigger value="uploads" className="gap-2 flex-1 sm:flex-none">
              <ArrowUpFromLine className="h-4 w-4" />
              {t("page.transfer.toolbar.upload")}
              <Badge variant="secondary" className="ml-1 h-5 px-1.5 text-xs">
                {counts.uploads}
              </Badge>
            </TabsTrigger>
            <TabsTrigger value="downloads" className="gap-2 flex-1 sm:flex-none">
              <ArrowDownToLine className="h-4 w-4" />
              {t("page.transfer.toolbar.download")}
              <Badge variant="secondary" className="ml-1 h-5 px-1.5 text-xs">
                {counts.downloads}
              </Badge>
            </TabsTrigger>
          </TabsList>
        </Tabs>

        <div className="flex shrink-0 items-center gap-2 self-end sm:self-auto flex-wrap justify-end">
          {/* <Button
            variant="outline"
            size="sm"
            onClick={onPauseAll}
            className="gap-2 bg-transparent"
            disabled={counts.active === 0}
          >
            <Pause className="h-4 w-4" />
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={onResumeAll}
            className="gap-2 bg-transparent"
            disabled={counts.paused === 0}
          >
            <Play className="h-4 w-4" />
          </Button> */}
          <Button
            variant="outline"
            size="sm"
            onClick={onClearCompleted}
            className="bg-transparent"
            disabled={counts.completed === 0}
          >
            <BrushCleaningIcon className="h-4 w-4" />
          </Button>
        </div>
      </div>

      <div className="flex items-center gap-2 w-full max-w-full min-w-0">
        <div className="relative flex-1 min-w-0">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder={t("g.search")}
            value={searchQuery}
            onChange={(e) => onSearchChange(e.target.value)}
            className="pl-9 w-full min-w-0"
          />
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="outline"
              size="icon"
              className={cn("shrink-0", statusFilter.length > 0 && "border-primary")}
            >
              <Filter className="h-4 w-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-48">
            <DropdownMenuLabel>상태 필터</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuCheckboxItem
              checked={statusFilter.includes("uploading")}
              onCheckedChange={() => onToggleStatusFilter("uploading")}
            >
              업로드 중
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={statusFilter.includes("downloading")}
              onCheckedChange={() => onToggleStatusFilter("downloading")}
            >
              다운로드 중
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={statusFilter.includes("paused")}
              onCheckedChange={() => onToggleStatusFilter("paused")}
            >
              일시정지됨
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={statusFilter.includes("completed")}
              onCheckedChange={() => onToggleStatusFilter("completed")}
            >
              완료됨
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={statusFilter.includes("failed")}
              onCheckedChange={() => onToggleStatusFilter("failed")}
            >
              실패
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem
              checked={statusFilter.includes("queued")}
              onCheckedChange={() => onToggleStatusFilter("queued")}
            >
              대기열
            </DropdownMenuCheckboxItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
