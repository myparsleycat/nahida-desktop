import { Titlebar } from "@renderer/components/titlebar";
import {
  type GameBananaGameKey,
  useGameBananaGameOverview,
  useGameBananaGames,
  useGameBananaGameSubfeed,
  useGameBananaModCategoryOverview,
  useGameBananaModOverview,
} from "@renderer/hooks/use-gamebanana-data";
import { cn } from "@renderer/lib/utils";
import { useGameBananaStore } from "@renderer/store/gamebanana";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { GameBananaToolbar } from "./-components/gamebanana-toolbar";
import { CategoryPanel } from "./-panels/category-panel";
import { GameHomePanel } from "./-panels/game-home-panel";
import { ModDetailPanel } from "./-panels/mod-detail-panel";
import { GameBananaAuthState } from "./-shared/common";
import { CategorySidebar } from "./-sidebars/category-sidebar";
import { ModFilesSidebar } from "./-sidebars/mod-files-sidebar";
import type { GameOption } from "./-types";

const EMPTY_GAMES_MAP: Record<string, number> = {};

export const Route = createFileRoute("/gamebanana/")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t, i18n } = useTranslation();
  const [authStatus, setAuthStatus] = useState<"checking" | "ready" | "error">("checking");
  const [authErrorCode, setAuthErrorCode] = useState<string | null>(null);
  const [isLoggingOut, setIsLoggingOut] = useState(false);
  const isAuthReady = authStatus === "ready";
  const {
    data: gamesMap,
    isLoading: isGamesLoading,
    error: gamesError,
  } = useGameBananaGames(isAuthReady);
  const selectedGameKey = useGameBananaStore((state) => state.selectedGame);
  const selectedCategoryId = useGameBananaStore((state) => state.selectedCategoryId);
  const categoryBreadcrumbs = useGameBananaStore((state) => state.categoryBreadcrumbs);
  const selectedMod = useGameBananaStore((state) => state.selectedMod);
  const subfeedPage = useGameBananaStore((state) => state.subfeedPage);
  const modsPage = useGameBananaStore((state) => state.modsPage);
  const modSearch = useGameBananaStore((state) => state.modSearch);
  const setInitialGame = useGameBananaStore((state) => state.setInitialGame);
  const setSelectedGame = useGameBananaStore((state) => state.setSelectedGame);
  const selectCategory = useGameBananaStore((state) => state.selectCategory);
  const selectMod = useGameBananaStore((state) => state.selectMod);
  const clearSelectedMod = useGameBananaStore((state) => state.clearSelectedMod);
  const selectBreadcrumbCategory = useGameBananaStore((state) => state.selectBreadcrumbCategory);
  const resetToGameHome = useGameBananaStore((state) => state.resetToGameHome);
  const setSubfeedPage = useGameBananaStore((state) => state.setSubfeedPage);
  const setModsPage = useGameBananaStore((state) => state.setModsPage);
  const setModSearch = useGameBananaStore((state) => state.setModSearch);

  const games = useMemo<GameOption[]>(
    () =>
      Object.entries(gamesMap ?? EMPTY_GAMES_MAP).map(([key, id]) => ({
        key: key as GameBananaGameKey,
        id,
      })),
    [gamesMap],
  );

  const selectedGame = games.find((game) => game.key === selectedGameKey) ?? games[0];
  const selectedGameId = selectedGame?.id;

  const gameOverviewQuery = useGameBananaGameOverview(selectedGameId, isAuthReady);
  const subfeedQuery = useGameBananaGameSubfeed(selectedGameId, subfeedPage, isAuthReady);
  const categoryOverviewQuery = useGameBananaModCategoryOverview(
    selectedCategoryId,
    modsPage,
    isAuthReady,
  );
  const modOverviewQuery = useGameBananaModOverview(selectedMod, isAuthReady);

  useEffect(() => {
    let cancelled = false;

    const ensureAuthenticated = async () => {
      setAuthStatus("checking");
      setAuthErrorCode(null);

      try {
        await window.api.invoke("gamebanana:ensureAuthenticated");
        if (!cancelled) {
          setAuthStatus("ready");
        }
      } catch (error) {
        if (cancelled) {
          return;
        }

        const code =
          error instanceof Error && error.message === "GAMEBANANA_LOGIN_CANCELLED"
            ? "GAMEBANANA_LOGIN_CANCELLED"
            : "GAMEBANANA_AUTH_FAILED";

        setAuthErrorCode(code);
        setAuthStatus("error");
      }
    };

    void ensureAuthenticated();

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!games.length) return;
    if (!selectedGameKey) {
      setInitialGame(games[0].key);
      return;
    }
    if (!games.some((game) => game.key === selectedGameKey)) {
      setSelectedGame(games[0].key);
    }
  }, [games, selectedGameKey, setInitialGame, setSelectedGame]);

  const rootCategories = gameOverviewQuery.data?.profile._aModRootCategories ?? [];
  const categoryChildren = categoryOverviewQuery.data?.categories ?? [];
  const mods = categoryOverviewQuery.data?.index._aRecords ?? [];
  const filteredMods = useMemo(() => {
    if (!modSearch.trim()) return mods;

    const query = modSearch.trim().toLowerCase();
    return mods.filter((mod) => mod._sName.toLowerCase().includes(query));
  }, [modSearch, mods]);

  const selectedRootCategory = rootCategories.find(
    (category) => category._idRow === selectedCategoryId,
  );
  const hasCategoryContext = Boolean(selectedCategoryId);
  const isViewingMod = Boolean(selectedMod);
  const showCategorySidebar =
    !hasCategoryContext ||
    isViewingMod ||
    categoryOverviewQuery.isLoading ||
    Boolean(categoryOverviewQuery.error) ||
    categoryChildren.length > 0;
  const selectedGameLabel = gameOverviewQuery.isPlaceholderData
    ? (selectedGame?.key.toUpperCase() ?? t("page.gamebanana.title"))
    : (gameOverviewQuery.data?.profile._sName ??
      selectedGame?.key.toUpperCase() ??
      t("page.gamebanana.title"));
  const selectedCategoryName = hasCategoryContext
    ? (categoryOverviewQuery.data?.profile._sName ?? selectedRootCategory?._sName ?? undefined)
    : undefined;
  const resolvedCategoryBreadcrumbs = useMemo(() => {
    if (!hasCategoryContext || !selectedCategoryId) return [];
    if (categoryBreadcrumbs.length === 0) {
      return selectedCategoryName ? [{ id: selectedCategoryId, name: selectedCategoryName }] : [];
    }

    return categoryBreadcrumbs.map((breadcrumb, index) => ({
      ...breadcrumb,
      name:
        index === categoryBreadcrumbs.length - 1
          ? (selectedCategoryName ?? breadcrumb.name)
          : breadcrumb.name,
    }));
  }, [categoryBreadcrumbs, hasCategoryContext, selectedCategoryId, selectedCategoryName]);
  const selectedModName = isViewingMod
    ? (modOverviewQuery.data?.profile._sName ?? undefined)
    : undefined;

  const currentProfileUrl =
    modOverviewQuery.data?.profile._sProfileUrl ??
    categoryOverviewQuery.data?.profile._sProfileUrl ??
    gameOverviewQuery.data?.profile._sProfileUrl;

  const handleOpenGameProfile = () => {
    if (!currentProfileUrl) return;
    void window.api.invoke("util:openExternal", currentProfileUrl);
  };

  const handleRetryAuth = () => {
    setAuthStatus("checking");
    setAuthErrorCode(null);
    void window.api
      .invoke("gamebanana:ensureAuthenticated")
      .then(() => {
        setAuthStatus("ready");
      })
      .catch((error) => {
        const code =
          error instanceof Error && error.message === "GAMEBANANA_LOGIN_CANCELLED"
            ? "GAMEBANANA_LOGIN_CANCELLED"
            : "GAMEBANANA_AUTH_FAILED";
        setAuthErrorCode(code);
        setAuthStatus("error");
      });
  };

  const handleLogout = () => {
    setIsLoggingOut(true);
    void window.api
      .invoke("gamebanana:logout")
      .then(() => {
        setAuthErrorCode("GAMEBANANA_AUTH_FAILED");
        setAuthStatus("error");
      })
      .finally(() => {
        setIsLoggingOut(false);
      });
  };

  if (authStatus === "checking") {
    return (
      <>
        <Titlebar title={{ text: "GameBanana", position: "center" }} />
        <main className="flex h-full flex-1 flex-col overflow-hidden bg-background p-4">
          <GameBananaAuthState
            title={t("page.gamebanana.auth.checking_title")}
            description={t("page.gamebanana.auth.checking_description")}
            pending={true}
          />
        </main>
      </>
    );
  }

  if (authStatus === "error") {
    const isCancelled = authErrorCode === "GAMEBANANA_LOGIN_CANCELLED";
    return (
      <>
        <Titlebar title={{ text: "GameBanana", position: "center" }} />
        <main className="flex h-full flex-1 flex-col overflow-hidden bg-background p-4">
          <GameBananaAuthState
            title={
              isCancelled
                ? t("page.gamebanana.auth.cancelled_title")
                : t("page.gamebanana.auth.required_title")
            }
            description={
              isCancelled
                ? t("page.gamebanana.auth.cancelled_description")
                : t("page.gamebanana.auth.required_description")
            }
            actionLabel={t("page.gamebanana.auth.retry")}
            onAction={handleRetryAuth}
          />
        </main>
      </>
    );
  }

  return (
    <>
      <Titlebar title={{ text: "GameBanana", position: "center" }} />
      <main className="flex h-full flex-1 flex-col overflow-hidden bg-background">
        <div className="lg:hidden flex h-full items-center justify-center p-4">
          <div className="w-full max-w-md rounded-xl border border-dashed p-6 text-center">
            <div className="text-base font-medium">{t("page.gamebanana.narrow_window.title")}</div>
            <div className="mt-2 text-sm text-muted-foreground">
              {t("page.gamebanana.narrow_window.description")}
            </div>
          </div>
        </div>

        <div className="hidden lg:flex lg:flex-col lg:flex-1 lg:overflow-hidden">
          <div className="border-b px-4 py-3">
            <GameBananaToolbar
              games={games}
              selectedGame={selectedGame?.key}
              selectedGameLabel={selectedGameLabel}
              stageLabel={
                isViewingMod
                  ? t("page.gamebanana.stage.mod")
                  : hasCategoryContext
                    ? t("page.gamebanana.stage.category")
                    : t("page.gamebanana.stage.game")
              }
              breadcrumbCategories={resolvedCategoryBreadcrumbs}
              breadcrumbMod={selectedModName}
              isGamesLoading={isGamesLoading}
              gamesError={Boolean(gamesError)}
              onSelectGame={setSelectedGame}
              onOpenGameProfile={handleOpenGameProfile}
              isLoggingOut={isLoggingOut}
              onLogout={handleLogout}
              onBackToCategory={clearSelectedMod}
              onSelectBreadcrumbCategory={selectBreadcrumbCategory}
              canOpenProfile={Boolean(currentProfileUrl)}
              onResetToGameHome={resetToGameHome}
            />
          </div>

          <div
            className={cn(
              "grid min-h-0 flex-1 gap-2 overflow-hidden",
              showCategorySidebar && "lg:grid-cols-[minmax(0,1fr)_320px]",
            )}
          >
            <div className="min-h-0 min-w-0">
              {!hasCategoryContext && !isViewingMod && (
                <GameHomePanel
                  t={t}
                  language={i18n.language}
                  subfeedQuery={subfeedQuery}
                  subfeedPage={subfeedPage}
                  onSubfeedPage={setSubfeedPage}
                  onSelectMod={selectMod}
                />
              )}

              {hasCategoryContext && !isViewingMod && (
                <CategoryPanel
                  t={t}
                  language={i18n.language}
                  categoryOverviewQuery={categoryOverviewQuery}
                  filteredMods={filteredMods}
                  modSearch={modSearch}
                  modsPage={modsPage}
                  hasSidebar={showCategorySidebar}
                  onChangeModSearch={setModSearch}
                  onSelectMod={selectMod}
                  onModsPage={setModsPage}
                />
              )}

              {isViewingMod && (
                <ModDetailPanel
                  t={t}
                  language={i18n.language}
                  selection={selectedMod}
                  modOverviewQuery={modOverviewQuery}
                />
              )}
            </div>

            {showCategorySidebar && (
              <div className="min-h-0 min-w-0 pr-4 py-4">
                {isViewingMod ? (
                  <ModFilesSidebar
                    t={t}
                    language={i18n.language}
                    modOverviewQuery={modOverviewQuery}
                  />
                ) : (
                  <CategorySidebar
                    t={t}
                    language={i18n.language}
                    hasCategoryContext={hasCategoryContext}
                    isGameOverviewLoading={gameOverviewQuery.isLoading}
                    isCategoryOverviewLoading={categoryOverviewQuery.isLoading}
                    gameOverviewError={Boolean(gameOverviewQuery.error)}
                    categoryOverviewError={Boolean(categoryOverviewQuery.error)}
                    rootCategories={rootCategories}
                    categoryChildren={categoryChildren}
                    selectedCategoryId={selectedCategoryId}
                    selectedCategoryName={selectedCategoryName}
                    onSelectCategory={selectCategory}
                    onResetToGameHome={resetToGameHome}
                  />
                )}
              </div>
            )}
          </div>
        </div>
      </main>
    </>
  );
}
