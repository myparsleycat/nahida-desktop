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
import { useFeedbackSubmit } from "@renderer/hooks/use-feedback-submit";
import { Logger } from "@renderer/lib/logger";
import { cn } from "@renderer/lib/utils";
import { formatSize, toErrorMessage } from "@shared/utils";
import { createFileRoute } from "@tanstack/react-router";
import {
  FileArchiveIcon,
  FileTextIcon,
  FilmIcon,
  FolderIcon,
  FolderPlusIcon,
  ImageIcon,
  Loader2Icon,
  PaperclipIcon,
  type LucideIcon,
  XIcon,
} from "lucide-react";
import { useId, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/feedback")({
  component: RouteComponent,
});

// The website category is offered on the website itself, not here.
const CATEGORIES = ["desktop", "service", "xxmi-libs"] as const;

type Category = (typeof CATEGORIES)[number];

// The media kinds are the server's; one it adds later gets the generic icon.
const kindIcons: Partial<Record<string, LucideIcon>> = {
  [Kind.KindImage]: ImageIcon,
  [Kind.KindVideo]: FilmIcon,
  [Kind.KindText]: FileTextIcon,
  [Kind.KindMod]: FolderIcon,
  archive: FileArchiveIcon,
};

function RouteComponent() {
  const { t } = useTranslation();
  const { session } = useAuth();
  const logCheckboxId = useId();
  const [category, setCategory] = useState<Category>("desktop");
  const [body, setBody] = useState("");
  const [includeLog, setIncludeLog] = useState(true);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const {
    rules,
    progress,
    sending,
    problem: reportProblem,
    submit: send,
    cancel,
  } = useFeedbackSubmit();

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
  const problem = reportProblem({ body, mediaFiles: media.length, mediaBytes });

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
    if (!rules) return [];
    const selected = await Dialog.ShowOpenDialog({
      title: t("page.setting.feedback.attachments.add_files"),
      defaultPath: "",
      filters: [
        {
          name: t("page.setting.feedback.attachments.filter"),
          extensions: (rules.mediaExtensions ?? []).map((item) => item.ext.slice(1)),
        },
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
    const result = await send({
      category,
      body,
      includeLog,
      files: media.map((item) => item.path),
      modDirs: mods.map((item) => item.path),
    });
    if (!result) return;
    setBody("");
    setAttachments([]);
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
                  rules && body.length > rules.maxBodyLength && "text-destructive",
                )}
              >
                {body.length.toLocaleString()}
                {rules && ` / ${rules.maxBodyLength.toLocaleString()}`}
              </span>
            </div>
            <Textarea
              value={body}
              disabled={sending}
              aria-invalid={rules && body.length > rules.maxBodyLength}
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
                  rules && mediaBytes > rules.maxMediaBytes && "text-destructive",
                )}
              >
                {formatSize(mediaBytes)}
                {rules && ` / ${formatSize(rules.maxMediaBytes)}`}
              </span>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("page.setting.feedback.attachments.description")}
            </p>
            {attachments.length > 0 && (
              <ul className="space-y-1">
                {attachments.map((item) => {
                  const Icon = kindIcons[item.kind] ?? PaperclipIcon;
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
                disabled={sending || !rules}
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
              <Button variant="outline" onClick={cancel}>
                {t("page.setting.feedback.cancel")}
              </Button>
            )}
            <Button
              disabled={sending || !rules || body.trim() === "" || problem !== null}
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
