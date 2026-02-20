import { cn } from "@renderer/lib/utils";
import { useModStore } from "@renderer/store/mod";
import { ArrowDownIcon, ArrowUpIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

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
    <thead className="sticky top-0 bg-background text-sm z-10">
      <tr className="h-8">
        <th className="w-[40px]"></th>
        <th className="pl-3 font-normal text-left align-middle w-full">
          <button
            type="button"
            className="flex flex-row items-center w-full justify-start select-none"
            onClick={() => handleSort("name")}
          >
            <div
              className={cn(
                "flex flex-row gap-2 items-center",
                sortType === "name" ? "text-primary" : "text-muted-foreground",
              )}
            >
              <p className="whitespace-nowrap">{t("g.name")}</p>
              {sortType === "name" && sortOrder === "desc" && <ArrowDownIcon size="16" />}
              {sortType === "name" && sortOrder === "asc" && <ArrowUpIcon size="16" />}
            </div>
          </button>
        </th>
        <th className="px-2 font-normal align-middle whitespace-nowrap w-[1%]">
          <button
            type="button"
            className="flex flex-row items-center w-full justify-end select-none"
            onClick={() => handleSort("size")}
          >
            <div
              className={cn(
                "flex flex-row gap-2 items-center justify-end",
                sortType === "size" ? "text-primary" : "text-muted-foreground",
              )}
            >
              <p className="whitespace-nowrap">{t("g.size")}</p>
              {sortType === "size" && sortOrder === "desc" && <ArrowDownIcon size="16" />}
              {sortType === "size" && sortOrder === "asc" && <ArrowUpIcon size="16" />}
            </div>
          </button>
        </th>
        <th className="px-2 pr-3 font-normal align-middle whitespace-nowrap w-[1%]">
          <button
            type="button"
            className="flex flex-row items-center w-full justify-end select-none"
            onClick={() => handleSort("date")}
          >
            <div
              className={cn(
                "flex flex-row gap-2 items-center justify-end",
                sortType === "date" ? "text-primary" : "text-muted-foreground",
              )}
            >
              <p className="whitespace-nowrap">{t("g.date")}</p>
              {sortType === "date" && sortOrder === "desc" && <ArrowDownIcon size="16" />}
              {sortType === "date" && sortOrder === "asc" && <ArrowUpIcon size="16" />}
            </div>
          </button>
        </th>
      </tr>
    </thead>
  );
}
