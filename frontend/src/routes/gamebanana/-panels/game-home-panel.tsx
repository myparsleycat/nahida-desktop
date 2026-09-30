import { ScrollArea } from "@renderer/components/ui/scroll-area";
import type { TFunction } from "i18next";

import type { GameBananaSubmissionSelection, GameSubfeedQuery } from "../-types";

import { SubmissionCard } from "../-cards/submission-card";
import { ErrorState, OverviewSkeleton, PaginationButtons } from "../-shared/common";
import { getGameBananaErrorPresentation } from "../-shared/errors";
import { getSubmissionDateKey } from "../-utils";

export function GameHomePanel({
  t,
  language,
  subfeedQuery,
  subfeedPage,
  onSubfeedPage,
  onSelectMod,
}: {
  t: TFunction;
  language: string;
  subfeedQuery: GameSubfeedQuery;
  subfeedPage: number;
  onSubfeedPage: (page: number) => void;
  onSelectMod: (submission: GameBananaSubmissionSelection) => void;
}) {
  const errorPresentation = getGameBananaErrorPresentation(subfeedQuery.error, t);
  const metadata = subfeedQuery.data?._aMetadata;
  const totalPages =
    metadata && metadata._nPerpage > 0
      ? Math.ceil(metadata._nRecordCount / metadata._nPerpage)
      : undefined;
  const disableNext = subfeedQuery.data == null || Boolean(metadata?._bIsComplete);

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col">
      <div className="flex shrink-0 items-center justify-between gap-3 border-b px-4 py-2">
        <h2 className="text-sm font-medium">{t("page.gamebanana.latest_feed")}</h2>
        <PaginationButtons
          page={subfeedPage}
          totalPages={totalPages}
          onPrev={() => onSubfeedPage(subfeedPage - 1)}
          onNext={() => onSubfeedPage(subfeedPage + 1)}
          onPageChange={onSubfeedPage}
          disablePrev={subfeedPage <= 1}
          disableNext={disableNext}
        />
      </div>
      <ScrollArea className="min-h-0 flex-1">
        <div className="space-y-3 p-4">
          {subfeedQuery.isLoading && <OverviewSkeleton />}
          {subfeedQuery.error && (
            <ErrorState
              title={t("page.gamebanana.error_title")}
              description={errorPresentation.description}
              details={errorPresentation.details}
            />
          )}
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-3 2xl:grid-cols-4">
            {getFeedRecords(subfeedQuery).map((submission) => (
              <SubmissionCard
                key={`feed-${submission._idRow}-${getSubmissionDateKey(submission)}`}
                submission={submission}
                language={language}
                onClick={() =>
                  onSelectMod({
                    id: submission._idRow,
                    modelName: submission._sModelName,
                  })
                }
              />
            ))}
          </div>
        </div>
      </ScrollArea>
    </div>
  );
}

function getFeedRecords(query: GameSubfeedQuery) {
  return query.data?._aRecords.filter((record) => record._sModelName === "Mod") ?? [];
}
