import { Button } from "@renderer/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Separator } from "@renderer/components/ui/separator";
import { useModStore } from "@renderer/store/mod";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowDown10,
  ArrowDownAZ,
  ArrowDownWideNarrow,
  ArrowUp10,
  ArrowUpAZ,
  ArrowUpWideNarrow,
  CircleIcon,
  CircleOffIcon,
  DownloadIcon,
  EllipsisIcon,
  FolderIcon,
  LayoutGridIcon,
  ListIcon,
  Search,
  WrenchIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import wuwaModFixerIcon from "@/renderer/assets/img/wuwa-mod-fixer-icon.png";

interface ContentHeaderProps {
  showWuwaFixer: boolean;
  handleOpenWuwaFixer: (path: string) => Promise<void>;
  isPreparing: boolean;
}

export function ContentHeader({
  showWuwaFixer,
  handleOpenWuwaFixer,
  isPreparing,
}: ContentHeaderProps) {
  const { t } = useTranslation();

  const searchValue = useModStore((s) => s.searchQuery);
  const onSearchChange = useModStore((s) => s.setSearchQuery);
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const setIsCustomDownloadDialogOpen = useModStore((s) => s.setIsCustomDownloadDialogOpen);
  const queryClient = useQueryClient();

  const sortType = useModStore((s) => s.sortType);
  const setSortType = useModStore((s) => s.setSortType);
  const sortOrder = useModStore((s) => s.sortOrder);
  const setSortOrder = useModStore((s) => s.setSortOrder);

  const viewMode = useModStore((s) => s.viewMode);
  const setViewMode = useModStore((s) => s.setViewMode);

  const groupName = selectedGroup?.name || "";
  const groupPath = selectedGroup?.path;
  const hasSelectedGroup = Boolean(groupPath);

  const handleEnableAll = async () => {
    if (!groupPath) return;

    try {
      await window.api.invoke("mod:enableAll", groupPath);
      queryClient.invalidateQueries({ queryKey: ["modGroup", groupPath] });
      toast.success(t("page.mod.content-header.all_enabled"));
    } catch (error) {
      toast.error((error as Error).message);
      console.error(error);
    }
  };

  const handleDisableAll = async () => {
    if (!groupPath) return;

    try {
      await window.api.invoke("mod:disableAll", groupPath);
      queryClient.invalidateQueries({ queryKey: ["modGroup", groupPath] });
      toast.success(t("page.mod.content-header.all_disabled"));
    } catch (error) {
      toast.error((error as Error).message);
      console.error(error);
    }
  };

  const toggleSortOrder = () => {
    setSortOrder(sortOrder === "asc" ? "desc" : "asc");
  };

  const renderSortIcon = () => {
    if (sortType === "name") {
      return sortOrder === "asc" ? <ArrowUpAZ size={16} /> : <ArrowDownAZ size={16} />;
    }
    if (sortType === "date") {
      return sortOrder === "asc" ? <ArrowUp10 size={16} /> : <ArrowDown10 size={16} />;
    }
    if (sortType === "size") {
      return sortOrder === "asc" ? (
        <ArrowUpWideNarrow size={16} />
      ) : (
        <ArrowDownWideNarrow size={16} />
      );
    }
    return null;
  };
  const handleSortTypeChange = (type: "name" | "date" | "size") => {
    setSortType(type);
    if (type === "name") {
      setSortOrder("asc");
    } else {
      setSortOrder("desc");
    }
  };

  return (
    <div className="flex items-center justify-between h-12 px-3 border-b z-20">
      <div className="flex items-center gap-3">
        {groupName && <h1 className="text-2xl font-semibold text-foreground">{groupName}</h1>}
      </div>

      <div className="flex items-center gap-2">
        <div className="relative w-45">
          <Input
            id="mod-search-input"
            className="h-8 pr-8 text-sm"
            placeholder={t("g.search")}
            value={searchValue}
            onChange={(e) => onSearchChange(e.target.value)}
          />
          <Search className="absolute right-2 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
        </div>

        <Separator orientation="vertical" />

        <div className="flex items-center gap-1">
          <Select value={sortType} onValueChange={handleSortTypeChange}>
            <SelectTrigger className="w-20">
              <SelectValue />
            </SelectTrigger>
            <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
              <SelectGroup>
                <SelectItem value="name">{t("g.name")}</SelectItem>
                <SelectItem value="date">{t("g.date")}</SelectItem>
                <SelectItem value="size">{t("g.size")}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>

          <Button variant="outline" size="icon" className="h-8 w-8" onClick={toggleSortOrder}>
            {renderSortIcon()}
          </Button>

          <Separator orientation="vertical" />

          <Button
            variant="outline"
            size="icon"
            className="h-8 w-8 ml-1"
            onClick={() => {
              setViewMode(viewMode === "grid" ? "list" : "grid");
            }}
          >
            {viewMode === "grid" ? <ListIcon size={16} /> : <LayoutGridIcon size={16} />}
          </Button>
        </div>

        <Separator orientation="vertical" />

        {groupPath && (
          <Button
            variant="outline"
            size="icon"
            className="h-8 w-8 ml-1"
            onClick={() => {
              window.api.invoke("util:openPath", groupPath);
            }}
          >
            <FolderIcon />
          </Button>
        )}

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="icon">
              <EllipsisIcon />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent onCloseAutoFocus={(e) => e.preventDefault()}>
            <DropdownMenuGroup>
              <DropdownMenuItem disabled={!hasSelectedGroup} onClick={handleEnableAll}>
                <CircleIcon />
                {t("page.mod.all_enabled")}
              </DropdownMenuItem>
              <DropdownMenuItem disabled={!hasSelectedGroup} onClick={handleDisableAll}>
                <CircleOffIcon />
                {t("page.mod.all_disabled")}
              </DropdownMenuItem>
            </DropdownMenuGroup>

            <DropdownMenuSeparator />

            <DropdownMenuItem
              disabled={!hasSelectedGroup}
              onClick={() => {
                setIsCustomDownloadDialogOpen(true);
              }}
            >
              <DownloadIcon />
              {t("g.download")}
            </DropdownMenuItem>

            {showWuwaFixer && hasSelectedGroup && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <WrenchIcon className="h-4 w-4" />
                    {t("page.mod.content-header.tools")}
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    <DropdownMenuItem
                      disabled={isPreparing}
                      onClick={() => void handleOpenWuwaFixer(groupPath!)}
                    >
                      <img src={wuwaModFixerIcon} className="size-4" />
                      {t("page.mod.content-header.wuwa-mod-fixer")}
                    </DropdownMenuItem>
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
