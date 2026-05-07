import { Avatar, AvatarFallback, AvatarImage } from "@renderer/components/ui/avatar";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { cn } from "@renderer/lib/utils";
import type { TFunction } from "i18next";
import { ErrorState } from "../-shared/common";
import { getGameBananaErrorPresentation } from "../-shared/errors";
import type { CategoryChildItem, RootCategoryItem } from "../-types";
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
  onSelectCategory,
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
  onSelectCategory: (categoryId: number, categoryName: string) => void;
  onResetToGameHome: () => void;
}) {
  const categories: Array<RootCategoryItem | CategoryChildItem> = hasCategoryContext
    ? categoryChildren
    : rootCategories;
  const isLoading = hasCategoryContext ? isCategoryOverviewLoading : isGameOverviewLoading;
  const hasError = hasCategoryContext ? categoryOverviewError : gameOverviewError;
  const errorPresentation = getGameBananaErrorPresentation(
    hasCategoryContext ? categoryOverviewErrorObject : gameOverviewErrorObject,
    t,
  );

  return (
    <Card className="flex h-full min-h-0 flex-col p-0">
      <CardHeader className="pt-3">
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle>{t("page.gamebanana.category_panel_title")}</CardTitle>
          </div>
          {hasCategoryContext && (
            <Button variant="ghost" size="sm" onClick={onResetToGameHome}>
              {t("page.gamebanana.root_categories")}
            </Button>
          )}
        </div>
      </CardHeader>
      <CardContent className="min-h-0 flex-1 p-0">
        <ScrollArea className="h-full min-h-0">
          <div className="space-y-2 px-3 pb-3">
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
                      "flex w-full items-center justify-between rounded-xl border px-3 py-2 text-left transition-colors",
                      isActive ? "border-primary bg-primary/8" : "hover:bg-muted/50",
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
      </CardContent>
    </Card>
  );
}
