import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@renderer/components/ui/breadcrumb";
import { Button } from "@renderer/components/ui/button";
import { ButtonGroup, ButtonGroupText } from "@renderer/components/ui/button-group";
import {
  Menubar,
  MenubarContent,
  MenubarMenu,
  MenubarRadioGroup,
  MenubarRadioItem,
  MenubarTrigger,
} from "@renderer/components/ui/menubar";
import { ArrowLeftIcon, ExternalLinkIcon, LinkIcon, LogOutIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { GameBananaBreadcrumbItem, GameOption } from "../-types";

export function GameBananaToolbar({
  games,
  selectedGame,
  selectedGameLabel,
  stageLabel,
  breadcrumbCategories,
  breadcrumbMod,
  isGamesLoading,
  gamesError,
  canOpenProfile,
  canGoBack,
  isLoggingOut,
  onSelectGame,
  onOpenModUrlDialog,
  onOpenGameProfile,
  onLogout,
  onGoBack,
  onBackToCategory,
  onSelectBreadcrumbCategory,
  onResetToGameHome,
}: {
  games: GameOption[];
  selectedGame?: GameOption["key"];
  selectedGameLabel?: string;
  stageLabel: string;
  breadcrumbCategories: GameBananaBreadcrumbItem[];
  breadcrumbMod?: string;
  isGamesLoading: boolean;
  gamesError: boolean;
  canOpenProfile: boolean;
  canGoBack: boolean;
  isLoggingOut: boolean;
  onSelectGame: (game: GameOption["key"]) => void;
  onOpenModUrlDialog: () => void;
  onOpenGameProfile: () => void;
  onLogout: () => void;
  onGoBack: () => void;
  onBackToCategory: () => void;
  onSelectBreadcrumbCategory: (index: number) => void;
  onResetToGameHome: () => void;
}) {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col justify-between gap-3 lg:flex-row">
      <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center">
        {canGoBack && (
          <Button variant="ghost" size="sm" className="h-9 gap-1 px-2" onClick={onGoBack}>
            <ArrowLeftIcon className="size-4" />
            {t("page.gamebanana.back")}
          </Button>
        )}

        <Menubar className="h-9 w-fit min-w-0">
          <MenubarMenu>
            <MenubarTrigger className="gap-2 px-3">
              <span className="text-xs text-muted-foreground">
                {t("page.gamebanana.game_menu")}
              </span>
              <span className="max-w-44 truncate">
                {selectedGameLabel ?? t("page.gamebanana.title")}
              </span>
            </MenubarTrigger>
            <MenubarContent>
              {isGamesLoading && (
                <div className="px-2 py-1 text-sm text-muted-foreground">
                  {t("page.gamebanana.loading")}
                </div>
              )}
              {gamesError && (
                <div className="px-2 py-1 text-sm text-muted-foreground">
                  {t("page.gamebanana.error_title")}
                </div>
              )}
              {!isGamesLoading && !gamesError && (
                <MenubarRadioGroup value={selectedGame}>
                  {games.map((game) => (
                    <MenubarRadioItem
                      key={game.key}
                      value={game.key}
                      onClick={() => onSelectGame(game.key)}
                    >
                      {game.key.toUpperCase()}
                    </MenubarRadioItem>
                  ))}
                </MenubarRadioGroup>
              )}
            </MenubarContent>
          </MenubarMenu>
        </Menubar>

        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbLink asChild className="cursor-pointer" onClick={onResetToGameHome}>
                <button type="button">{selectedGameLabel}</button>
              </BreadcrumbLink>
            </BreadcrumbItem>
            {breadcrumbCategories.map((category, index) => {
              const isLastCategory = index === breadcrumbCategories.length - 1;
              const isClickable = breadcrumbMod || !isLastCategory;

              return (
                <div key={`category-${category.id}-${index}`} className="contents">
                  <BreadcrumbSeparator />
                  <BreadcrumbItem>
                    {isClickable ? (
                      <BreadcrumbLink
                        asChild
                        className="cursor-pointer"
                        onClick={
                          breadcrumbMod && isLastCategory
                            ? onBackToCategory
                            : () => onSelectBreadcrumbCategory(index)
                        }
                      >
                        <button type="button">{category.name}</button>
                      </BreadcrumbLink>
                    ) : (
                      <BreadcrumbPage>{category.name}</BreadcrumbPage>
                    )}
                  </BreadcrumbItem>
                </div>
              );
            })}
            {breadcrumbMod && (
              <>
                <BreadcrumbSeparator />
                <BreadcrumbItem>
                  <BreadcrumbPage>{breadcrumbMod}</BreadcrumbPage>
                </BreadcrumbItem>
              </>
            )}
          </BreadcrumbList>
        </Breadcrumb>
      </div>

      <ButtonGroup className="shrink-0">
        <ButtonGroupText className="h-8 text-xs text-muted-foreground">
          {stageLabel}
        </ButtonGroupText>
        <Button variant="outline" onClick={onOpenModUrlDialog}>
          <LinkIcon />
          {/* {t("page.gamebanana.open_mod_url.button")} */}
        </Button>
        {canOpenProfile && (
          <Button variant="outline" onClick={onOpenGameProfile}>
            <ExternalLinkIcon />
            {/* {t("page.gamebanana.open_profile")} */}
          </Button>
        )}
        <Button variant="outline" onClick={onLogout} disabled={isLoggingOut}>
          <LogOutIcon />
          {/* {t("page.gamebanana.logout")} */}
        </Button>
      </ButtonGroup>
    </div>
  );
}
