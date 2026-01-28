import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";
import {
  Search,
  Filter,
  MoreHorizontal,
  Download,
  Trash2,
  RefreshCw,
  HardDrive,
  Clock,
  CheckCircle2,
  AlertCircle,
  Loader2,
  Plus,
  Calendar,
  ChevronDown,
} from "lucide-react";

export const Route = createFileRoute("/backup/")({
  component: RouteComponent,
});

type BackupStatus = "completed" | "in-progress" | "failed";

interface Backup {
  id: string;
  name: string;
  status: BackupStatus;
  size: string;
  createdAt: string;
}

const backups: Backup[] = [
  {
    id: "8JfpicWAW",
    name: "Genshin Mods",
    status: "completed",
    size: "2.4 GB",
    createdAt: "12m ago",
  },
  {
    id: "BCoTKPg4n",
    name: "Starail Mods",
    status: "in-progress",
    size: "856 MB",
    createdAt: "38m ago",
  },
];

function StatusBadge({ status }: { status: BackupStatus }) {
  const config = {
    completed: {
      icon: CheckCircle2,
      label: "Completed",
      className: "text-success",
    },
    "in-progress": {
      icon: Loader2,
      label: "In Progress",
      className: "text-info animate-spin",
    },
    failed: {
      icon: AlertCircle,
      label: "Failed",
      className: "text-destructive",
    },
  };

  const { icon: Icon, label, className } = config[status];

  return (
    <div className="flex items-center gap-2">
      <Icon className={`h-4 w-4 ${className}`} />
      <span className="text-sm text-foreground">{label}</span>
    </div>
  );
}

function RouteComponent() {
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<BackupStatus | "all">("all");

  const filteredBackups = backups.filter((backup) => {
    const matchesSearch = backup.name.toLowerCase().includes(searchQuery.toLowerCase());
    const matchesStatus = statusFilter === "all" || backup.status === statusFilter;
    return matchesSearch && matchesStatus;
  });

  const statusCounts = {
    completed: backups.filter((b) => b.status === "completed").length,
    "in-progress": backups.filter((b) => b.status === "in-progress").length,
    failed: backups.filter((b) => b.status === "failed").length,
  };

  return (
    <div className="flex flex-col gap-6 p-6">
      {/* Filters */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-1 items-center gap-3">
          <div className="relative flex-1 max-w-sm">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              placeholder="Search backups..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-9 bg-secondary border-border"
            />
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" className="gap-2 bg-transparent">
                <Calendar className="h-4 w-4" />
                <span className="hidden sm:inline">Date Range</span>
                <ChevronDown className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuItem>Last 24 hours</DropdownMenuItem>
              <DropdownMenuItem>Last 7 days</DropdownMenuItem>
              <DropdownMenuItem>Last 30 days</DropdownMenuItem>
              <DropdownMenuItem>All time</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" className="gap-2 bg-transparent">
                <Filter className="h-4 w-4" />
                <span className="hidden sm:inline">Filter</span>
                <ChevronDown className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuItem onClick={() => setStatusFilter("all")}>All Status</DropdownMenuItem>
              <DropdownMenuItem onClick={() => setStatusFilter("completed")}>
                Completed
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => setStatusFilter("in-progress")}>
                In Progress
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => setStatusFilter("failed")}>Failed</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <div className="flex items-center gap-3">
          <div className="hidden md:flex items-center gap-2 text-sm text-muted-foreground">
            <div className="flex items-center gap-1">
              <span className="h-2 w-2 rounded-full bg-success" />
              <span className="h-2 w-2 rounded-full bg-info" />
              <span className="h-2 w-2 rounded-full bg-destructive" />
            </div>
            <span>
              Status{" "}
              <span className="text-foreground">
                {statusCounts.completed + statusCounts["in-progress"]}/{backups.length}
              </span>
            </span>
          </div>
          <Button className="gap-2">
            <Plus className="h-4 w-4" />
            Create Backup
          </Button>
        </div>
      </div>

      {/* Backups Table */}
      <div className="rounded-lg border border-border overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b border-border bg-secondary/50">
                <th className="px-4 py-3 text-left text-sm font-medium text-muted-foreground">
                  Name
                </th>
                <th className="px-4 py-3 text-left text-sm font-medium text-muted-foreground">
                  Status
                </th>
                <th className="px-4 py-3 text-left text-sm font-medium text-muted-foreground hidden md:table-cell">
                  Size
                </th>
                <th className="px-4 py-3 text-right text-sm font-medium text-muted-foreground">
                  Created
                </th>
                <th className="px-4 py-3 w-12" />
              </tr>
            </thead>
            <tbody>
              {filteredBackups.map((backup) => (
                <tr
                  key={backup.id}
                  className="border-b border-border last:border-0 hover:bg-secondary/30 transition-colors"
                >
                  <td className="px-4 py-4">
                    <div className="flex flex-col gap-1">
                      <span className="font-medium text-foreground">{backup.name}</span>
                      <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <HardDrive className="h-3 w-3" />
                        <span className="font-mono">{backup.id}</span>
                      </div>
                    </div>
                  </td>
                  <td className="px-4 py-4">
                    <StatusBadge status={backup.status} />
                  </td>
                  <td className="px-4 py-4 hidden md:table-cell">
                    <span className="text-sm text-foreground">{backup.size}</span>
                  </td>
                  <td className="px-4 py-4 text-right">
                    <span className="text-sm text-muted-foreground">{backup.createdAt}</span>
                  </td>
                  <td className="px-4 py-4">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon" className="h-8 w-8">
                          <MoreHorizontal className="h-4 w-4" />
                          <span className="sr-only">Open menu</span>
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem className="gap-2">
                          <Download className="h-4 w-4" />
                          Download
                        </DropdownMenuItem>
                        <DropdownMenuItem className="gap-2">
                          <RefreshCw className="h-4 w-4" />
                          Restore
                        </DropdownMenuItem>
                        <DropdownMenuItem className="gap-2 text-destructive">
                          <Trash2 className="h-4 w-4" />
                          Delete
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {filteredBackups.length === 0 && (
          <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
            <HardDrive className="h-12 w-12 mb-4 opacity-50" />
            <p className="text-lg font-medium">No backups found</p>
            <p className="text-sm">Try adjusting your search or filters</p>
          </div>
        )}
      </div>
    </div>
  );
}
