import type { SubmissionListItem } from "../-types";
import { PreviewCard } from "./preview-card";

export function SubmissionCard({
  submission,
  language,
  onClick,
  active = false,
}: {
  submission: SubmissionListItem;
  language: string;
  onClick: () => void;
  active?: boolean;
}) {
  return (
    <PreviewCard
      item={submission}
      language={language}
      onClick={onClick}
      active={active}
      hoverClassName="hover:bg-muted/40"
    />
  );
}
