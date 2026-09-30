import { Avatar, AvatarFallback, AvatarImage } from "@renderer/components/ui/avatar";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { cn } from "@renderer/lib/utils";
import type { TFunction } from "i18next";
import { SearchIcon } from "lucide-react";

import type { CategoryChildItem, RootCategoryItem } from "../-types";

import { ErrorState } from "../-shared/common";
import { getGameBananaErrorPresentation } from "../-shared/errors";
import { formatNumber } from "../-utils";

export function CategorySidebar({
  t,
  language,
  hasCategoryContext,
  isGameOverviewLoading,
  isCategoryOverviewLoading,
  gameOverviewError,
  gameOverviewErrorObject,
  categoryOverviewError,
  categoryOverviewErrorObject,
  rootCategories,
  categoryChildren,
  selectedCategoryId,
  categorySearch,
  onSelectCategory,
  onChangeCategorySearch,
  onResetToGameHome,
}: {
  t: TFunction;
  language: string;
  hasCategoryContext: boolean;
  isGameOverviewLoading: boolean;
  isCategoryOverviewLoading: boolean;
  gameOverviewError: boolean;
  gameOverviewErrorObject?: unknown;
  categoryOverviewError: boolean;
  categoryOverviewErrorObject?: unknown;
  rootCategories: RootCategoryItem[];
  categoryChildren: CategoryChildItem[];
  selectedCategoryId?: number;
  categorySearch: string;
  onSelectCategory: (categoryId: number, categoryName: string) => void;
  onChangeCategorySearch: (value: string) => void;
  onResetToGameHome: () => void;
}) {
  const categories = hasCategoryContext ? categoryChildren : rootCategories;
  const normalizedCategorySearch = categorySearch.trim().toLocaleLowerCase(language);
  const filteredCategories = normalizedCategorySearch
    ? categories.filter((category) =>
        category._sName.toLocaleLowerCase(language).includes(normalizedCategorySearch),
      )
    : categories;
  const isLoading = hasCategoryContext ? isCategoryOverviewLoading : isGameOverviewLoading;
  const hasError = hasCategoryContext ? categoryOverviewError : gameOverviewError;
  const errorPresentation = getGameBananaErrorPresentation(
    hasCategoryContext ? categoryOverviewErrorObject : gameOverviewErrorObject,
    t,
  );

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="shrink-0 p-3">
        <div className="flex min-h-7 items-center justify-between gap-3">
          <h2 className="px-1 text-sm font-medium text-muted-foreground">
            {t("page.gamebanana.category_panel_title")}
          </h2>
          {hasCategoryContext && (
            <Button variant="ghost" size="sm" onClick={onResetToGameHome}>
              {t("page.gamebanana.root_categories")}
            </Button>
          )}
        </div>
        <div className="relative mt-3">
          <SearchIcon className="pointer-events-none absolute top-2.5 left-3 size-4 text-muted-foreground" />
          <Input
            value={categorySearch}
            onChange={(event) => onChangeCategorySearch(event.target.value)}
            placeholder={t("page.gamebanana.search_categories")}
            aria-label={t("page.gamebanana.search_categories")}
            className="pl-9"
          />
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1">
        <div className="space-y-0.5 px-3 pb-3">
          {isLoading && (
            <>
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
            </>
          )}
          {hasError && (
            <ErrorState
              title={t("page.gamebanana.error_title")}
              description={errorPresentation.description}
              details={errorPresentation.details}
            />
          )}
          {!isLoading && !hasError && filteredCategories.length === 0 && (
            <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
              {categorySearch.trim()
                ? t("page.gamebanana.no_category_results")
                : t("page.gamebanana.no_categories")}
            </div>
          )}
          {!isLoading &&
            !hasError &&
            filteredCategories.map((category) => {
              const isActive = selectedCategoryId === category._idRow;

              return (
                <button
                  key={category._idRow}
                  type="button"
                  className={cn(
                    "flex w-full items-center justify-between rounded-md px-3 py-2 text-left transition-colors",
                    isActive ? "bg-primary/10 text-primary" : "hover:bg-muted/50",
                  )}
                  onClick={() =>
                    category._idRow && onSelectCategory(category._idRow, category._sName)
                  }
                >
                  <Avatar size="lg">
                    <AvatarImage src={category._sIconUrl} />
                    <AvatarFallback>Icon</AvatarFallback>
                  </Avatar>
                  <span className="truncate text-sm font-medium">{category._sName}</span>
                  {"_nItemCount" in category && typeof category._nItemCount === "number" && (
                    <span className="text-xs text-muted-foreground">
                      {formatNumber(category._nItemCount, language)}
                    </span>
                  )}
                </button>
              );
            })}
        </div>
      </ScrollArea>
    </div>
  );
}
