import { Card, CardContent, CardHeader } from "@renderer/components/ui/card";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { cn } from "@renderer/lib/utils";
import type { TFunction } from "i18next";
import { SearchIcon } from "lucide-react";
import { ModGridCard } from "../-cards/mod-grid-card";
import { ErrorState, OverviewSkeleton, PaginationButtons } from "../-shared/common";
import { getGameBananaErrorPresentation } from "../-shared/errors";
import type {
  CategoryOverviewQuery,
  GameBananaSubmissionSelection,
  SubmissionListItem,
} from "../-types";

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
  const errorPresentation = getGameBananaErrorPresentation(categoryOverviewQuery.error, t);
  const metadata = categoryOverviewQuery.data?.index._aMetadata;
  const hasCategoryData = categoryOverviewQuery.data != null;
  const isSearching = modSearch.trim().length > 0;
  const serverTotalPages = metadata
    ? Math.ceil(metadata._nRecordCount / metadata._nPerpage)
    : undefined;
  const paginationPage = isSearching ? 1 : modsPage;
  const paginationTotalPages = isSearching ? 1 : serverTotalPages;
  const disablePrev = !hasCategoryData || isSearching || modsPage <= 1;
  const disableNext = !hasCategoryData || isSearching || Boolean(metadata?._bIsComplete);

  const handleModsPageChange = (page: number) => {
    if (!hasCategoryData || isSearching) {
      return;
    }

    onModsPage(page);
  };

  return (
    <>
      <ScrollArea
        className="h-full min-h-0 min-w-0"
        viewportClassName="overflow-x-hidden [&>div]:!block [&>div]:!min-w-0 [&>div]:!w-full [&>div]:max-w-full"
      >
        <div className="min-w-0 max-w-full space-y-4 p-4">
          <Card className="flex h-full min-h-0 flex-col">
            <CardHeader>
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div></div>
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
                    page={paginationPage}
                    totalPages={paginationTotalPages}
                    onPrev={() => handleModsPageChange(modsPage - 1)}
                    onNext={() => handleModsPageChange(modsPage + 1)}
                    onPageChange={handleModsPageChange}
                    disablePrev={disablePrev}
                    disableNext={disableNext}
                  />
                </div>
              </div>
            </CardHeader>
            <CardContent className="min-h-0 flex-1">
              <ScrollArea className="h-full min-h-0 pr-4">
                <div className="space-y-4">
                  {categoryOverviewQuery.isLoading && <OverviewSkeleton />}
                  {categoryOverviewQuery.error && (
                    <ErrorState
                      title={t("page.gamebanana.error_title")}
                      description={errorPresentation.description}
                      details={errorPresentation.details}
                    />
                  )}
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
                          hasSidebar
                            ? "sm:grid-cols-3 2xl:grid-cols-4"
                            : "sm:grid-cols-4 2xl:grid-cols-5",
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
        </div>
      </ScrollArea>
    </>
  );
}
