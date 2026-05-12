import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Button } from "@renderer/components/ui/button";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useGlobalStore } from "@renderer/store/global";
import type { ReactNode } from "react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

const RELEASE_NOTE_LINK_PATTERN = /\[(https?:\/\/[^\]\s]+)\]/g;
const RELEASE_NOTE_LINK_BOUNDARIES = ["* ", "- ", "• ", ": ", "의 ", "by ", "from "];
const RELEASE_NOTE_BULLET_PATTERN = /^(\s*)([*-])\s+(.*)$/;

function findReleaseNoteLinkSplitIndex(value: string) {
  let splitIndex = -1;

  for (const boundary of RELEASE_NOTE_LINK_BOUNDARIES) {
    const boundaryIndex = value.lastIndexOf(boundary);
    if (boundaryIndex === -1) {
      continue;
    }

    const candidateIndex = boundaryIndex + boundary.length;
    if (candidateIndex < value.length && candidateIndex > splitIndex) {
      splitIndex = candidateIndex;
    }
  }

  return splitIndex;
}

function renderReleaseNoteLine(line: string, lineIndex: number) {
  const nodes: ReactNode[] = [];
  let cursor = 0;
  let matchIndex = 0;

  for (const match of line.matchAll(RELEASE_NOTE_LINK_PATTERN)) {
    const fullMatch = match[0];
    const url = match[1];
    const matchStart = match.index ?? 0;
    const prefix = line.slice(cursor, matchStart);
    const splitIndex = findReleaseNoteLinkSplitIndex(prefix);
    const leadingText = splitIndex >= 0 ? prefix.slice(0, splitIndex) : "";
    const linkLabel = (splitIndex >= 0 ? prefix.slice(splitIndex) : prefix).trimEnd();

    if (leadingText) {
      nodes.push(<span key={`text-${lineIndex}-${matchIndex}`}>{leadingText}</span>);
    }

    if (linkLabel) {
      nodes.push(
        <button
          key={`link-${lineIndex}-${matchIndex}`}
          type="button"
          className="inline cursor-pointer text-primary underline underline-offset-4 hover:text-primary/80"
          onClick={() => void window.api.invoke("util:openExternal", url)}
          title={url}
        >
          {linkLabel}
        </button>,
      );
    } else if (prefix && !leadingText) {
      nodes.push(<span key={`text-${lineIndex}-${matchIndex}`}>{prefix}</span>);
    } else {
      nodes.push(
        <button
          key={`link-${lineIndex}-${matchIndex}`}
          type="button"
          className="inline cursor-pointer text-primary underline underline-offset-4 hover:text-primary/80"
          onClick={() => void window.api.invoke("util:openExternal", url)}
          title={url}
        >
          {url}
        </button>,
      );
    }

    cursor = matchStart + fullMatch.length;
    matchIndex += 1;
  }

  const trailingText = line.slice(cursor);
  if (trailingText) {
    nodes.push(<span key={`text-${lineIndex}-tail`}>{trailingText}</span>);
  }

  if (nodes.length === 0) {
    return <span>&nbsp;</span>;
  }

  return nodes;
}

function ReleaseNotesContent({ text }: { text: string }) {
  return (
    <div className="px-4 py-3 text-sm whitespace-pre-wrap break-words">
      {text.split("\n").map((line, index) => {
        const bulletMatch = line.match(RELEASE_NOTE_BULLET_PATTERN);
        if (bulletMatch) {
          const [, indent, marker, content] = bulletMatch;
          return (
            <div
              key={`release-note-line-${index}`}
              className="flex items-start gap-2"
              style={indent ? { paddingLeft: `${indent.length}ch` } : undefined}
            >
              <span className="shrink-0">{marker}</span>
              <div className="min-w-0 flex-1">{renderReleaseNoteLine(content, index)}</div>
            </div>
          );
        }

        return <div key={`release-note-line-${index}`}>{renderReleaseNoteLine(line, index)}</div>;
      })}
    </div>
  );
}

export function UpdateAlertDialog() {
  const { t } = useTranslation();
  const appStatus = useGlobalStore((state) => state.appStatus);
  const open = useGlobalStore((state) => state.shouldPromptForUpdate);
  const releaseVersion = useGlobalStore((state) => state.releaseVersion);
  const releaseNotes = useGlobalStore((state) => state.releaseNotes);
  const setShouldPromptForUpdate = useGlobalStore((state) => state.setShouldPromptForUpdate);
  const isDismissingRef = useRef(false);
  const skipNextDismissRef = useRef(false);
  const [showOriginalReleaseNotes, setShowOriginalReleaseNotes] = useState(false);
  const versionRangeText =
    appStatus?.version && releaseVersion ? ` (${appStatus.version} → ${releaseVersion})` : "";
  const hasTranslatedReleaseNotes = !!(releaseNotes?.translated && releaseNotes?.original);
  const displayedReleaseNotesText =
    hasTranslatedReleaseNotes && !showOriginalReleaseNotes
      ? releaseNotes.translated
      : (releaseNotes?.original ?? releaseNotes?.translated ?? null);

  useEffect(() => {
    setShowOriginalReleaseNotes(false);
  }, [releaseNotes?.original, releaseNotes?.translated, releaseNotes?.translatedLanguage]);

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setShouldPromptForUpdate(true);
      return;
    }

    setShouldPromptForUpdate(false);

    if (skipNextDismissRef.current) {
      skipNextDismissRef.current = false;
      return;
    }

    if (isDismissingRef.current) {
      return;
    }

    isDismissingRef.current = true;
    window.api.invoke("updater:dismissUpdateDialog").finally(() => {
      isDismissingRef.current = false;
    });
  };

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent className="min-w-xl">
        <AlertDialogHeader>
          <AlertDialogTitle>{t("updater.toast.available.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("updater.toast.available.description")}
            <br />
            {versionRangeText}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {displayedReleaseNotesText && (
          <section className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <h3 className="text-sm font-medium">
                {t("updater.toast.available.releaseNotesTitle")}
              </h3>
              {hasTranslatedReleaseNotes && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setShowOriginalReleaseNotes((current) => !current)}
                >
                  {showOriginalReleaseNotes
                    ? t("updater.toast.available.showTranslation")
                    : t("updater.toast.available.showOriginal")}
                </Button>
              )}
            </div>
            <ScrollArea className="h-64 rounded-md border">
              <ReleaseNotesContent text={displayedReleaseNotesText} />
            </ScrollArea>
          </section>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>{t("g.later")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              skipNextDismissRef.current = true;
              setShouldPromptForUpdate(false);
              window.api.invoke("updater:installUpdate");
            }}
          >
            {t("updater.toast.available.action")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
