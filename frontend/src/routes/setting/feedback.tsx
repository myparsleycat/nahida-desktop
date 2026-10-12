import { type Attachment, Kind, Service as Feedback } from "@bindings/feedback";
import { Dialog } from "@bindings/platform";
import { Button } from "@renderer/components/ui/button";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { Progress } from "@renderer/components/ui/progress";
import {
  Section,
  SectionContent,
  SectionDescription,
  SectionHeader,
  SectionTitle,
} from "@renderer/components/ui/section";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Textarea } from "@renderer/components/ui/textarea";
import { useAuth } from "@renderer/hooks/use-auth";
import { Logger } from "@renderer/lib/logger";
import { cn } from "@renderer/lib/utils";
import { formatSize, toErrorMessage } from "@shared/utils";
import { createFileRoute } from "@tanstack/react-router";
import { Events } from "@wailsio/runtime";
import {
  FileTextIcon,
  FilmIcon,
  FolderIcon,
  FolderPlusIcon,
  ImageIcon,
  Loader2Icon,
  PaperclipIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/feedback")({
  component: RouteComponent,
});

// The backend enforces the same limits; they are repeated here so a report it
// would refuse is caught before anything is uploaded.
const MAX_BODY_LENGTH = 10_000;
const MAX_MEDIA_BYTES = 100 * 1024 * 1024;
const MAX_MEDIA_FILES = 20;
const MAX_MOD_FILES = 2000;
const MEDIA_EXTENSIONS = [
  "png",
  "jpg",
  "jpeg",
  "webp",
  "gif",
  "avif",
  "bmp",
  "mp4",
  "webm",
  "mov",
  "mkv",
  "txt",
  "log",
  "md",
  "json",
  "ini",
];
// The website category is offered on the website itself, not here.
const CATEGORIES = ["desktop", "service", "xxmi-libs"] as const;
const ERROR_CODES = [
  "FEEDBACK_UNSUPPORTED_FILE",
  "FEEDBACK_MEDIA_TOO_LARGE",
  "FEEDBACK_TOO_MANY_MEDIA",
  "FEEDBACK_TOO_MANY_MOD_FILES",
  "FEEDBACK_EMPTY_MOD",
  "FEEDBACK_BUSY",
] as const;

type Category = (typeof CATEGORIES)[number];

const kindIcons = {
  [Kind.KindImage]: ImageIcon,
  [Kind.KindVideo]: FilmIcon,
  [Kind.KindText]: FileTextIcon,
  [Kind.KindMod]: FolderIcon,
  [Kind.KindUnsupported]: FileTextIcon,
};

function RouteComponent() {
  const { t, i18n } = useTranslation();
  const { session } = useAuth();
  const logCheckboxId = useId();
  const [category, setCategory] = useState<Category>("desktop");
  const [body, setBody] = useState("");
  const [includeLog, setIncludeLog] = useState(true);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [progress, setProgress] = useState<number | null>(null);
  const sending = progress !== null;
  const cancelled = useRef(false);

  useEffect(() => {
    return Events.On("feedback:progress", (event) => {
      const data = event.data as { uploadedBytes: number; totalBytes: number };
      setProgress((current) =>
        current === null
          ? current
          : data.totalBytes > 0
            ? (data.uploadedBytes / data.totalBytes) * 100
            : 100,
      );
    });
  }, []);

  const categoryOptions = useMemo(
    () =>
      CATEGORIES.map((value) => ({
        value,
        label: t(`page.setting.feedback.category.${value}`),
      })),
    [t],
  );
  const media = attachments.filter((item) => item.kind !== Kind.KindMod);
  const mods = attachments.filter((item) => item.kind === Kind.KindMod);
  const mediaBytes = media.reduce((sum, item) => sum + item.size, 0);
  const problem =
    body.length > MAX_BODY_LENGTH
      ? t("page.setting.feedback.error.body_too_long")
      : media.length > MAX_MEDIA_FILES
        ? t("page.setting.feedback.error.FEEDBACK_TOO_MANY_MEDIA", { count: MAX_MEDIA_FILES })
        : mediaBytes > MAX_MEDIA_BYTES
          ? t("page.setting.feedback.error.FEEDBACK_MEDIA_TOO_LARGE")
          : null;

  const addPaths = async (pick: () => Promise<string[]>) => {
    try {
      const paths = await pick();
      if (paths.length === 0) return;
      const described = (await Feedback.Describe(paths)) ?? [];
      const unsupported = described.filter((item) => item.kind === Kind.KindUnsupported);
      if (unsupported.length > 0) {
        toast.error(
          t("page.setting.feedback.error.FEEDBACK_UNSUPPORTED_FILE", {
            name: unsupported.map((item) => item.name).join(", "),
          }),
        );
      }
      // An empty file has no bytes to sign an upload for.
      const empty = described.filter(
        (item) =>
          item.kind !== Kind.KindUnsupported && item.kind !== Kind.KindMod && item.size === 0,
      );
      if (empty.length > 0) {
        toast.error(
          t("page.setting.feedback.error.empty_file", {
            name: empty.map((item) => item.name).join(", "),
          }),
        );
      }
      setAttachments((current) => [
        ...current,
        ...described.filter(
          (item) =>
            item.kind !== Kind.KindUnsupported &&
            !empty.includes(item) &&
            !current.some((existing) => existing.path === item.path),
        ),
      ]);
    } catch (error) {
      Logger.error(error, "Feedback:addPaths");
      toast.error(toErrorMessage(error));
    }
  };

  const pickFiles = async () => {
    const selected = await Dialog.ShowOpenDialog({
      title: t("page.setting.feedback.attachments.add_files"),
      defaultPath: "",
      filters: [
        { name: t("page.setting.feedback.attachments.filter"), extensions: MEDIA_EXTENSIONS },
      ],
      properties: ["openFile", "multiSelections"],
    });
    return selected.canceled ? [] : (selected.filePaths ?? []);
  };

  const pickMod = async () => {
    const selected = await Dialog.SelectDirectory();
    return selected.canceled || !selected.filePath ? [] : [selected.filePath];
  };

  const submit = async () => {
    cancelled.current = false;
    setProgress(0);
    try {
      const result = await Feedback.Submit({
        category,
        body,
        includeLog,
        files: media.map((item) => item.path),
        modDirs: mods.map((item) => item.path),
        locale: i18n.language,
      });
      toast.success(t("page.setting.feedback.sent"), {
        description:
          result.skippedModFiles > 0
            ? t("page.setting.feedback.skipped_mod_files", { count: result.skippedModFiles })
            : undefined,
      });
      setBody("");
      setAttachments([]);
    } catch (error) {
      if (cancelled.current) return;
      const message = toErrorMessage(error);
      const code = ERROR_CODES.find((item) => message.startsWith(`${item}:`));
      toast.error(t("page.setting.feedback.failed"), {
        description: code
          ? t(`page.setting.feedback.error.${code}`, {
              name: message.slice(code.length + 1),
              count: code === "FEEDBACK_TOO_MANY_MEDIA" ? MAX_MEDIA_FILES : MAX_MOD_FILES,
            })
          : message,
      });
    } finally {
      setProgress(null);
    }
  };

  return (
    <div className="space-y-6 p-4">
      <Section>
        <SectionHeader>
          <SectionTitle>{t("page.setting.feedback.title")}</SectionTitle>
          <SectionDescription>{t("page.setting.feedback.description")}</SectionDescription>
        </SectionHeader>
        <SectionContent layout="flow" className="gap-5">
          <div className="space-y-2">
            <span className="text-sm font-medium">{t("page.setting.feedback.category.label")}</span>
            <Select
              value={category}
              items={categoryOptions}
              disabled={sending}
              onValueChange={(value) => {
                if (value !== null) setCategory(value);
              }}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent finalFocus={false}>
                <SelectGroup>
                  {categoryOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <div className="flex items-baseline justify-between">
              <span className="text-sm font-medium">{t("page.setting.feedback.body.label")}</span>
              <span
                className={cn(
                  "text-xs text-muted-foreground tabular-nums",
                  body.length > MAX_BODY_LENGTH && "text-destructive",
                )}
              >
                {body.length.toLocaleString()} / {MAX_BODY_LENGTH.toLocaleString()}
              </span>
            </div>
            <Textarea
              value={body}
              disabled={sending}
              aria-invalid={body.length > MAX_BODY_LENGTH}
              placeholder={t("page.setting.feedback.body.placeholder")}
              className="max-h-96 min-h-40"
              onChange={(event) => setBody(event.target.value)}
            />
          </div>

          <div className="space-y-2">
            <div className="flex items-baseline justify-between">
              <span className="text-sm font-medium">
                {t("page.setting.feedback.attachments.label")}
              </span>
              <span
                className={cn(
                  "text-xs text-muted-foreground tabular-nums",
                  mediaBytes > MAX_MEDIA_BYTES && "text-destructive",
                )}
              >
                {formatSize(mediaBytes)} / {formatSize(MAX_MEDIA_BYTES)}
              </span>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("page.setting.feedback.attachments.description")}
            </p>
            {attachments.length > 0 && (
              <ul className="space-y-1">
                {attachments.map((item) => {
                  const Icon = kindIcons[item.kind];
                  return (
                    <li
                      key={item.path}
                      className="flex items-center gap-2 rounded-md bg-muted/50 px-2.5 py-1.5 text-sm"
                    >
                      <Icon className="size-4 shrink-0 text-muted-foreground" />
                      <span className="min-w-0 flex-1 truncate" title={item.path}>
                        {item.name}
                      </span>
                      <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                        {item.kind === Kind.KindMod &&
                          `${t("page.setting.feedback.attachments.file_count", { count: item.files })} · `}
                        {formatSize(item.size)}
                      </span>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        disabled={sending}
                        aria-label={t("page.setting.feedback.attachments.remove")}
                        onClick={() =>
                          setAttachments((current) =>
                            current.filter((existing) => existing.path !== item.path),
                          )
                        }
                      >
                        <XIcon />
                      </Button>
                    </li>
                  );
                })}
              </ul>
            )}
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={sending}
                onClick={() => void addPaths(pickFiles)}
              >
                <PaperclipIcon />
                {t("page.setting.feedback.attachments.add_files")}
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={sending || !session}
                onClick={() => void addPaths(pickMod)}
              >
                <FolderPlusIcon />
                {t("page.setting.feedback.attachments.add_mod")}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              {t(
                session
                  ? "page.setting.feedback.attachments.mod_description"
                  : "page.setting.feedback.attachments.mod_login_required",
              )}
            </p>
          </div>

          <div className="flex items-start gap-2.5">
            <Checkbox
              id={logCheckboxId}
              className="mt-0.5"
              checked={includeLog}
              disabled={sending}
              onCheckedChange={(checked) => setIncludeLog(checked)}
            />
            <label htmlFor={logCheckboxId} className="space-y-0.5">
              <span className="text-sm font-medium">{t("page.setting.feedback.log.label")}</span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.feedback.log.description")}
              </p>
            </label>
          </div>

          {sending && <Progress value={progress} />}
          {problem && <p className="text-xs text-destructive">{problem}</p>}

          <div className="flex justify-end gap-2">
            {sending && (
              <Button
                variant="outline"
                onClick={() => {
                  cancelled.current = true;
                  void Feedback.Cancel();
                }}
              >
                {t("page.setting.feedback.cancel")}
              </Button>
            )}
            <Button
              disabled={sending || body.trim() === "" || problem !== null}
              onClick={() => void submit()}
            >
              {sending && <Loader2Icon className="animate-spin" />}
              {t(sending ? "page.setting.feedback.sending" : "page.setting.feedback.submit")}
            </Button>
          </div>
        </SectionContent>
      </Section>
    </div>
  );
}
