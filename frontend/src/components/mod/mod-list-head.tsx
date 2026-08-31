import { cn } from "@renderer/lib/utils";
import { useModStore } from "@renderer/store/mod";
import { ArrowDownIcon, ArrowUpIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { MOD_LIST_GRID_TEMPLATE_COLUMNS } from "./mod-list-layout";

export function ListHead() {
  const sortType = useModStore((s) => s.sortType);
  const setSortType = useModStore((s) => s.setSortType);
  const sortOrder = useModStore((s) => s.sortOrder);
  const setSortOrder = useModStore((s) => s.setSortOrder);
  const { t } = useTranslation();

  const handleSort = (field: "name" | "size" | "date") => {
    if (sortType !== field) {
      setSortType(field);
      setSortOrder(field === "name" ? "asc" : "desc");
    } else {
      setSortOrder(sortOrder === "desc" ? "asc" : "desc");
    }
  };

  return (
    <div role="rowgroup" className="shrink-0 bg-background text-sm">
      <div
        role="row"
        className="grid h-8 items-center"
        style={{ gridTemplateColumns: MOD_LIST_GRID_TEMPLATE_COLUMNS }}
      >
        <div role="columnheader" />
        <div role="columnheader" className="min-w-0 pl-3 text-left font-normal">
          <button
            type="button"
            className="flex w-full flex-row items-center justify-start select-none"
            onClick={() => handleSort("name")}
          >
            <div
              className={cn(
                "flex flex-row items-center gap-2",
                sortType === "name" ? "text-primary" : "text-muted-foreground",
              )}
            >
              <p className="whitespace-nowrap">{t("g.name")}</p>
              {sortType === "name" && sortOrder === "desc" && <ArrowDownIcon size="16" />}
              {sortType === "name" && sortOrder === "asc" && <ArrowUpIcon size="16" />}
            </div>
          </button>
        </div>
        <div role="columnheader" className="px-2 font-normal whitespace-nowrap">
          <button
            type="button"
            className="flex w-full flex-row items-center justify-end select-none"
            onClick={() => handleSort("size")}
          >
            <div
              className={cn(
                "flex flex-row items-center justify-end gap-2",
                sortType === "size" ? "text-primary" : "text-muted-foreground",
              )}
            >
              <p className="whitespace-nowrap">{t("g.size")}</p>
              {sortType === "size" && sortOrder === "desc" && <ArrowDownIcon size="16" />}
              {sortType === "size" && sortOrder === "asc" && <ArrowUpIcon size="16" />}
            </div>
          </button>
        </div>
        <div role="columnheader" className="px-2 pr-3 font-normal whitespace-nowrap">
          <button
            type="button"
            className="flex w-full flex-row items-center justify-end select-none"
            onClick={() => handleSort("date")}
          >
            <div
              className={cn(
                "flex flex-row items-center justify-end gap-2",
                sortType === "date" ? "text-primary" : "text-muted-foreground",
              )}
            >
              <p className="whitespace-nowrap">{t("g.date")}</p>
              {sortType === "date" && sortOrder === "desc" && <ArrowDownIcon size="16" />}
              {sortType === "date" && sortOrder === "asc" && <ArrowUpIcon size="16" />}
            </div>
          </button>
        </div>
      </div>
    </div>
  );
}
