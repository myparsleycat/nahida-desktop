import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { cn } from "@renderer/lib/utils";
import type { TFunction } from "i18next";
import { ErrorState } from "../shared/common";
import type { CategoryChildItem, RootCategoryItem } from "../types";
import { formatNumber } from "../utils";

export function CategorySidebar({
  t,
  language,
  hasCategoryContext,
  isGameOverviewLoading,
  isCategoryOverviewLoading,
  gameOverviewError,
  categoryOverviewError,
  rootCategories,
  categoryChildren,
  selectedCategoryId,
  selectedCategoryName,
  onSelectCategory,
  onResetToGameHome,
}: {
  t: TFunction;
  language: string;
  hasCategoryContext: boolean;
  isGameOverviewLoading: boolean;
  isCategoryOverviewLoading: boolean;
  gameOverviewError: boolean;
  categoryOverviewError: boolean;
  rootCategories: RootCategoryItem[];
  categoryChildren: CategoryChildItem[];
  selectedCategoryId?: number;
  selectedCategoryName?: string;
  onSelectCategory: (categoryId: number, categoryName: string) => void;
  onResetToGameHome: () => void;
}) {
  const categories: Array<RootCategoryItem | CategoryChildItem> = hasCategoryContext
    ? categoryChildren
    : rootCategories;
  const isLoading = hasCategoryContext ? isCategoryOverviewLoading : isGameOverviewLoading;
  const hasError = hasCategoryContext ? categoryOverviewError : gameOverviewError;

  return (
    <Card className="flex h-full min-h-0 flex-col">
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle>{t("page.gamebanana.category_panel_title")}</CardTitle>
            <CardDescription>
              {hasCategoryContext
                ? selectedCategoryName
                  ? t("page.gamebanana.category_panel_description_selected", { name: selectedCategoryName })
                  : t("page.gamebanana.category_panel_description_nested")
                : t("page.gamebanana.category_panel_description_root")}
            </CardDescription>
          </div>
          {hasCategoryContext && (
            <Button variant="ghost" size="sm" onClick={onResetToGameHome}>
              {t("page.gamebanana.root_categories")}
            </Button>
          )}
        </div>
      </CardHeader>
      <CardContent className="min-h-0 flex-1">
        <ScrollArea className="h-full min-h-0 pr-4">
          <div className="space-y-2">
            {isLoading && (
              <>
                <Skeleton className="h-12 w-full" />
                <Skeleton className="h-12 w-full" />
                <Skeleton className="h-12 w-full" />
              </>
            )}
            {hasError && <ErrorState title={t("page.gamebanana.error_title")} />}
            {!isLoading && !hasError && categories.length === 0 && (
              <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
                {t("page.gamebanana.no_categories")}
              </div>
            )}
            {!isLoading &&
              !hasError &&
              categories.map((category) => {
                const isActive = selectedCategoryId === category._idRow;

                return (
                  <button
                    key={category._idRow}
                    type="button"
                    className={cn(
                      "flex w-full items-center justify-between rounded-xl border px-3 py-3 text-left transition-colors",
                      isActive ? "border-primary bg-primary/8" : "hover:bg-muted/50",
                    )}
                    onClick={() => category._idRow && onSelectCategory(category._idRow, category._sName)}
                  >
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
      </CardContent>
    </Card>
  );
}
