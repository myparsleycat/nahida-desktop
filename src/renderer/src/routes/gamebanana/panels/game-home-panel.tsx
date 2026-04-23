import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import type { TFunction } from "i18next";
import { SubmissionCard } from "../cards/submission-card";
import { PaginationButtons, ErrorState, OverviewSkeleton } from "../shared/common";
import type { GameSubfeedQuery } from "../types";
import { getSubmissionDateKey } from "../utils";

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
  onSelectMod: (modId: number) => void;
}) {
  return (
    <Card className="flex h-full min-h-0 flex-col">
      <CardHeader>
        <div className="flex items-center justify-between gap-3">
          <div>
            <CardTitle className="text-base">{t("page.gamebanana.latest_feed")}</CardTitle>
          </div>
          <PaginationButtons
            page={subfeedPage}
            onPrev={() => onSubfeedPage(subfeedPage - 1)}
            onNext={() => onSubfeedPage(subfeedPage + 1)}
            disablePrev={subfeedPage <= 1}
            disableNext={Boolean(subfeedQuery.data?._aMetadata._bIsComplete)}
          />
        </div>
      </CardHeader>
      <CardContent className="min-h-0 flex-1">
        <ScrollArea className="h-full min-h-0 pr-4">
          <div className="space-y-3">
            {subfeedQuery.isLoading && <OverviewSkeleton />}
            {subfeedQuery.error && <ErrorState title={t("page.gamebanana.error_title")} />}
            <div className="grid gap-4 sm:grid-cols-3 2xl:grid-cols-4">
              {subfeedQuery.data?._aRecords.map((submission) => (
                <SubmissionCard
                  key={`feed-${submission._idRow}-${getSubmissionDateKey(submission)}`}
                  submission={submission}
                  language={language}
                  onClick={() => onSelectMod(submission._idRow)}
                />
              ))}
            </div>
          </div>
        </ScrollArea>
      </CardContent>
    </Card>
  );
}
