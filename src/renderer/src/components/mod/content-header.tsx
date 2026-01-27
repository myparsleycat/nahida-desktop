import { Search, FolderIcon, Loader2Icon, EllipsisIcon } from "lucide-react";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";

import { useModStore } from "@renderer/store/mod";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useCharacters } from "@renderer/hooks/use-mod-data";

export function ContentHeader() {
  const searchValue = useModStore((s) => s.searchQuery);
  const onSearchChange = useModStore((s) => s.setSearchQuery);
  const selectedGame = useModStore((s) => s.selectedGame);
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const queryClient = useQueryClient();

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
        <div className="relative w-[200px]">
          <Input
            className="h-8 pr-8 text-sm"
            placeholder="검색..."
            value={searchValue}
            onChange={(e) => onSearchChange(e.target.value)}
          />
          <Search className="absolute right-2 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
        </div>

        {groupPath && (
          <Button
            variant="ghost"
            size="icon"
            onClick={() => {
              window.api.invoke("util:openPath", groupPath);
            }}
          >
            <FolderIcon />
          </Button>
        )}

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon">
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
