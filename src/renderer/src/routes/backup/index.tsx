import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
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
  FolderIcon,
  FileIcon,
} from "lucide-react";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@renderer/components/ui/dialog";
import { Label } from "@renderer/components/ui/label";
import { useQuery } from "@tanstack/react-query";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { cn } from "@renderer/lib/utils";

export const Route = createFileRoute("/backup/")({
  component: RouteComponent,
});

type BackupStatus = "completed" | "in-progress" | "failed";

interface Backup {
  id: string;
  name: string;
  status: BackupStatus;
  size: string;
  lastUpdated: string;
}

const backups: Backup[] = [
  {
    id: "8JfpicWAW",
    name: "Genshin Mods",
    status: "completed",
    size: "2.4 GB",
    lastUpdated: "12m ago",
  },
  {
    id: "BCoTKPg4n",
    name: "Starail Mods",
    status: "in-progress",
    size: "856 MB",
    lastUpdated: "38m ago",
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

  const [localPath, setLocalPath] = useState("");
  const [selectCloudPathDialogOpen, setSelectCloudPathDialogOpen] = useState(false);
  const [currentId, setCurrentId] = useState<string>("root");
  const [cloudPath, setCloudPath] = useState("");
  const [selectedItemId, setSelectedItemId] = useState<string>("");

  const cloudQuery = useQuery({
    queryKey: ["test-nnn", currentId],
    queryFn: async () => {
      return await window.api.invoke("drive:get:item", currentId);
    },
    enabled: selectCloudPathDialogOpen,
  });

  useEffect(() => {
    if (!selectCloudPathDialogOpen) {
      setCurrentId("root");
      setSelectedItemId("");
    }
  }, [selectCloudPathDialogOpen]);

  return (
    <div className="flex flex-col gap-6 p-6">
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
          <Dialog>
            <form>
              <DialogTrigger asChild>
                <Button className="gap-2">
                  <Plus className="h-4 w-4" />
                  백업 생성
                </Button>
              </DialogTrigger>
              <DialogContent
                showCloseButton={false}
                onCloseAutoFocus={(e) => e.preventDefault()}
                onPointerDownOutside={(e) => e.preventDefault()}
              >
                <DialogHeader>
                  <DialogTitle>백업 생성</DialogTitle>
                </DialogHeader>
                <div className="grid gap-4">
                  <div className="grid gap-2">
                    <Label htmlFor="backupName">백업 이름</Label>
                    <Input id="backupName" name="backupName" />
                  </div>

                  <div className="grid gap-2">
                    <Label htmlFor="localPath">로컬 경로</Label>
                    <div className="flex gap-2">
                      <Input value={localPath} readOnly />
                      <Button
                        variant="outline"
                        onClick={async () => {
                          const result = await window.api.invoke("util:showOpenDialog", {
                            properties: ["openDirectory"],
                          });
                          if (result.canceled) return;
                          setLocalPath(result.filePaths[0]);
                        }}
                      >
                        선택
                      </Button>
                    </div>
                  </div>

                  <div className="grid gap-2">
                    <Label htmlFor="cloudPath">클라우드 경로</Label>
                    <div className="flex gap-2">
                      <Input value={cloudPath} readOnly />
                      <Dialog
                        open={selectCloudPathDialogOpen}
                        onOpenChange={setSelectCloudPathDialogOpen}
                      >
                        <DialogTrigger>
                          <Button variant="outline">선택</Button>
                        </DialogTrigger>

                        <DialogContent
                          showCloseButton={false}
                          onCloseAutoFocus={(e) => e.preventDefault()}
                          onPointerDownOutside={(e) => e.preventDefault()}
                        >
                          <div className="flex flex-col h-[500px] w-full overflow-hidden">
                            {cloudQuery.isLoading && (
                              <div className="flex flex-1 items-center justify-center">
                                <Loader2 className="h-6 w-6 animate-spin" />
                              </div>
                            )}
                            {cloudQuery.isError && (
                              <div className="flex flex-1 items-center justify-center text-destructive">
                                에러가 발생했습니다.
                              </div>
                            )}

                            {cloudQuery.data && (
                              <div className="flex flex-col flex-1 min-h-0 w-full overflow-hidden">
                                <div className="flex flex-wrap items-center gap-y-1 py-2 text-sm shrink-0 w-full">
                                  <span
                                    className="cursor-pointer hover:underline text-muted-foreground hover:text-foreground shrink-0"
                                    onClick={() => setCurrentId("root")}
                                  >
                                    드라이브
                                  </span>
                                  {cloudQuery.data.ancestors.map((item) => (
                                    <div key={item.id} className="flex items-center min-w-0">
                                      <span className="mx-1 text-muted-foreground shrink-0">/</span>
                                      <span
                                        className="cursor-pointer hover:underline truncate max-w-[120px]"
                                        onClick={() => {
                                          setCurrentId(item.id);
                                        }}
                                      >
                                        {item.name}
                                      </span>
                                    </div>
                                  ))}
                                </div>

                                <ScrollArea className="flex-1 min-h-0 w-full border rounded-md">
                                  <div className="flex flex-col p-1 space-y-1">
                                    {cloudQuery.data.children.map((item) => (
                                      <div
                                        className={cn(
                                          "p-2 hover:bg-secondary rounded-lg cursor-pointer transition-colors grid grid-cols-[auto_minmax(0,1fr)] items-center gap-2",
                                          selectedItemId === item.id && "bg-secondary",
                                          !item.isDir && "text-muted-foreground",
                                        )}
                                        key={item.id}
                                        onClick={() => {
                                          if (!item.isDir) return;
                                          setSelectedItemId(item.id);
                                        }}
                                        onDoubleClick={() => {
                                          if (!item.isDir) return;
                                          setCurrentId(item.id);
                                        }}
                                      >
                                        <div className="flex shrink-0">
                                          {item.isDir ? (
                                            <FolderIcon className="h-4 w-4" />
                                          ) : (
                                            <FileIcon className="h-4 w-4" />
                                          )}
                                        </div>
                                        <span className="truncate block">{item.name}</span>
                                      </div>
                                    ))}
                                  </div>
                                </ScrollArea>
                              </div>
                            )}

                            <div className="flex justify-end space-x-2 pt-4 shrink-0">
                              <DialogClose asChild>
                                <Button variant="outline">취소</Button>
                              </DialogClose>
                              <Button
                                disabled={!selectedItemId}
                                onClick={() => {
                                  const cloudPath =
                                    "/" +
                                    cloudQuery.data?.ancestors.map((item) => item.name).join("/");
                                  setCloudPath(cloudPath);
                                  setSelectCloudPathDialogOpen(false);
                                }}
                              >
                                선택
                              </Button>
                            </div>
                          </div>
                        </DialogContent>
                      </Dialog>
                    </div>
                  </div>
                </div>
                <DialogFooter>
                  <DialogClose asChild>
                    <Button variant="outline">취소</Button>
                  </DialogClose>
                  <Button type="submit">생성</Button>
                </DialogFooter>
              </DialogContent>
            </form>
          </Dialog>
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
                  Last Updated
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
                    <span className="text-sm text-muted-foreground">{backup.lastUpdated}</span>
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
