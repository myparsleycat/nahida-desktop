import { Service as Agent } from "@bindings/agent";
import type {
  AgentApproval,
  AgentImage,
  AgentScope,
  AgentSessionSnapshot,
  AgentSessionSummary,
} from "@bindings/agent/models";
import { Markdown } from "@renderer/components/markdown";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Textarea } from "@renderer/components/ui/textarea";
import type { AgentStreamEvent, LiveAgentChatEntry } from "@renderer/lib/agent-stream";
import {
  acceptAgentStreamEvent,
  hasPendingAgentApproval,
  isAgentApprovalEvent,
  updateAgentLiveEntries,
} from "@renderer/lib/agent-stream";
import { localFileSrc } from "@renderer/lib/local-file";
import { cn } from "@renderer/lib/utils";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Events } from "@wailsio/runtime";
import {
  ArrowUpIcon,
  BrainIcon,
  CheckIcon,
  ChevronDownIcon,
  CircleStopIcon,
  CopyIcon,
  FolderIcon,
  Loader2Icon,
  MessageSquarePlusIcon,
  MoreHorizontalIcon,
  PaperclipIcon,
  PencilIcon,
  ShieldAlertIcon,
  SparklesIcon,
  Trash2Icon,
  Undo2Icon,
  WrenchIcon,
  XIcon,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import "./agent.css";

// Base UI's ScrollArea.Content keeps an inline `min-width: fit-content`, which would let a long
// session title widen the whole panel. Both scroll areas are vertical-only, so pin their content to
// the viewport width and drop the unused horizontal axis.
const scrollAreaClasses =
  "[&_[data-slot=scroll-area-content]]:min-w-0! [&_[data-slot=scroll-area-content]]:w-full [&_[data-slot=scroll-area-content]]:max-w-full [&_[data-slot=scroll-area-viewport]]:overflow-x-hidden!";

const pulseDot =
  "size-[7px] flex-none animate-[agent-pulse_1.4s_ease-in-out_infinite_alternate] rounded-full bg-(--agent-blue) shadow-[0_0_0_3px_color-mix(in_oklab,var(--agent-blue)_16%,transparent)] motion-reduce:animate-none";

const disclosure = "ml-px transition-transform duration-120 group-open:rotate-180";

const supportedImageTypes = ["image/png", "image/jpeg", "image/webp", "image/gif"];
const maxAttachedImages = 4;
const maxImageBytes = 5 << 20;

interface PendingImage {
  name: string;
  mimeType: string;
  data: string;
}

// A pending attachment renders from its data URL until the stored entry replaces it with the file
// path the local-file protocol serves.
function agentImageSrc(image: AgentImage) {
  return image.src.startsWith("data:") ? image.src : localFileSrc(image.src);
}

function pendingAgentImages(images: PendingImage[]): AgentImage[] {
  return images.map((image) => ({
    name: image.name,
    mimeType: image.mimeType,
    bytes: Math.floor((image.data.length * 3) / 4),
    path: "",
    src: `data:${image.mimeType};base64,${image.data}`,
  }));
}

async function readImageFile(file: File): Promise<PendingImage> {
  const dataUrl = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result !== "string") {
        reject(new Error(`Failed to read ${file.name}`));
        return;
      }
      resolve(reader.result);
    };
    reader.onerror = () => reject(reader.error ?? new Error(`Failed to read ${file.name}`));
    reader.readAsDataURL(file);
  });
  return { name: file.name, mimeType: file.type, data: dataUrl.slice(dataUrl.indexOf(",") + 1) };
}

interface AgentSearch {
  session?: string;
}

interface SessionGroup {
  key: string;
  label: string;
  sessions: AgentSessionSummary[];
}

export const Route = createFileRoute("/agent")({
  validateSearch: (search: Record<string, unknown>): AgentSearch => ({
    session: typeof search.session === "string" ? search.session : undefined,
  }),
  component: AgentRoute,
});

function AgentRoute() {
  const { t } = useTranslation();
  const search = Route.useSearch();
  const navigate = useNavigate();
  const [sessions, setSessions] = useState<AgentSessionSummary[]>([]);
  const [snapshot, setSnapshot] = useState<AgentSessionSnapshot>();
  const [draft, setDraft] = useState("");
  const [images, setImages] = useState<PendingImage[]>([]);
  const [liveEntries, setLiveEntries] = useState<LiveAgentChatEntry[]>([]);
  const [runId, setRunId] = useState<string>();
  const [decidingApproval, setDecidingApproval] = useState<string>();
  const [reverting, setReverting] = useState(false);
  const [menuSessionId, setMenuSessionId] = useState<string>();
  const [renameTarget, setRenameTarget] = useState<AgentSessionSummary>();
  const [renameValue, setRenameValue] = useState("");
  const [renaming, setRenaming] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<AgentSessionSummary>();
  const [deleting, setDeleting] = useState(false);
  const [loading, setLoading] = useState(true);
  const latestSequence = useRef(new Map<string, number>());
  const sessionGeneration = useRef(0);
  const displayedSessionId = useRef<string | undefined>(undefined);
  const renameInputRef = useRef<HTMLInputElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const hasPendingApproval = hasPendingAgentApproval(snapshot?.approvals);

  const refreshSessions = useCallback(async () => {
    setSessions((await Agent.ListSessions()) ?? []);
  }, []);

  const openSnapshot = useCallback(async (sessionId: string) => {
    const generation = ++sessionGeneration.current;
    // Invalidate the previous load before awaiting the backend, and point the stream subscription
    // at this session before its snapshot renders.
    displayedSessionId.current = sessionId;
    setReverting(false);
    const value = await Agent.GetSession(sessionId);
    if (generation !== sessionGeneration.current || displayedSessionId.current !== sessionId)
      return;
    setSnapshot(value);
    setRunId(value.summary.running ? "restored" : undefined);
    setLiveEntries([]);
  }, []);

  useEffect(() => {
    let cancelled = false;
    const initialize = async () => {
      setLoading(true);
      try {
        await refreshSessions();
        const session = search.session
          ? { id: search.session }
          : await Agent.OpenSession({ type: "global" });
        if (cancelled) return;
        if (!search.session) {
          await navigate({ to: "/agent", search: { session: session.id }, replace: true });
        }
        await openSnapshot(session.id);
        if (!search.session) await refreshSessions();
      } catch (error) {
        toast.error(String(error));
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void initialize();
    return () => {
      cancelled = true;
    };
  }, [navigate, openSnapshot, refreshSessions, search.session]);

  useEffect(() => {
    return Events.On("agent:update", (event) => {
      const update = event.data as AgentStreamEvent;
      // A generated title can land after the turn that produced it finished, so it is applied
      // before the per-run sequence check, which drops such late events.
      if (update.type === "session-title") {
        const title = update.payload?.title;
        if (!title) return;
        setSessions((current) =>
          current.map((session) =>
            session.id === update.sessionId ? { ...session, title } : session,
          ),
        );
        setSnapshot((current) =>
          current?.summary.id === update.sessionId
            ? { ...current, summary: { ...current.summary, title } }
            : current,
        );
        return;
      }
      // Read the session on screen from the ref: this handler outlives the render that installed
      // it, and a captured snapshot would accept the previous conversation's deltas for as long
      // as the render that switches conversations is still pending.
      if (update.sessionId !== displayedSessionId.current) return;
      if (!acceptAgentStreamEvent(latestSequence.current, update)) return;

      if (
        update.type === "assistant-delta" ||
        update.type === "reasoning-delta" ||
        update.type === "tool-start" ||
        update.type === "tool-end"
      ) {
        setLiveEntries((entries) => updateAgentLiveEntries(entries, update));
      } else if (isAgentApprovalEvent(update.type)) {
        if (update.type === "approval-requested") {
          setRunId(undefined);
        }
        void openSnapshot(update.sessionId);
      } else if (update.type === "status" || update.type === "error") {
        if (update.type === "status" && update.payload?.status === "running") {
          setRunId(update.runId);
        }
        const terminal =
          update.type === "error" ||
          update.payload?.status === "completed" ||
          update.payload?.status === "cancelled";
        if (terminal) {
          setRunId(undefined);
          setLiveEntries([]);
          void openSnapshot(update.sessionId);
          void refreshSessions();
        }
      }
    });
  }, [openSnapshot, refreshSessions]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [snapshot?.entries, liveEntries]);

  useEffect(() => {
    if (!renameTarget) return;
    queueMicrotask(() => {
      renameInputRef.current?.focus();
      renameInputRef.current?.select();
    });
  }, [renameTarget]);

  const addImages = async (files: File[]) => {
    const accepted: PendingImage[] = [];
    let overflow = false;
    for (const file of files) {
      if (images.length + accepted.length >= maxAttachedImages) {
        overflow = true;
        break;
      }
      if (!supportedImageTypes.includes(file.type)) {
        toast.error(t("page.agent.image_unsupported_type", { name: file.name }));
        continue;
      }
      if (file.size > maxImageBytes) {
        toast.error(
          t("page.agent.image_too_large", { name: file.name, limit: maxImageBytes >> 20 }),
        );
        continue;
      }
      try {
        accepted.push(await readImageFile(file));
      } catch (error) {
        toast.error(String(error));
      }
    }
    if (overflow) {
      toast.error(t("page.agent.image_limit_reached", { count: maxAttachedImages }));
    }
    if (accepted.length > 0) {
      setImages((current) => [...current, ...accepted].slice(0, maxAttachedImages));
    }
  };

  const send = async () => {
    if (
      !snapshot ||
      (!draft.trim() && images.length === 0) ||
      runId ||
      hasPendingApproval ||
      reverting ||
      snapshot.unavailableReason
    )
      return;
    const text = draft.trim();
    const attachments = images;
    setDraft("");
    setImages([]);
    setLiveEntries([]);
    setSnapshot((value) =>
      value
        ? {
            ...value,
            entries: [
              // Sending commits the staged revert, which deletes these events; drop them here so the
              // conversation does not show messages that no longer exist for the whole turn.
              ...(value.entries ?? []).filter((entry) => !entry.reverted),
              {
                sequence: Number.MAX_SAFE_INTEGER,
                turnId: "pending",
                type: "turn/start",
                role: "user",
                text,
                images: pendingAgentImages(attachments),
                createdAt: new Date().toISOString(),
              },
            ],
            // Sending commits a staged revert on the backend, so drop the banner now instead of
            // leaving it up for the whole turn.
            revert: undefined,
          }
        : value,
    );
    try {
      const run = await Agent.Send(
        snapshot.summary.id,
        text,
        attachments.map((image) => ({
          name: image.name,
          mimeType: image.mimeType,
          data: image.data,
        })),
      );
      setRunId(run.runId);
      latestSequence.current.delete(run.runId);
    } catch (error) {
      toast.error(String(error));
      setDraft(text);
      setImages(attachments);
      await openSnapshot(snapshot.summary.id);
    }
  };

  const createSession = async () => {
    // Clicking "New conversation" from a conversation that already has no messages should not
    // leave a trail of empty sessions: reuse the empty one instead of creating another.
    if (snapshot && (snapshot.entries?.length ?? 0) === 0) return;

    const scope = snapshot?.summary.scope ?? { type: "global" };
    const existing = await findEmptySession(sessions, scope);
    if (existing) {
      await navigate({ to: "/agent", search: { session: existing.id }, replace: true });
      return;
    }

    const session = await Agent.CreateSession(scope);
    await refreshSessions();
    await navigate({ to: "/agent", search: { session: session.id }, replace: true });
  };

  const openRenameDialog = (session: AgentSessionSummary) => {
    setRenameValue(session.title);
    setRenameTarget(session);
  };

  const submitRename = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!renameTarget || renaming) return;
    const title = renameValue.trim();
    if (!title) return;

    setRenaming(true);
    try {
      await Agent.RenameSession(renameTarget.id, title);
      setRenameTarget(undefined);
      await refreshSessions();
      if (snapshot?.summary.id === renameTarget.id) {
        await openSnapshot(renameTarget.id);
      }
    } catch (error) {
      toast.error(String(error));
    } finally {
      setRenaming(false);
    }
  };

  const openDeleteDialog = (session: AgentSessionSummary) => setDeleteTarget(session);

  const confirmDelete = async () => {
    if (!deleteTarget || deleting) return;
    setDeleting(true);
    try {
      await Agent.DeleteSession(deleteTarget.id);
      const remaining = sessions.filter((value) => value.id !== deleteTarget.id);
      setSessions(remaining);
      setDeleteTarget(undefined);
      const next = remaining[0] ?? (await Agent.OpenSession({ type: "global" }));
      await navigate({ to: "/agent", search: { session: next.id }, replace: true });
    } catch (error) {
      toast.error(String(error));
    } finally {
      setDeleting(false);
    }
  };

  const decideApproval = async (approval: AgentApproval, approved: boolean) => {
    if (approval.status !== "pending" || decidingApproval) return;
    setDecidingApproval(approval.id);
    try {
      if (approved) {
        await Agent.ApproveAction(approval.id);
      } else {
        await Agent.RejectAction(approval.id);
      }
      await openSnapshot(approval.sessionId);
    } catch (error) {
      toast.error(String(error));
      await openSnapshot(approval.sessionId);
    } finally {
      setDecidingApproval(undefined);
    }
  };

  const revertToMessage = async (entry: LiveAgentChatEntry) => {
    if (!snapshot || runId || hasPendingApproval || reverting) return;
    const sessionId = snapshot.summary.id;
    const generation = ++sessionGeneration.current;
    displayedSessionId.current = sessionId;
    setReverting(true);
    try {
      const value = await Agent.RevertSession(sessionId, entry.sequence);
      if (generation !== sessionGeneration.current || displayedSessionId.current !== sessionId)
        return;
      setSnapshot(value);
      setLiveEntries([]);
      // Only refill an empty composer: overwriting an in-progress draft would discard it silently.
      const revertedText = entry.text;
      if (revertedText) setDraft((current) => (current.trim() ? current : revertedText));
    } catch (error) {
      if (generation === sessionGeneration.current && displayedSessionId.current === sessionId) {
        toast.error(String(error));
      }
    } finally {
      if (generation === sessionGeneration.current && displayedSessionId.current === sessionId) {
        setReverting(false);
      }
    }
  };

  const unrevertSession = async () => {
    if (!snapshot || reverting) return;
    const sessionId = snapshot.summary.id;
    const generation = ++sessionGeneration.current;
    displayedSessionId.current = sessionId;
    setReverting(true);
    try {
      const value = await Agent.UnrevertSession(sessionId);
      if (generation !== sessionGeneration.current || displayedSessionId.current !== sessionId)
        return;
      setSnapshot(value);
    } catch (error) {
      if (generation === sessionGeneration.current && displayedSessionId.current === sessionId) {
        toast.error(String(error));
      }
    } finally {
      if (generation === sessionGeneration.current && displayedSessionId.current === sessionId) {
        setReverting(false);
      }
    }
  };

  const entries = useMemo(
    () => [...(snapshot?.entries ?? []), ...liveEntries],
    [liveEntries, snapshot?.entries],
  );
  const sessionGroups = useMemo(
    () => groupSessions(sessions, t("page.agent.scope_global")),
    [sessions, t],
  );
  const empty = !loading && entries.length === 0;

  return (
    <div className="agent-page flex h-full min-h-0 overflow-hidden bg-background text-foreground">
      <aside className="flex h-full w-(--agent-sidebar-width) max-w-(--agent-sidebar-width) min-w-(--agent-sidebar-width) flex-none flex-col overflow-hidden border-r border-border/78 bg-[color-mix(in_oklab,var(--sidebar)_76%,var(--muted))] px-3 py-1.5">
        <div className="mb-2 flex h-[60px] flex-none items-center px-1 py-2">
          <div className="flex w-full max-w-full min-w-0 items-center gap-[9px] overflow-hidden text-[17px] font-semibold tracking-[-0.01em]">
            <span className="inline-grid size-6 flex-none place-items-center text-(--agent-blue)">
              <SparklesIcon className="size-[22px]" />
            </span>
            <span className="min-w-0 truncate">{t("page.agent.title")}</span>
          </div>
        </div>

        <button
          type="button"
          className="mx-0.5 mb-3 flex h-[38px] flex-none items-center justify-center gap-[7px] rounded-[12px] border border-border bg-background px-4 text-sm font-medium text-foreground transition-colors duration-120 hover:border-[color-mix(in_oklab,var(--border)_72%,var(--foreground))] hover:bg-muted"
          onClick={() => void createSession()}
        >
          <MessageSquarePlusIcon className="size-[15px]" />
          <span>{t("page.agent.new_chat")}</span>
        </button>

        <ScrollArea
          className={cn(
            "m-0 min-h-0 w-full max-w-full min-w-0 flex-1 overflow-hidden",
            scrollAreaClasses,
          )}
        >
          <nav
            className="flex w-full max-w-full min-w-0 flex-col gap-3 overflow-hidden pt-0.5 pr-2 pb-[18px] pl-1"
            aria-label={t("page.agent.recent")}
          >
            {sessionGroups.map((group) => (
              <section key={group.key} className="flex max-w-full min-w-0 flex-col gap-0.5">
                <div
                  className="flex h-8 min-w-0 items-center gap-1.5 px-2 text-xs font-medium text-muted-foreground"
                  title={group.label}
                >
                  <FolderIcon className="size-[15px] flex-none" />
                  <span className="min-w-0 truncate">{group.label}</span>
                </div>
                {group.sessions.map((session) => (
                  <div
                    key={session.id}
                    data-session-id={session.id}
                    data-menu-open={menuSessionId === session.id ? "" : undefined}
                    className={cn(
                      "group relative flex h-[34px] w-full max-w-full min-w-0 items-center rounded-[8px] text-foreground transition-colors duration-100 focus-within:bg-foreground/7 hover:bg-foreground/7",
                      (snapshot?.summary.id === session.id || menuSessionId === session.id) &&
                        "bg-foreground/7",
                    )}
                  >
                    <button
                      type="button"
                      className="flex h-full w-full max-w-full min-w-0 flex-1 items-center overflow-hidden rounded-[inherit] px-2 text-left text-inherit"
                      aria-current={snapshot?.summary.id === session.id ? "page" : undefined}
                      onClick={() =>
                        void navigate({
                          to: "/agent",
                          search: { session: session.id },
                          replace: true,
                        })
                      }
                      onDoubleClick={() => openRenameDialog(session)}
                      title={t("page.agent.rename_hint")}
                    >
                      <span className="grid h-5 w-4 flex-none place-items-center">
                        {session.running && <span className={pulseDot} />}
                      </span>
                      <span className="mr-1.5 ml-1 block max-w-full min-w-0 flex-1 truncate text-[13px] leading-5">
                        {session.title}
                      </span>
                      <span
                        className={cn(
                          "flex-none text-[11px] leading-5 text-muted-foreground group-focus-within:invisible group-hover:invisible",
                          menuSessionId === session.id && "invisible",
                        )}
                      >
                        {formatRelativeTime(session.updatedAt)}
                      </span>
                    </button>
                    {/* Kept in the layout while hidden: the session menu is anchored to this
                     * trigger, so removing its box would let the closing popup jump position. */}
                    <div
                      className={cn(
                        "invisible absolute right-[5px] flex items-center group-focus-within:visible group-hover:visible",
                        menuSessionId === session.id && "visible",
                      )}
                    >
                      <DropdownMenu
                        onOpenChange={(open) => {
                          if (open) setMenuSessionId(session.id);
                        }}
                        onOpenChangeComplete={(open) => {
                          // Hold the row's menu-open state until the popup is gone. Clearing it when
                          // the menu starts closing hides the trigger while the popup animates out.
                          if (!open) {
                            setMenuSessionId((current) =>
                              current === session.id ? undefined : current,
                            );
                          }
                        }}
                      >
                        <DropdownMenuTrigger
                          className="grid size-6 place-items-center rounded-[6px] text-muted-foreground data-popup-open:bg-foreground/9 data-popup-open:text-foreground"
                          aria-label={t("page.agent.session_menu")}
                        >
                          <MoreHorizontalIcon className="size-3.5" />
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end" className="w-44" finalFocus={false}>
                          <DropdownMenuItem onClick={() => openRenameDialog(session)}>
                            <PencilIcon />
                            {t("page.agent.rename_session")}
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            variant="destructive"
                            onClick={() => openDeleteDialog(session)}
                          >
                            <Trash2Icon />
                            {t("page.agent.delete_session")}
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                  </div>
                ))}
              </section>
            ))}
          </nav>
        </ScrollArea>
      </aside>

      <main className="relative flex h-full max-w-full min-w-0 flex-1 flex-col bg-background">
        <header
          className={cn(
            "flex min-h-[76px] flex-none flex-col border-b border-border pt-2.5 pr-7 pl-5",
            empty && "hidden",
          )}
        >
          <div className="flex min-h-[30px] items-center gap-4">
            <div className="flex min-w-0 flex-1 items-center gap-[7px] overflow-hidden text-sm font-medium whitespace-nowrap">
              {snapshot?.summary.scope.type === "mod" && snapshot.summary.scope.modName ? (
                <>
                  <span className="truncate font-normal text-muted-foreground">
                    {snapshot.summary.scope.modName}
                  </span>
                  <span className="font-normal text-muted-foreground">/</span>
                </>
              ) : null}
              <span className="truncate">{snapshot?.summary.title ?? t("page.agent.title")}</span>
              {runId && <span className={cn(pulseDot, "ml-0.5")} />}
            </div>
            <div className="flex max-w-[42%] flex-none items-center gap-1.5 overflow-hidden max-[980px]:hidden">
              {snapshot?.roots?.map((root) => (
                <span
                  key={root.id}
                  className="inline-flex h-[25px] max-w-[180px] min-w-0 items-center gap-1 truncate overflow-hidden rounded-[13px] bg-muted px-2 text-[11px] whitespace-nowrap text-muted-foreground"
                  title={root.path}
                >
                  <FolderIcon className="size-3 flex-none" />
                  {root.name}
                </span>
              ))}
            </div>
          </div>
          <div className="mt-2.5 flex gap-9 pl-2">
            <span className="relative pb-[9px] text-[13px] leading-4 font-medium text-(--agent-blue) after:absolute after:inset-x-0 after:-bottom-px after:h-0.5 after:rounded-[2px] after:bg-(--agent-blue) after:content-['']">
              {t("page.agent.chat")}
            </span>
          </div>
        </header>

        <div className="relative flex min-h-0 flex-1">
          <ScrollArea
            className={cn(
              "min-w-0 flex-1",
              scrollAreaClasses,
              "[&_[data-slot=scroll-area-content]]:min-h-full",
            )}
            viewportClassName="min-h-full"
          >
            <div className="relative mx-auto flex min-h-full w-[min(var(--agent-chat-width),calc(100%_-_64px))] flex-col gap-[18px] overflow-hidden pt-7 pb-[180px] max-[980px]:w-[calc(100%_-_40px)] [&>*]:max-w-full [&>*]:min-w-0">
              {loading && (
                <Loader2Icon className="m-auto size-5 animate-spin text-muted-foreground" />
              )}
              {snapshot?.unavailableReason && (
                <div className="rounded-[10px] border border-destructive/30 bg-destructive/8 px-3 py-2.5 text-[13px] text-destructive">
                  {snapshot.unavailableReason}
                </div>
              )}
              {empty && (
                <div className="absolute inset-x-0 top-[clamp(72px,23%,180px)] flex flex-col items-center gap-2 px-6 text-center">
                  <div className="flex items-center justify-center gap-2.5 text-[26px] leading-8 font-medium tracking-[-0.025em]">
                    <span className="inline-grid size-[34px] flex-none place-items-center text-(--agent-blue)">
                      <SparklesIcon className="size-[30px]" />
                    </span>
                    <span>{t("page.agent.hero_title")}</span>
                  </div>
                  <p className="max-w-[520px] text-[13px] leading-5 text-muted-foreground">
                    {t("page.agent.empty")}
                  </p>
                </div>
              )}
              {!empty &&
                entries.map((entry, index) => (
                  <div
                    key={`${entry.sequence}-${entry.type}-${index}`}
                    className={cn("max-w-full min-w-0", entry.reverted && "opacity-45")}
                  >
                    {entry.approval ? (
                      <ApprovalCard
                        approval={entry.approval}
                        busy={decidingApproval === entry.approval.id}
                        onDecision={decideApproval}
                      />
                    ) : (
                      <ChatEntry
                        entry={entry}
                        onRevert={revertToMessage}
                        revertDisabled={!!runId || hasPendingApproval || reverting}
                      />
                    )}
                  </div>
                ))}
              {runId && liveEntries.length === 0 && (
                <div className="inline-flex h-[26px] animate-[agent-shimmer_1.8s_linear_infinite] items-center self-start bg-[linear-gradient(90deg,#4176e6_0%,#4176e6_38%,#b7c8fe_50%,#4176e6_62%,#4176e6_100%)] [background-size:250%_100%] bg-clip-text [background-position:100%_50%] text-sm font-medium text-transparent motion-reduce:animate-none">
                  <span>{t("page.agent.thinking")}</span>
                </div>
              )}
              <div ref={bottomRef} />
            </div>
          </ScrollArea>

          <Composer
            draft={draft}
            setDraft={setDraft}
            images={images}
            onAddImages={addImages}
            onRemoveImage={(index) =>
              setImages((current) => current.filter((_, entryIndex) => entryIndex !== index))
            }
            snapshot={snapshot}
            runId={runId}
            hasPendingApproval={hasPendingApproval}
            reverting={reverting}
            empty={empty}
            onSend={send}
            onUnrevert={unrevertSession}
          />
        </div>
      </main>

      <Dialog
        open={renameTarget !== undefined}
        onOpenChange={(open) => {
          if (!open && !renaming) setRenameTarget(undefined);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("page.agent.rename_title")}</DialogTitle>
            <DialogDescription>{t("page.agent.rename_description")}</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={submitRename}>
            <Input
              ref={renameInputRef}
              value={renameValue}
              aria-label={t("page.agent.rename_placeholder")}
              placeholder={t("page.agent.rename_placeholder")}
              maxLength={80}
              disabled={renaming}
              onChange={(event) => setRenameValue(event.target.value)}
            />
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                disabled={renaming}
                onClick={() => setRenameTarget(undefined)}
              >
                {t("g.cancel")}
              </Button>
              <Button type="submit" disabled={renaming || !renameValue.trim()}>
                {t("page.agent.rename_confirm")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={deleteTarget !== undefined}
        onOpenChange={(open) => {
          if (!open && !deleting) setDeleteTarget(undefined);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.agent.delete_title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.agent.delete_description", { title: deleteTarget?.title ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={deleting}
              onClick={() => void confirmDelete()}
            >
              {t("page.agent.delete_confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function Composer({
  draft,
  setDraft,
  images,
  onAddImages,
  onRemoveImage,
  snapshot,
  runId,
  hasPendingApproval,
  reverting,
  empty,
  onSend,
  onUnrevert,
}: {
  draft: string;
  setDraft: (value: string) => void;
  images: PendingImage[];
  onAddImages: (files: File[]) => Promise<void>;
  onRemoveImage: (index: number) => void;
  snapshot?: AgentSessionSnapshot;
  runId?: string;
  hasPendingApproval: boolean;
  reverting: boolean;
  empty: boolean;
  onSend: () => Promise<void>;
  onUnrevert: () => Promise<void>;
}) {
  const { t } = useTranslation();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const unavailable = !!snapshot?.unavailableReason;
  const disabled = hasPendingApproval || reverting || unavailable;
  const canAttachImages = !!snapshot?.supportsImages;
  const attachHint = t("page.agent.attach_hint", {
    count: maxAttachedImages,
    limit: maxImageBytes >> 20,
  });
  const scopeLabel =
    snapshot?.summary.scope.type === "mod" && snapshot.summary.scope.modName
      ? snapshot.summary.scope.modName
      : t("page.agent.scope_global");

  return (
    <div
      className={cn(
        "pointer-events-none absolute inset-x-0 bottom-0 z-7 flex justify-center bg-[linear-gradient(180deg,transparent_0,var(--background)_36px)] px-4 pt-9 pb-3",
        empty && "top-[clamp(190px,42%,350px)] bottom-auto bg-transparent bg-none pt-0",
      )}
    >
      <div
        className={cn(
          "pointer-events-auto flex w-(--agent-composer-width) flex-col gap-[9px] rounded-[22px] border border-border bg-[color-mix(in_oklab,var(--card)_96%,var(--muted))] p-2 pb-[7px] shadow-[0_1px_2px_rgb(0_0_0/6%),0_8px_30px_rgb(0_0_0/8%)] transition-[border-color,box-shadow] duration-120 focus-within:border-[color-mix(in_oklab,var(--agent-blue)_45%,var(--border))] focus-within:shadow-[0_1px_2px_rgb(0_0_0/6%),0_8px_30px_rgb(0_0_0/9%)]",
          empty && "w-[min(712px,calc(100%_-_48px))]",
          dragging && "border-(--agent-blue)",
        )}
        onDragOver={(event) => {
          if (!canAttachImages || !event.dataTransfer.types.includes("Files")) return;
          event.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(event) => {
          if (!canAttachImages) return;
          event.preventDefault();
          setDragging(false);
          void onAddImages([...event.dataTransfer.files]);
        }}
      >
        {snapshot?.revert && (
          <div className="flex items-center justify-between gap-3 rounded-[14px] bg-muted px-3 py-2">
            <div className="min-w-0">
              <p className="truncate text-xs font-medium text-foreground">
                {t("page.agent.revert_pending", { count: snapshot.revert.revertedCount })}
              </p>
              <p className="truncate text-[11px] text-muted-foreground">
                {t("page.agent.revert_files_notice")}
              </p>
            </div>
            <button
              type="button"
              disabled={reverting}
              className="flex-none rounded-[8px] border border-border bg-background px-2.5 py-1 text-xs font-medium text-foreground transition-colors duration-100 hover:bg-background/60 disabled:cursor-default disabled:opacity-35"
              onClick={() => void onUnrevert()}
            >
              {t("page.agent.revert_undo")}
            </button>
          </div>
        )}
        {images.length > 0 && (
          <div className="flex flex-wrap gap-2 px-[7px] pt-[5px]">
            {images.map((image, index) => (
              <div key={`${image.name}-${index}`} className="relative">
                <img
                  src={`data:${image.mimeType};base64,${image.data}`}
                  alt={image.name}
                  className="size-16 rounded-[10px] border border-border object-cover"
                />
                <button
                  type="button"
                  aria-label={t("page.agent.remove_image")}
                  className="absolute -top-1.5 -right-1.5 grid size-5 place-items-center rounded-full border border-border bg-background text-muted-foreground shadow-sm transition-colors duration-100 hover:text-foreground"
                  onClick={() => onRemoveImage(index)}
                >
                  <XIcon className="size-3" />
                </button>
              </div>
            ))}
          </div>
        )}
        <Textarea
          value={draft}
          disabled={disabled}
          aria-label={t("page.agent.placeholder")}
          placeholder={t("page.agent.placeholder")}
          className="max-h-[260px] min-h-[52px] resize-none rounded-[14px] border-0 bg-transparent! px-[7px] pt-[5px] pb-0 text-sm leading-6 shadow-none focus-visible:ring-0"
          onChange={(event) => setDraft(event.target.value)}
          onPaste={(event) => {
            if (!canAttachImages) return;
            const files = [...event.clipboardData.files].filter((file) =>
              file.type.startsWith("image/"),
            );
            // Keep a text paste intact when the clipboard carries text as well.
            if (files.length === 0 || event.clipboardData.getData("text/plain").trim()) return;
            event.preventDefault();
            void onAddImages(files);
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              void onSend();
            }
          }}
        />
        <div className="flex items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-1">
            {canAttachImages && (
              <>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept={supportedImageTypes.join(",")}
                  multiple
                  className="hidden"
                  onChange={(event) => {
                    void onAddImages([...(event.target.files ?? [])]);
                    event.target.value = "";
                  }}
                />
                <button
                  type="button"
                  aria-label={t("page.agent.attach_image")}
                  title={attachHint}
                  disabled={disabled}
                  className="grid size-7 flex-none place-items-center rounded-[9px] text-muted-foreground transition-colors duration-100 hover:bg-muted hover:text-foreground disabled:cursor-default disabled:opacity-35"
                  onClick={() => fileInputRef.current?.click()}
                >
                  <PaperclipIcon className="size-4" />
                </button>
              </>
            )}
            <div
              className="flex h-7 max-w-[calc(100%_-_50px)] min-w-0 items-center gap-[5px] rounded-[9px] px-2 text-xs font-medium text-muted-foreground"
              title={scopeLabel}
            >
              <FolderIcon className="size-3.5 flex-none" />
              <span className="min-w-0 truncate">{scopeLabel}</span>
            </div>
          </div>
          {runId ? (
            <button
              type="button"
              className="grid size-[34px] flex-none place-items-center rounded-full bg-foreground text-background transition-colors duration-100"
              aria-label={t("page.agent.stop")}
              onClick={() => snapshot && void Agent.Cancel(snapshot.summary.id, runId)}
            >
              <CircleStopIcon className="size-[17px] stroke-[2.2]" />
            </button>
          ) : (
            <button
              type="button"
              className="grid size-[34px] flex-none place-items-center rounded-full bg-(--agent-blue) text-white transition-colors duration-100 hover:bg-[#679efe] disabled:cursor-default disabled:opacity-35"
              aria-label={t("page.agent.send")}
              disabled={(!draft.trim() && images.length === 0) || disabled}
              onClick={() => void onSend()}
            >
              <ArrowUpIcon className="size-[17px] stroke-[2.2]" />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function ApprovalCard({
  approval,
  busy,
  onDecision,
}: {
  approval: AgentApproval;
  busy: boolean;
  onDecision: (approval: AgentApproval, approved: boolean) => Promise<void>;
}) {
  const { t } = useTranslation();
  const pending = approval.status === "pending";

  return (
    <section className="rounded-[14px] border border-[#f59e0b]/42 bg-[color-mix(in_oklab,#f59e0b_6%,var(--background))] p-4 text-[13px]">
      <div className="flex items-center gap-2 font-semibold">
        <ShieldAlertIcon className="size-4 text-[#d97706]" />
        <span>{t("page.agent.approval_title")}</span>
        <span className="ml-auto rounded-[10px] bg-muted px-[7px] py-0.5 text-[10px] font-medium text-muted-foreground uppercase">
          {t(`page.agent.approval_status_${approval.status}`)}
        </span>
      </div>
      <p className="mt-3">{approval.summary}</p>
      {approval.target && (
        <div className="mt-[7px] text-xs wrap-anywhere text-muted-foreground">
          <strong className="text-foreground">{t("page.agent.approval_target")}:</strong>{" "}
          {approval.target}
        </div>
      )}
      <p className="mt-[7px] text-xs wrap-anywhere text-muted-foreground">{approval.impact}</p>
      {approval.error && <p className="text-destructive">{approval.error}</p>}
      {pending && (
        <div className="mt-3.5 flex justify-end gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => void onDecision(approval, false)}
          >
            <XIcon />
            {t("page.agent.reject")}
          </Button>
          <Button size="sm" disabled={busy} onClick={() => void onDecision(approval, true)}>
            {busy ? <Loader2Icon className="animate-spin" /> : <CheckIcon />}
            {t("page.agent.approve")}
          </Button>
        </div>
      )}
    </section>
  );
}

function ReasoningPanel({ text, streaming = false }: { text: string; streaming?: boolean }) {
  const { t } = useTranslation();

  return (
    <details
      open={streaming}
      className="agent-reasoning group text-[13px] leading-5 text-muted-foreground"
    >
      <summary className="flex min-h-[26px] w-fit cursor-pointer list-none items-center gap-[7px] rounded-[6px] select-none hover:text-foreground [&::-webkit-details-marker]:hidden">
        {streaming ? (
          <Loader2Icon className="size-3.5 animate-spin" />
        ) : (
          <BrainIcon className="size-3.5" />
        )}
        <span>{t("page.agent.reasoning")}</span>
        <ChevronDownIcon className={disclosure} />
      </summary>
      <div className="mt-1 py-0.5 pl-[21px] wrap-anywhere whitespace-pre-wrap text-muted-foreground">
        {text}
      </div>
    </details>
  );
}

function AgentImages({ images, alt }: { images?: AgentImage[] | null; alt: string }) {
  if (!images?.length) return null;

  return (
    <>
      {images.map((image, index) => (
        <img
          key={`${image.path || image.src}-${index}`}
          src={agentImageSrc(image)}
          alt={image.name || alt}
          className="max-h-64 max-w-full rounded-[10px] border border-border object-contain"
        />
      ))}
    </>
  );
}

function ChatEntry({
  entry,
  onRevert,
  revertDisabled = false,
}: {
  entry: LiveAgentChatEntry;
  onRevert?: (entry: LiveAgentChatEntry) => void;
  revertDisabled?: boolean;
}) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  if (entry.role === "user") {
    const copy = async () => {
      if (!entry.text) return;
      try {
        await navigator.clipboard.writeText(entry.text);
        setCopied(true);
        window.setTimeout(() => setCopied(false), 1500);
      } catch {
        toast.error(t("page.agent.copy_failed"));
      }
    };

    return (
      <div className="flex justify-end">
        <div className="flex w-fit max-w-[min(70%,640px)] min-w-[176px] flex-col items-end gap-1">
          <div className="max-w-full min-w-0 rounded-[22px] bg-[color-mix(in_oklab,var(--muted)_88%,var(--background))] px-4 py-2.5 text-sm leading-[22px] wrap-anywhere whitespace-pre-wrap">
            <AgentImages images={entry.images} alt="" />
            {entry.text}
          </div>
          <div className="flex w-full items-center justify-between gap-2 px-1">
            <button
              type="button"
              aria-label={t("page.agent.revert_message")}
              title={t("page.agent.revert_message")}
              disabled={revertDisabled || !onRevert}
              className="grid size-6 flex-none place-items-center rounded-[7px] text-muted-foreground transition-colors duration-100 hover:bg-muted hover:text-foreground disabled:cursor-default disabled:opacity-35"
              onClick={() => onRevert?.(entry)}
            >
              <Undo2Icon className="size-3.5" />
            </button>
            <button
              type="button"
              aria-label={copied ? t("page.agent.copied") : t("page.agent.copy_message")}
              title={copied ? t("page.agent.copied") : t("page.agent.copy_message")}
              disabled={!entry.text}
              className="grid size-6 flex-none place-items-center rounded-[7px] text-muted-foreground transition-colors duration-100 hover:bg-muted hover:text-foreground disabled:cursor-default disabled:opacity-35"
              onClick={() => void copy()}
            >
              {copied ? <CheckIcon className="size-3.5" /> : <CopyIcon className="size-3.5" />}
            </button>
          </div>
        </div>
      </div>
    );
  }
  if (entry.role === "assistant") {
    return (
      <div className="agent-assistant-message max-w-full min-w-0 overflow-hidden wrap-anywhere text-foreground">
        {entry.reasoning && (
          <ReasoningPanel text={entry.reasoning} streaming={entry.reasoningStreaming} />
        )}
        {entry.text && <Markdown text={entry.text} streaming={entry.streaming} />}
      </div>
    );
  }
  if (entry.type.startsWith("tool/")) {
    return (
      <details
        className="group text-[13px] leading-5 text-muted-foreground"
        open={entry.type === "tool/start"}
      >
        <summary className="flex min-h-[26px] w-fit cursor-pointer list-none items-center gap-[7px] rounded-[6px] select-none hover:text-foreground [&::-webkit-details-marker]:hidden">
          {entry.type === "tool/start" ? (
            <Loader2Icon className="size-3.5 animate-spin" />
          ) : (
            <WrenchIcon className="size-3.5" />
          )}
          <span>{entry.toolName}</span>
          <ChevronDownIcon className={disclosure} />
        </summary>
        <div className="mt-1 max-w-full min-w-0 overflow-hidden py-1 pl-[21px] wrap-anywhere">
          {entry.error && <p className="text-destructive">{entry.error}</p>}
          {!!entry.changedFiles?.length && (
            <ul className="my-1 pl-4.5">
              {entry.changedFiles.map((file) => (
                <li key={file}>{file}</li>
              ))}
            </ul>
          )}
          {!!entry.images?.length && (
            <div className="my-1.5 flex flex-wrap gap-2">
              <AgentImages images={entry.images} alt={entry.toolName ?? ""} />
            </div>
          )}
          {entry.result !== undefined && (
            <pre className="mt-1.5 max-h-64 w-full max-w-full min-w-0 overflow-auto rounded-[8px] bg-muted px-3 py-2.5 font-mono text-[11px] leading-[17px] wrap-anywhere whitespace-pre-wrap">
              {JSON.stringify(entry.result, null, 2)}
            </pre>
          )}
        </div>
      </details>
    );
  }
  if (entry.error) {
    return (
      <div className="rounded-[10px] border border-destructive/30 bg-destructive/8 px-3 py-2.5 text-[13px] text-destructive">
        {entry.error}
      </div>
    );
  }
  return null;
}

function sameAgentScope(left: AgentScope, right: AgentScope) {
  return left.type === right.type && (left.modPath ?? "") === (right.modPath ?? "");
}

async function findEmptySession(sessions: AgentSessionSummary[], scope: AgentScope) {
  const candidates = sessions.filter((session) => sameAgentScope(session.scope, scope));
  const snapshots = await Promise.all(candidates.map((session) => Agent.GetSession(session.id)));
  return candidates.find((session, index) => (snapshots[index]?.entries?.length ?? 0) === 0);
}

function groupSessions(sessions: AgentSessionSummary[], globalLabel: string): SessionGroup[] {
  const groups = new Map<string, SessionGroup>();
  for (const session of sessions) {
    const isMod = session.scope.type === "mod" && !!session.scope.modName;
    const key = isMod ? `mod:${session.scope.modPath || session.scope.modName}` : "global";
    const label = isMod ? (session.scope.modName ?? globalLabel) : globalLabel;
    const group = groups.get(key) ?? { key, label, sessions: [] };
    group.sessions.push(session);
    groups.set(key, group);
  }
  return [...groups.values()];
}

function formatRelativeTime(value: string) {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "";
  const elapsedMinutes = Math.max(0, Math.round((Date.now() - timestamp) / 60_000));
  if (elapsedMinutes < 1) return "now";
  if (elapsedMinutes < 60) return `${elapsedMinutes}m`;
  const elapsedHours = Math.round(elapsedMinutes / 60);
  if (elapsedHours < 24) return `${elapsedHours}h`;
  const elapsedDays = Math.round(elapsedHours / 24);
  return `${elapsedDays}d`;
}
