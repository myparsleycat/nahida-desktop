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
import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import {
  ArrowLeftIcon,
  ChevronDownIcon,
  ExternalLinkIcon,
  LinkIcon,
  LoaderIcon,
  LogOutIcon,
} from "lucide-react";
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
  isModUrlOpen,
  onToggleModUrl,
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
  isModUrlOpen: boolean;
  onSelectGame: (game: GameOption["key"]) => void;
  onToggleModUrl: () => void;
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
              <BreadcrumbLink
                render={<button type="button" />}
                className="cursor-pointer"
                onClick={onResetToGameHome}
              >
                {selectedGameLabel}
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
                        render={<button type="button" />}
                        className="cursor-pointer"
                        onClick={
                          breadcrumbMod && isLastCategory
                            ? onBackToCategory
                            : () => onSelectBreadcrumbCategory(index)
                        }
                      >
                        {category.name}
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
        <Tooltip>
          <TooltipTrigger
            render={
              <Button variant="outline" onClick={onToggleModUrl} aria-expanded={isModUrlOpen} />
            }
          >
            <LinkIcon />
            <ChevronDownIcon
              className={isModUrlOpen ? "rotate-180 transition-transform" : "transition-transform"}
            />
            <span className="sr-only">{t("page.gamebanana.open_mod_url.button")}</span>
          </TooltipTrigger>
          <TooltipContent side="bottom">{t("page.gamebanana.open_mod_url.button")}</TooltipContent>
        </Tooltip>
        {canOpenProfile && (
          <Tooltip>
            <TooltipTrigger render={<Button variant="outline" onClick={onOpenGameProfile} />}>
              <ExternalLinkIcon />
              <span className="sr-only">{t("page.gamebanana.open_profile")}</span>
            </TooltipTrigger>
            <TooltipContent side="bottom">{t("page.gamebanana.open_profile")}</TooltipContent>
          </Tooltip>
        )}
        <Tooltip>
          <TooltipTrigger
            render={<Button variant="outline" onClick={onLogout} disabled={isLoggingOut} />}
          >
            {isLoggingOut ? <LoaderIcon className="animate-spin" /> : <LogOutIcon />}
            <span className="sr-only">{t("page.gamebanana.logout")}</span>
          </TooltipTrigger>
          <TooltipContent side="bottom">{t("page.gamebanana.logout")}</TooltipContent>
        </Tooltip>
      </ButtonGroup>
    </div>
  );
}
