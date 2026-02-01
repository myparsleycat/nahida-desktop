import {
  Search,
  FolderIcon,
  Loader2Icon,
  EllipsisIcon,
  ArrowUpAZ,
  ArrowDownAZ,
  ArrowUp10,
  ArrowDown10,
  ArrowUpWideNarrow,
  ArrowDownWideNarrow,
} from "lucide-react";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Separator } from "@renderer/components/ui/separator";

import { useModStore } from "@renderer/store/mod";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

export function ContentHeader() {
  const searchValue = useModStore((s) => s.searchQuery);
  const onSearchChange = useModStore((s) => s.setSearchQuery);
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const queryClient = useQueryClient();

  const sortType = useModStore((s) => s.sortType);
  const setSortType = useModStore((s) => s.setSortType);
  const sortOrder = useModStore((s) => s.sortOrder);
  const setSortOrder = useModStore((s) => s.setSortOrder);

  const groupName = selectedGroup?.name || "";
  const groupPath = selectedGroup?.path;

  const handleEnableAll = async () => {
    if (!groupPath) return;

    try {
      await window.api.invoke("mod:enableAll", groupPath);
      queryClient.invalidateQueries({ queryKey: ["modGroup", groupPath] });
      toast.success("모든 모드가 활성화되었습니다.");
    } catch (error) {
      toast.error("모드 활성화에 실패했습니다.");
      console.error(error);
    }
  };

  const handleDisableAll = async () => {
    if (!groupPath) return;

    try {
      await window.api.invoke("mod:disableAll", groupPath);
      queryClient.invalidateQueries({ queryKey: ["modGroup", groupPath] });
      toast.success("모든 모드가 비활성화되었습니다.");
    } catch (error) {
      toast.error("모드 비활성화에 실패했습니다.");
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
        {groupName ? (
          <h1 className="text-2xl font-semibold text-foreground">{groupName}</h1>
        ) : (
          <Loader2Icon className="animate-spin" />
        )}
      </div>

      <div className="flex items-center gap-2">
        <div className="relative w-[180px]">
          <Input
            className="h-8 pr-8 text-sm"
            placeholder="검색..."
            value={searchValue}
            onChange={(e) => onSearchChange(e.target.value)}
          />
          <Search className="absolute right-2 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
        </div>

        <Separator orientation="vertical" />

        <div className="flex items-center gap-1">
          <Select value={sortType} onValueChange={handleSortTypeChange}>
            <SelectTrigger className="w-[80px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent position="popper">
              <SelectItem value="name">이름</SelectItem>
              <SelectItem value="date">날짜</SelectItem>
              <SelectItem value="size">크기</SelectItem>
            </SelectContent>
          </Select>

          <Button variant="outline" size="icon" className="h-8 w-8" onClick={toggleSortOrder}>
            {renderSortIcon()}
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
          <DropdownMenuContent>
            <DropdownMenuGroup>
              <DropdownMenuItem onClick={handleEnableAll}>전체 활성화</DropdownMenuItem>
              <DropdownMenuItem onClick={handleDisableAll}>전체 비활성화</DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
