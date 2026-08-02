import { Titlebar } from "@renderer/components/titlebar";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import {
  type GameBananaGameKey,
  useGameBananaGameOverview,
  useGameBananaGames,
  useGameBananaGameSubfeed,
  useGameBananaModCategoryOverview,
  useGameBananaModOverview,
} from "@renderer/hooks/use-gamebanana-data";
import { useGames } from "@renderer/hooks/use-mod-data";
import { cn } from "@renderer/lib/utils";
import { gameBananaStore, useGameBananaStore } from "@renderer/store/gamebanana";
import { modStore } from "@renderer/store/mod";
import { getGameBananaKeyForImporter } from "@shared/mod";
import { createFileRoute } from "@tanstack/react-router";
import { Loader2Icon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import isURL from "validator/lib/isURL";

import type { GameOption } from "./-types";

import { GameBananaToolbar } from "./-components/gamebanana-toolbar";
import { CategoryPanel } from "./-panels/category-panel";
import { GameHomePanel } from "./-panels/game-home-panel";
import { ModDetailPanel } from "./-panels/mod-detail-panel";
import { GameBananaAuthState } from "./-shared/common";
import { CategorySidebar } from "./-sidebars/category-sidebar";
import { ModFilesSidebar } from "./-sidebars/mod-files-sidebar";

const EMPTY_GAMES_MAP: Record<string, number> = {};

export const Route = createFileRoute("/gamebanana/")({
  validateSearch: (search: Record<string, unknown>) => {
    const mod =
      typeof search.mod === "number"
        ? search.mod
        : typeof search.mod === "string" && /^\d+$/.test(search.mod)
          ? Number(search.mod)
          : undefined;

    return mod !== undefined && mod > 0 && Number.isSafeInteger(mod) ? { mod } : {};
  },
  component: RouteComponent,
});

function parseGameBananaModUrl(value: string) {
  const trimmedValue = value.trim();
  if (
    !isURL(trimmedValue, {
      protocols: ["http", "https"],
      require_protocol: true,
      host_whitelist: ["gamebanana.com", "www.gamebanana.com"],
      disallow_auth: true,
      allow_fragments: true,
      allow_query_components: true,
    })
  ) {
    return undefined;
  }

  const url = new URL(trimmedValue);
  const match = /^\/mods\/(\d+)\/?$/i.exec(url.pathname);
  if (!match) return undefined;

  return {
    id: Number(match[1]),
    modelName: "Mod",
  };
}

function RouteComponent() {
  const { t, i18n } = useTranslation();
  const { mod: deepLinkedModId } = Route.useSearch();
  const navigate = Route.useNavigate();
  const [authStatus, setAuthStatus] = useState<"checking" | "ready" | "error">("checking");
  const [authErrorCode, setAuthErrorCode] = useState<string | null>(null);
  const [isLoggingOut, setIsLoggingOut] = useState(false);
  const [isManualRmcDialogOpen, setIsManualRmcDialogOpen] = useState(false);
  const [manualRmcValue, setManualRmcValue] = useState("");
  const [manualRmcError, setManualRmcError] = useState<string | null>(null);
  const [isSavingManualRmc, setIsSavingManualRmc] = useState(false);
  const [modUrlValue, setModUrlValue] = useState("");
  const [modUrlError, setModUrlError] = useState<string | null>(null);
  const isAuthReady = authStatus === "ready";
  const {
    data: gamesMap,
    isLoading: isGamesLoading,
    error: gamesError,
  } = useGameBananaGames(isAuthReady);
  const { data: modGames = [], isLoading: isModGamesLoading } = useGames();
  const selectedGameKey = useGameBananaStore((state) => state.selectedGame);
  const selectedCategoryId = useGameBananaStore((state) => state.selectedCategoryId);
  const categoryBreadcrumbs = useGameBananaStore((state) => state.categoryBreadcrumbs);
  const selectedMod = useGameBananaStore((state) => state.selectedMod);
  const subfeedPage = useGameBananaStore((state) => state.subfeedPage);
  const modsPage = useGameBananaStore((state) => state.modsPage);
  const modSearch = useGameBananaStore((state) => state.modSearch);
  const modsSort = useGameBananaStore((state) => state.modsSort);
  const isModUrlOpen = useGameBananaStore((state) => state.isModUrlOpen);
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
  const setModsSort = useGameBananaStore((state) => state.setModsSort);
  const toggleModUrl = useGameBananaStore((state) => state.toggleModUrl);

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
    modsSort,
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

  useEffect(() => {
    if (!games.length || isModGamesLoading) return;
    if (!gameBananaStore.getState().consumeModGameSync()) return;
    if (!modGames.length) return;

    const modSelectedGame = modStore.getState().selectedGame;
    if (!modSelectedGame) return;

    const modGameConfig = modGames.find((game) => game.game === modSelectedGame);
    const gameBananaKey = getGameBananaKeyForImporter(modGameConfig?.importer);
    if (!gameBananaKey || !games.some((game) => game.key === gameBananaKey)) return;

    setSelectedGame(gameBananaKey as GameBananaGameKey);
  }, [games, modGames, isModGamesLoading, setSelectedGame]);

  useEffect(() => {
    if (!isAuthReady || !deepLinkedModId) return;

    selectMod({ id: deepLinkedModId, modelName: "Mod" });
    void navigate({
      replace: true,
      search: (previous) => ({ ...previous, mod: undefined }),
    });
  }, [deepLinkedModId, isAuthReady, navigate, selectMod]);

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

  const handleToggleModUrl = () => {
    setModUrlError(null);
    toggleModUrl();
  };

  const handleOpenModUrl = () => {
    const selection = parseGameBananaModUrl(modUrlValue);
    if (!selection) {
      setModUrlError(t("page.gamebanana.open_mod_url.invalid"));
      return;
    }

    selectMod(selection);
    setModUrlValue("");
    setModUrlError(null);
  };

  const canGoBack = isViewingMod || hasCategoryContext;
  const handleGoBack = () => {
    if (isViewingMod) {
      clearSelectedMod();
      return;
    }

    if (!hasCategoryContext) {
      return;
    }

    if (resolvedCategoryBreadcrumbs.length > 1) {
      selectBreadcrumbCategory(resolvedCategoryBreadcrumbs.length - 2);
      return;
    }

    resetToGameHome();
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

  const handleOpenManualRmcDialog = () => {
    setManualRmcError(null);
    setIsManualRmcDialogOpen(true);
  };

  const handleSaveManualRmc = () => {
    const nextValue = manualRmcValue.trim();
    if (!nextValue) {
      setManualRmcError(t("page.gamebanana.auth.manual_rmc.empty"));
      return;
    }

    setIsSavingManualRmc(true);
    setManualRmcError(null);
    void window.api
      .invoke("gamebanana:setManualRmcToken", nextValue)
      .then((result) => {
        if (result.ok === false) {
          setManualRmcError(t(`page.gamebanana.auth.manual_rmc.${result.errorCode}`));
          return;
        }

        setIsManualRmcDialogOpen(false);
        setManualRmcValue("");
        setAuthErrorCode(null);
        setAuthStatus("ready");
      })
      .catch((error) => {
        setManualRmcError(
          t(
            error instanceof Error &&
              [
                "GAMEBANANA_INVALID_RMC",
                "GAMEBANANA_SERVER_UNREACHABLE",
                "GAMEBANANA_MANUAL_RMC_SAVE_FAILED",
              ].includes(error.message)
              ? `page.gamebanana.auth.manual_rmc.${error.message}`
              : "page.gamebanana.auth.manual_rmc.GAMEBANANA_MANUAL_RMC_SAVE_FAILED",
          ),
        );
      })
      .finally(() => {
        setIsSavingManualRmc(false);
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
            extraAction={
              <Button
                variant="outline"
                onClick={handleOpenManualRmcDialog}
                disabled={isSavingManualRmc}
              >
                {t("page.gamebanana.auth.manual_rmc.button")}
              </Button>
            }
          />
          <Dialog
            open={isManualRmcDialogOpen}
            onOpenChange={(open) => {
              setIsManualRmcDialogOpen(open);
              if (!open) {
                setManualRmcError(null);
              }
            }}
          >
            <DialogContent>
              <DialogHeader>
                <DialogTitle>{t("page.gamebanana.auth.manual_rmc.title")}</DialogTitle>
                <DialogDescription>
                  {t("page.gamebanana.auth.manual_rmc.description")}
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-2">
                <Input
                  value={manualRmcValue}
                  onChange={(event) => setManualRmcValue(event.target.value)}
                  disabled={isSavingManualRmc}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") {
                      event.preventDefault();
                      handleSaveManualRmc();
                    }
                  }}
                />
                {manualRmcError && <p className="text-sm text-destructive">{manualRmcError}</p>}
              </div>
              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => setIsManualRmcDialogOpen(false)}
                  disabled={isSavingManualRmc}
                >
                  {t("g.cancel")}
                </Button>
                <Button onClick={handleSaveManualRmc} disabled={isSavingManualRmc}>
                  {isSavingManualRmc ? <Loader2Icon className="size-4 animate-spin" /> : null}
                  {t("g.save")}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </main>
      </>
    );
  }

  return (
    <>
      <Titlebar title={{ text: "GameBanana", position: "center" }} />
      <main className="flex h-full flex-1 flex-col overflow-hidden bg-background">
        <div className="flex h-full items-center justify-center p-4 lg:hidden">
          <div className="w-full max-w-md rounded-xl border border-dashed p-6 text-center">
            <div className="text-base font-medium">{t("page.gamebanana.narrow_window.title")}</div>
            <div className="mt-2 text-sm text-muted-foreground">
              {t("page.gamebanana.narrow_window.description")}
            </div>
          </div>
        </div>

        <div className="hidden lg:flex lg:flex-1 lg:flex-col lg:overflow-hidden">
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
              isModUrlOpen={isModUrlOpen}
              onToggleModUrl={handleToggleModUrl}
              onOpenGameProfile={handleOpenGameProfile}
              isLoggingOut={isLoggingOut}
              onLogout={handleLogout}
              canGoBack={canGoBack}
              onGoBack={handleGoBack}
              onBackToCategory={clearSelectedMod}
              onSelectBreadcrumbCategory={selectBreadcrumbCategory}
              canOpenProfile={Boolean(currentProfileUrl)}
              onResetToGameHome={resetToGameHome}
            />
            {isModUrlOpen && (
              <form
                className="mt-3 flex items-start gap-2 border-t pt-3"
                onSubmit={(event) => {
                  event.preventDefault();
                  handleOpenModUrl();
                }}
              >
                <div className="min-w-0 flex-1 space-y-1">
                  <Input
                    value={modUrlValue}
                    placeholder={t("page.gamebanana.open_mod_url.placeholder")}
                    onChange={(event) => setModUrlValue(event.target.value)}
                    aria-label={t("page.gamebanana.open_mod_url.title")}
                    autoFocus
                  />
                  {modUrlError && <p className="text-sm text-destructive">{modUrlError}</p>}
                </div>
                <Button type="submit">{t("page.gamebanana.open_mod_url.confirm")}</Button>
              </form>
            )}
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
                  modsSort={modsSort}
                  hasSidebar={showCategorySidebar}
                  onChangeModSearch={setModSearch}
                  onSelectMod={selectMod}
                  onModsPage={setModsPage}
                  onModsSort={setModsSort}
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
              <div className="min-h-0 min-w-0 py-4 pr-4">
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
                    gameOverviewErrorObject={gameOverviewQuery.error}
                    categoryOverviewError={Boolean(categoryOverviewQuery.error)}
                    categoryOverviewErrorObject={categoryOverviewQuery.error}
                    rootCategories={rootCategories}
                    categoryChildren={categoryChildren}
                    selectedCategoryId={selectedCategoryId}
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
