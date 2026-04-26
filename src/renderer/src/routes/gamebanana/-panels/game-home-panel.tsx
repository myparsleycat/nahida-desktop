import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import type { TFunction } from "i18next";
import { SubmissionCard } from "../-cards/submission-card";
import { ErrorState, OverviewSkeleton, PaginationButtons } from "../-shared/common";
import type { GameBananaSubmissionSelection, GameSubfeedQuery } from "../-types";
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
  return (
    <>
      <ScrollArea
        className="h-full min-h-0 min-w-0"
        viewportClassName="overflow-x-hidden [&>div]:!block [&>div]:!min-w-0 [&>div]:!w-full [&>div]:max-w-full"
      >
        <div className="min-w-0 max-w-full space-y-4 p-4">
          <Card className="flex h-full min-h-0 flex-col p-0">
            <CardHeader>
              <div className="flex items-center justify-between gap-3 pt-3">
                <div>
                  <CardTitle className="text-base">{t("page.gamebanana.latest_feed")}</CardTitle>
                </div>
                <PaginationButtons
                  page={subfeedPage}
                  totalPages={
                    subfeedQuery.data?._aMetadata
                      ? Math.ceil(
                          subfeedQuery.data._aMetadata._nRecordCount /
                            subfeedQuery.data._aMetadata._nPerpage,
                        )
                      : undefined
                  }
                  onPrev={() => onSubfeedPage(subfeedPage - 1)}
                  onNext={() => onSubfeedPage(subfeedPage + 1)}
                  onPageChange={onSubfeedPage}
                  disablePrev={subfeedPage <= 1}
                  disableNext={Boolean(subfeedQuery.data?._aMetadata._bIsComplete)}
                />
              </div>
            </CardHeader>
            <CardContent className="min-h-0 flex-1 p-0 px-1">
              <ScrollArea className="h-full min-h-0">
                <div className="space-y-3 p-4">
                  {subfeedQuery.isLoading && <OverviewSkeleton />}
                  {subfeedQuery.error && <ErrorState title={t("page.gamebanana.error_title")} />}
                  <div className="grid gap-4 sm:grid-cols-3 2xl:grid-cols-4">
                    {subfeedQuery.data?._aRecords.map((submission) => (
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
            </CardContent>
          </Card>
        </div>
      </ScrollArea>
    </>
  );
}
