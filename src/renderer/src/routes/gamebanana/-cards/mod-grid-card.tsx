import type { SubmissionListItem } from "../-types";
import { PreviewCard } from "./preview-card";

export function ModGridCard({
  mod,
  language,
  onClick,
}: {
  mod: SubmissionListItem;
  language: string;
  onClick: () => void;
}) {
  return (
    <PreviewCard
      item={mod}
      language={language}
      onClick={onClick}
      hoverClassName="hover:bg-muted/30"
    />
  );
}
