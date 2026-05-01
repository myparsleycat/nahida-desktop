import type {
    GameBananaGameKey,
    GameBananaSubmissionSelection,
    useGameBananaGameSubfeed,
    useGameBananaModCategoryOverview,
    useGameBananaModOverview,
} from "@renderer/hooks/use-gamebanana-data";
import type { IpcHandlers } from "@shared/types";

type GameOverviewData = Awaited<ReturnType<IpcHandlers["gamebanana:getGameOverview"]>>;
type GameSubfeedData = Awaited<ReturnType<IpcHandlers["gamebanana:getGameSubfeed"]>>;
type CategoryOverviewData = Awaited<ReturnType<IpcHandlers["gamebanana:getModCategoryOverview"]>>;
type ModOverviewData = Awaited<ReturnType<IpcHandlers["gamebanana:getModOverview"]>>;

export interface GameOption {
    key: GameBananaGameKey;
    id: number;
}

export type { GameBananaSubmissionSelection };

export interface GameBananaBreadcrumbItem {
    id: number;
    name: string;
}

export type SubmissionListItem =
    | GameSubfeedData["_aRecords"][number]
    | CategoryOverviewData["index"]["_aRecords"][number];
export type PreviewMedia = NonNullable<SubmissionListItem["_aPreviewMedia"]>;
export type PreviewImage = NonNullable<PreviewMedia["_aImages"]>[number];
export type RootCategoryItem = GameOverviewData["profile"]["_aModRootCategories"][number];
export type CategoryChildItem = CategoryOverviewData["categories"][number];
export type ModFileItem = NonNullable<ModOverviewData["profile"]["_aFiles"]>[number];

export type GameSubfeedQuery = ReturnType<typeof useGameBananaGameSubfeed>;
export type CategoryOverviewQuery = ReturnType<typeof useGameBananaModCategoryOverview>;
export type ModOverviewQuery = ReturnType<typeof useGameBananaModOverview>;
