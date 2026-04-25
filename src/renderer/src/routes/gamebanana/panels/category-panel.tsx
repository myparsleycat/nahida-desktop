import { Input } from "@renderer/components/ui/input";
import { Card, CardContent, CardHeader } from "@renderer/components/ui/card";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { cn } from "@renderer/lib/utils";
import { SearchIcon } from "lucide-react";
import type { TFunction } from "i18next";
import { ModGridCard } from "../cards/mod-grid-card";
import { ErrorState, OverviewSkeleton, PaginationButtons } from "../shared/common";
import type {
  CategoryOverviewQuery,
  GameBananaSubmissionSelection,
  SubmissionListItem,
} from "../types";

export function CategoryPanel({
  t,
  language,
  categoryOverviewQuery,
  filteredMods,
  modSearch,
  modsPage,
  hasSidebar,
  onChangeModSearch,
  onSelectMod,
  onModsPage,
}: {
  t: TFunction;
  language: string;
  categoryOverviewQuery: CategoryOverviewQuery;
  filteredMods: SubmissionListItem[];
  modSearch: string;
  modsPage: number;
  hasSidebar: boolean;
  onChangeModSearch: (value: string) => void;
  onSelectMod: (submission: GameBananaSubmissionSelection) => void;
  onModsPage: (page: number) => void;
}) {
  return (
    <Card className="flex h-full min-h-0 flex-col">
      <CardHeader>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="text-sm font-semibold">{t("page.gamebanana.mods")}</div>
            <div className="text-xs text-muted-foreground">{t("page.gamebanana.mods_description")}</div>
          </div>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <div className="relative w-full sm:w-64">
              <SearchIcon className="pointer-events-none absolute left-2 top-2.5 size-4 text-muted-foreground" />
              <Input
                className="pl-8"
                value={modSearch}
                onChange={(event) => onChangeModSearch(event.target.value)}
                placeholder={t("page.gamebanana.search_mods")}
              />
            </div>
            <PaginationButtons
              page={modsPage}
              totalPages={
                categoryOverviewQuery.data?.index._aMetadata
                  ? Math.ceil(
                      categoryOverviewQuery.data.index._aMetadata._nRecordCount /
                        categoryOverviewQuery.data.index._aMetadata._nPerpage,
                    )
                  : undefined
              }
              onPrev={() => onModsPage(modsPage - 1)}
              onNext={() => onModsPage(modsPage + 1)}
              onPageChange={onModsPage}
              disablePrev={modsPage <= 1}
              disableNext={Boolean(categoryOverviewQuery.data?.index._aMetadata._bIsComplete)}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="min-h-0 flex-1">
        <ScrollArea className="h-full min-h-0 pr-4">
          <div className="space-y-4">
            {categoryOverviewQuery.isLoading && <OverviewSkeleton />}
            {categoryOverviewQuery.error && <ErrorState title={t("page.gamebanana.error_title")} />}
            {categoryOverviewQuery.data && (
              <section className="space-y-3">
                {filteredMods.length === 0 && (
                  <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
                    {t("page.gamebanana.no_results")}
                  </div>
                )}

                <div
                  className={cn(
                    "grid gap-4",
                    hasSidebar ? "sm:grid-cols-3 2xl:grid-cols-4" : "sm:grid-cols-4 2xl:grid-cols-5",
                  )}
                >
                  {filteredMods.map((mod) => (
                    <ModGridCard
                      key={`mod-${mod._idRow}`}
                      mod={mod}
                      language={language}
                      onClick={() =>
                        onSelectMod({
                          id: mod._idRow,
                          modelName: mod._sModelName,
                        })
                      }
                    />
                  ))}
                </div>
              </section>
            )}
          </div>
        </ScrollArea>
      </CardContent>
    </Card>
  );
}
