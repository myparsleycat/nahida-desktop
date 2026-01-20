import { Search, FolderIcon, Loader2Icon } from "lucide-react";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";

import { useModStore } from "@renderer/store/mod";

interface ContentHeaderProps {
  groupName: string;
  groupPath?: string;
}

export function ContentHeader({ groupName, groupPath }: ContentHeaderProps) {
  const searchValue = useModStore((s) => s.searchQuery);
  const onSearchChange = useModStore((s) => s.setSearchQuery);
  return (
    <div className="flex items-center justify-between h-12 px-3 border-[#2a2a2a] border-b">
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
            className="bg-[#2a2a2a] border-[#3a3a3a] h-8 pr-8 text-sm"
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
            className="h-8 w-8 hover:bg-[#2a2a2a]"
            onClick={() => {
              window.api.invoke("util:openPath", groupPath);
            }}
          >
            <FolderIcon />
          </Button>
        )}
      </div>
    </div>
  );
}
