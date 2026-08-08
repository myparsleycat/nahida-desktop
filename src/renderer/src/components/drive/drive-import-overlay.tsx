import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@renderer/components/ui/input-group";
import { Label } from "@renderer/components/ui/label";
import { Progress } from "@renderer/components/ui/progress";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useAuth } from "@renderer/hooks/use-auth";
import { getSetting } from "@renderer/lib/settings";
import { cn } from "@renderer/lib/utils";
import { useViewStore } from "@renderer/store/drive";
import type {
  DriveCopyProgress,
  DriveImportContent,
  DriveListChildrenResult,
  DriveResolveImportSourceResult,
} from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeftIcon,
  CheckIcon,
  CloudDownloadIcon,
  FolderIcon,
  LinkIcon,
  Loader2Icon,
  LockIcon,
  XIcon,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { ImportFolderTree, type TreeNode } from "./import-folder-tree";

type WizardStep = 1 | 2 | 3;

type NodeData = {
  content: DriveImportContent;
  children: string[];
  depth: number;
  loaded: boolean;
};

function getErrorCode(error: unknown) {
  if (!error || typeof error !== "object") return undefined;
  const code = (error as { code?: unknown }).code;
  if (typeof code === "string") return code;
  const match = toErrorMessage(error).match(/\b(DRIVE_[A-Z0-9_]+)\b/);
  return match ? match[1] : undefined;
}

function isNotFoundError(error: unknown) {
  const code = getErrorCode(error) ?? "";
  const msg = toErrorMessage(error).toLowerCase();
  return (
    code.includes("NOT_FOUND") ||
    code.includes("EXPIRED") ||
    msg.includes("not found") ||
    msg.includes("expired") ||
    msg.includes("404")
  );
}

function isValidDriveUrl(value: string) {
  try {
    const url = new URL(value);
    if (url.protocol !== "https:") return false;
    if (!["nahida.live", "www.nahida.live"].includes(url.hostname.toLowerCase())) return false;
    return (
      /^\/akasha\/link\/[A-Za-z0-9_-]+\/?$/i.test(url.pathname) ||
      /^\/akasha\/mod\/[A-Za-z0-9_-]+\/?$/i.test(url.pathname)
    );
  } catch {
    return false;
  }
}

export function DriveImportOverlay({ destinationId }: { destinationId: string }) {
  const { t } = useTranslation();
  const { session, sessionInitialized, startLogin } = useAuth();
  const queryClient = useQueryClient();

  const importOverlay = useViewStore((s) => s.importOverlay);
  const setImportOverlay = useViewStore((s) => s.setImportOverlay);

  const [url, setUrl] = useState("");
  const [password, setPassword] = useState("");
  const [requiresPassword, setRequiresPassword] = useState(false);
  const [passwordInvalid, setPasswordInvalid] = useState(false);
  const [resolveFailed, setResolveFailed] = useState(false);
  const [step, setStep] = useState<WizardStep>(1);
  const [resolving, setResolving] = useState(false);
  const [sourceInfo, setSourceInfo] = useState<DriveResolveImportSourceResult | null>(null);
  const [treeData, setTreeData] = useState<Map<string, NodeData>>(new Map());
  const [rootId, setRootId] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [loadingIds, setLoadingIds] = useState<Set<string>>(new Set());
  const [copyProgress, setCopyProgress] = useState<DriveCopyProgress | null>(null);
  const [isImporting, setIsImporting] = useState(false);

  const copyOperationIdRef = useRef<string | undefined>(undefined);
  const isPendingRef = useRef(false);
  const urlInputRef = useRef<HTMLInputElement>(null);
  const passwordInputRef = useRef<HTMLInputElement>(null);
  const loadSeqRef = useRef(0);
  const lastResolvedUrlRef = useRef("");

  const destinationQuery = useQuery({
    queryKey: ["drive", "import-destination", destinationId],
    enabled: !!importOverlay && !!destinationId,
    queryFn: async () => await window.api.invoke("drive:get:item", destinationId),
  });

  // reset when overlay opens
  useEffect(() => {
    if (!importOverlay) return;
    setUrl(importOverlay.url);
    setPassword("");
    setRequiresPassword(false);
    setPasswordInvalid(false);
    setResolveFailed(false);
    setStep(1);
    setResolving(false);
    setSourceInfo(null);
    setTreeData(new Map());
    setRootId(null);
    setExpanded(new Set());
    setSelected(new Set());
    setLoadingIds(new Set());
    setCopyProgress(null);
    setIsImporting(false);
    copyOperationIdRef.current = undefined;
    isPendingRef.current = false;
    loadSeqRef.current = 0;
    lastResolvedUrlRef.current = "";
    requestAnimationFrame(() => urlInputRef.current?.focus());
  }, [importOverlay]);

  useEffect(() => {
    if (!importOverlay) return;
    return window.api.on("drive:copy-progress", (progress) => {
      if (progress.operationId !== copyOperationIdRef.current) return;
      setCopyProgress(progress);
    });
  }, [importOverlay]);

  // auto-resolve when the URL becomes a valid drive source URL
  useEffect(() => {
    if (!importOverlay) return;
    const trimmed = url.trim();
    if (trimmed === lastResolvedUrlRef.current) return;
    loadSeqRef.current += 1;
    setSourceInfo(null);
    setTreeData(new Map());
    setRootId(null);
    setExpanded(new Set());
    setSelected(new Set());
    setStep(1);
    setRequiresPassword(false);
    setPasswordInvalid(false);
    setResolveFailed(false);
    if (!isValidDriveUrl(trimmed)) return;
    if (!session) return;
    const seq = ++loadSeqRef.current;
    const timer = setTimeout(() => {
      if (seq === loadSeqRef.current) void handleResolve(seq);
    }, 500);
    return () => clearTimeout(timer);
  }, [url, importOverlay, session]);

  // 가져오기는 transfer로 승격되어 백그라운드에서 계속되므로
  // overlay 닫힘/페이지 이동이 전송을 취소하면 안된다.
  // 취소는 명시적 handleCancel 또는 transfer 탭에서만 수행한다.

  // focus password when required
  useEffect(() => {
    if (requiresPassword) requestAnimationFrame(() => passwordInputRef.current?.focus());
  }, [requiresPassword]);

  const descendantMap = useMemo(() => {
    const map = new Map<string, Set<string>>();
    // build parent -> descendants via BFS from treeData
    // For each node, collect all descendant folder ids (only folders)
    const getDescendants = (nodeId: string, visited = new Set<string>()): Set<string> => {
      if (visited.has(nodeId)) return new Set();
      visited.add(nodeId);
      const node = treeData.get(nodeId);
      if (!node) return new Set();
      const result = new Set<string>();
      for (const childId of node.children) {
        const child = treeData.get(childId);
        if (!child) continue;
        if (child.content.isDir) {
          result.add(childId);
          const sub = getDescendants(childId, visited);
          for (const s of sub) result.add(s);
        } else {
          // files not counted for folder selection descendantMap, but we still traverse if it were folder
        }
        // also need to consider deeper via child's descendants already
        // Actually we already added via recursion, but need to ensure we also add grandchildren via child's descendant set
        // The recursion above handles.
      }
      return result;
    };
    for (const [id, node] of treeData) {
      if (!node.content.isDir) continue;
      map.set(id, getDescendants(id));
    }
    return map;
  }, [treeData]);

  const visibleNodes: TreeNode[] = useMemo(() => {
    if (!rootId) return [];
    const result: TreeNode[] = [];
    const virtualModRoot = rootId.startsWith("mod:virtual:");
    const stack: string[] = [];
    // if mod virtual root, start with its children, not itself
    if (virtualModRoot) {
      const rootNode = treeData.get(rootId);
      if (rootNode) {
        for (let i = rootNode.children.length - 1; i >= 0; i--) stack.push(rootNode.children[i]);
      }
    } else {
      stack.push(rootId);
    }
    while (stack.length > 0) {
      const id = stack.pop()!;
      const node = treeData.get(id);
      if (!node) continue;
      result.push({
        id: node.content.id,
        name: node.content.name,
        isDir: node.content.isDir,
        size: node.content.size,
        depth: node.depth,
      });
      if (node.content.isDir && expanded.has(id) && node.loaded) {
        // push children in reverse to keep order
        for (let i = node.children.length - 1; i >= 0; i--) stack.push(node.children[i]);
      }
    }
    return result;
  }, [rootId, treeData, expanded]);

  const handleResolve = useCallback(
    async (seqArg?: number) => {
      const trimmed = url.trim();
      if (!trimmed) {
        toast.warning(t("page.drive.import.url_required"));
        return;
      }
      if (!session) {
        if (sessionInitialized) {
          toast.warning(t("page.drive.import.login_required"));
          await startLogin();
        }
        return;
      }
      const seq = seqArg ?? ++loadSeqRef.current;
      setResolving(true);
      setRequiresPassword(false);
      setPasswordInvalid(false);
      setResolveFailed(false);
      setCopyProgress(null);
      try {
        const resolved = (await window.api.invoke("drive:fn:resolveImportSource", {
          url: trimmed,
          password: password || undefined,
        })) as unknown as DriveResolveImportSourceResult;
        applyResolvedResult(resolved, seq, trimmed);
      } catch (error) {
        const code = getErrorCode(error);
        const msg = toErrorMessage(error);
        if (code === "DRIVE_LINK_PASSWORD_REQUIRED" || code === "DRIVE_MOD_PASSWORD_REQUIRED") {
          const autoTried = await tryAutoPasswords(trimmed, seq);
          if (autoTried || seq !== loadSeqRef.current) return;
          setRequiresPassword(true);
          setResolveFailed(true);
          toast.warning(t("page.drive.import.password_required"));
          setStep(1);
          return;
        }
        if (code === "DRIVE_LINK_INVALID_PASSWORD" || code === "DRIVE_MOD_INVALID_PASSWORD") {
          setRequiresPassword(true);
          setPasswordInvalid(true);
          setResolveFailed(true);
          toast.error(t("page.drive.import.invalid_password"));
          setStep(1);
          return;
        }
        if (code === "DRIVE_INVALID_SOURCE_URL" || msg.includes("DRIVE_INVALID_SOURCE_URL")) {
          setResolveFailed(true);
          toast.error("유효하지 않은 링크입니다.", {
            description: "나히다 공유 링크 또는 컬렉션 URL을 입력하세요.",
          });
          return;
        }
        if (isNotFoundError(error)) {
          setResolveFailed(true);
          toast.error("링크가 만료되었거나 존재하지 않습니다.", { description: msg });
          // reset to initial ui
          setSourceInfo(null);
          setTreeData(new Map());
          setRootId(null);
          setExpanded(new Set());
          setSelected(new Set());
          setStep(1);
          setRequiresPassword(false);
          setPasswordInvalid(false);
          setPassword("");
          return;
        }
        if (msg.includes("DRIVE_COPY_CANCELED")) return;
        setResolveFailed(true);
        toast.error(t("page.drive.import.failed"), { description: msg });
      } finally {
        if (seq === loadSeqRef.current) setResolving(false);
      }
    },
    [url, password, session, sessionInitialized, startLogin, t],
  );

  const tryAutoPasswords = useCallback(
    async (trimmedUrl: string, seq: number): Promise<boolean> => {
      try {
        const settings = await getSetting(["drive.autoTryPasswords", "drive.passwordList"]);
        if (seq !== loadSeqRef.current) return true;
        if (!settings["drive.autoTryPasswords"]) return false;

        const candidates = settings["drive.passwordList"].filter(Boolean);
        if (candidates.length === 0) return false;

        const winner = await new Promise<{
          resolved: DriveResolveImportSourceResult;
          password: string;
        } | null>((resolve) => {
          let pending = candidates.length;
          let settled = false;

          for (const candidate of candidates) {
            void window.api
              .invoke("drive:fn:resolveImportSource", {
                url: trimmedUrl,
                password: candidate,
              })
              .then((resolved) => {
                if (settled) return;
                settled = true;
                resolve({
                  resolved: resolved as unknown as DriveResolveImportSourceResult,
                  password: candidate,
                });
              })
              .catch(() => {
                pending -= 1;
                if (!settled && pending === 0) resolve(null);
              });
          }
        });

        if (seq !== loadSeqRef.current) return true;
        if (!winner) return false;

        setPassword(winner.password);
        setRequiresPassword(false);
        setPasswordInvalid(false);
        applyResolvedResult(winner.resolved, seq, trimmedUrl);
        return true;
      } catch {
        return false;
      }
    },
    [],
  );

  function applyResolvedResult(
    resolved: DriveResolveImportSourceResult,
    seq: number,
    trimmedUrl: string,
  ) {
    if (seq !== loadSeqRef.current) return;
    setSourceInfo(resolved);
    lastResolvedUrlRef.current = trimmedUrl;
    // Build initial tree
    if (resolved.source === "link") {
      const rid = resolved.parent.id;
      const rootContent: DriveImportContent = {
        id: rid,
        name: resolved.parent.name,
        isDir: true,
        size: null,
        mimeType: null,
        parentId: null,
        createdAt: new Date(),
        updatedAt: new Date(),
      };
      const newMap = new Map<string, NodeData>();
      newMap.set(rid, { content: rootContent, children: [], depth: 0, loaded: false });
      setTreeData(newMap);
      setRootId(rid);
      setExpanded(new Set([rid]));
      setSelected(new Set());
      setStep(3);
      // load children
      void loadLinkChildren(rid, resolved.linkId, resolved.token, newMap, rid);
    } else {
      // mod
      const virtualId = `mod:virtual:${resolved.modId}`;
      const modTitle =
        (resolved.modData as unknown as { mod?: { title?: string } }).mod?.title ??
        (resolved.modData as unknown as { title?: string }).title ??
        resolved.modId;
      const virtualContent: DriveImportContent = {
        id: virtualId,
        name: String(modTitle),
        isDir: true,
        size: null,
        mimeType: null,
        parentId: null,
        createdAt: new Date(),
        updatedAt: new Date(),
      };
      const newMap = new Map<string, NodeData>();
      newMap.set(virtualId, { content: virtualContent, children: [], depth: 0, loaded: true });
      const collectionNodes: NodeData[] = [];
      for (const col of resolved.modData.collections as Array<{
        id: string;
        name: string;
        rootId: string | null;
        private: boolean;
      }>) {
        if (!col.rootId) continue;
        // skip private already filtered in backend, but keep
        const colContent: DriveImportContent = {
          id: col.rootId,
          name: col.name,
          isDir: true,
          size: null,
          mimeType: null,
          parentId: virtualId,
          createdAt: new Date(),
          updatedAt: new Date(),
        };
        collectionNodes.push({ content: colContent, children: [], depth: 1, loaded: false });
        newMap.get(virtualId)!.children.push(col.rootId);
        newMap.set(col.rootId, { content: colContent, children: [], depth: 1, loaded: false });
      }
      if (collectionNodes.length === 0) {
        toast.warning("가져올 수 있는 컬렉션이 없습니다.");
      }
      setTreeData(newMap);
      setRootId(virtualId);
      setExpanded(new Set([virtualId, ...collectionNodes.map((n) => n.content.id)]));
      setSelected(new Set());
      setStep(3);
      // Optionally auto-load first collection's children
      for (const n of collectionNodes) {
        void loadModChildren(n.content.id, resolved.token, resolved.sig, newMap, virtualId);
      }
    }
    setRequiresPassword(false);
    setPasswordInvalid(false);
  }

  const loadLinkChildren = async (
    parentId: string,
    linkId: string,
    linkToken: string,
    currentMap?: Map<string, NodeData>,
    _currentRootId?: string | null,
  ) => {
    setLoadingIds((prev) => new Set(prev).add(parentId));
    try {
      const result: DriveListChildrenResult = await window.api.invoke("drive:fn:listLinkChildren", {
        linkId,
        linkToken,
        itemId: parentId,
      });
      setTreeData((prev) => {
        const base = currentMap ? new Map(currentMap) : new Map(prev);
        const parentNode = base.get(parentId);
        if (!parentNode) return prev;
        const parentDepth = parentNode.depth;
        const childIds: string[] = [];
        for (const child of result.children) {
          const cid = child.id;
          childIds.push(cid);
          if (!base.has(cid)) {
            base.set(cid, {
              content: child,
              children: [],
              depth: parentDepth + 1,
              loaded: false,
            });
          }
        }
        base.set(parentId, { ...parentNode, children: childIds, loaded: true });
        return base;
      });
    } catch (error) {
      if (isNotFoundError(error)) {
        toast.error("링크가 만료되었습니다. 처음부터 다시 시도하세요.");
        setSourceInfo(null);
        setTreeData(new Map());
        setRootId(null);
        setExpanded(new Set());
        setSelected(new Set());
        setStep(1);
        setRequiresPassword(false);
        return;
      }
      toast.error("폴더 목록을 불러오지 못했습니다", { description: toErrorMessage(error) });
    } finally {
      setLoadingIds((prev) => {
        const n = new Set(prev);
        n.delete(parentId);
        return n;
      });
    }
  };

  const loadModChildren = async (
    parentId: string,
    modToken?: string,
    modSig?: string,
    currentMap?: Map<string, NodeData>,
    _virtualRoot?: string | null,
  ) => {
    setLoadingIds((prev) => new Set(prev).add(parentId));
    try {
      const result: DriveListChildrenResult = await window.api.invoke("drive:fn:listModChildren", {
        itemId: parentId,
        modToken,
        modSig,
      });
      setTreeData((prev) => {
        const base = currentMap ? new Map(currentMap) : new Map(prev);
        const parentNode = base.get(parentId);
        if (!parentNode) return prev;
        const parentDepth = parentNode.depth;
        const childIds: string[] = [];
        for (const child of result.children) {
          const cid = child.id;
          childIds.push(cid);
          if (!base.has(cid)) {
            base.set(cid, {
              content: child,
              children: [],
              depth: parentDepth + 1,
              loaded: false,
            });
          }
        }
        base.set(parentId, { ...parentNode, children: childIds, loaded: true });
        return base;
      });
    } catch (error) {
      if (isNotFoundError(error)) {
        toast.error("컬렉션을 불러오지 못했습니다. 링크가 만료되었을 수 있습니다.");
        setSourceInfo(null);
        setTreeData(new Map());
        setRootId(null);
        setExpanded(new Set());
        setSelected(new Set());
        setStep(1);
        return;
      }
      toast.error("폴더 목록을 불러오지 못했습니다", { description: toErrorMessage(error) });
    } finally {
      setLoadingIds((prev) => {
        const n = new Set(prev);
        n.delete(parentId);
        return n;
      });
    }
  };

  const handleExpand = useCallback(
    (id: string) => {
      setExpanded((prev) => new Set(prev).add(id));
      const node = treeData.get(id);
      if (!node || node.loaded || !sourceInfo) return;
      if (sourceInfo.source === "link") {
        void loadLinkChildren(id, sourceInfo.linkId, sourceInfo.token);
      } else {
        void loadModChildren(id, sourceInfo.token, sourceInfo.sig);
      }
    },
    [treeData, sourceInfo],
  );

  const handleCollapse = useCallback((id: string) => {
    setExpanded((prev) => {
      const n = new Set(prev);
      n.delete(id);
      return n;
    });
  }, []);

  const getAncestors = useCallback(
    (id: string) => {
      const ancestors: string[] = [];
      let cur = treeData.get(id);
      while (cur && cur.content.parentId) {
        const pid = cur.content.parentId;
        ancestors.push(pid);
        cur = treeData.get(pid);
      }
      // for mod virtual root, also need to handle virtual parent
      return ancestors;
    },
    [treeData],
  );

  const handleToggle = useCallback(
    (id: string) => {
      const node = treeData.get(id);
      if (!node || !node.content.isDir) return;
      setSelected((prev) => {
        const next = new Set(prev);
        if (next.has(id)) {
          next.delete(id);
          return next;
        }
        // if ancestor already selected, don't add (redundant)
        const ancestors = getAncestors(id);
        for (const anc of ancestors) {
          if (next.has(anc)) return next;
        }
        // remove descendants
        const desc = descendantMap.get(id);
        if (desc) for (const d of desc) next.delete(d);
        next.add(id);
        return next;
      });
    },
    [treeData, descendantMap, getAncestors],
  );

  const selectedCount = selected.size;
  const canImport = selectedCount > 0 && !isImporting && !resolving;

  const importMutation = useMutation({
    mutationKey: ["drive", "copy-from-url-many"],
    mutationFn: async () => {
      if (!session) throw new Error("DRIVE_AUTH_REQUIRED");
      if (!sourceInfo) throw new Error("SOURCE_NOT_RESOLVED");
      const operationId = crypto.randomUUID();
      copyOperationIdRef.current = operationId;
      isPendingRef.current = true;
      setIsImporting(true);
      setCopyProgress({
        operationId,
        source: sourceInfo.source,
        phase: "preparing",
        current: 0,
        total: selected.size,
      });

      // prune selectedIds: remove descendants where ancestor already selected (already pruned on toggle, but double-check)
      const pruned = new Set(selected);
      for (const id of [...selected]) {
        const ancestors = getAncestors(id);
        for (const anc of ancestors) if (selected.has(anc)) pruned.delete(id);
      }
      const selectedIds = [...pruned];
      if (selectedIds.length === 0) throw new Error("NO_SELECTION");

      const result = await window.api.invoke("drive:fn:copyFromUrl", {
        url: url.trim(),
        password: password || undefined,
        destinationId,
        operationId,
        selectedIds,
      });
      return result;
    },
  });

  const handleImport = async () => {
    if (!session) {
      if (sessionInitialized) {
        toast.warning(t("page.drive.import.login_required"));
        await startLogin();
      }
      return;
    }
    if (selected.size === 0) {
      toast.warning("가져올 폴더를 선택하세요.");
      return;
    }
    const destinationIdSnapshot = destinationId;
    // 전송 시작 시 overlay를 즉시 닫는다. 실제 복사는 transfer로 승격되어 백그라운드에서 계속되며,
    // 설정 general.moveTransferPageWhenStartTransfer 가 켜져 있으면 main의 transfer.createTransfer
    // 에서 fn:navi 로 /transfer 이동을 트리거한다 (use-global-events.ts:67).
    setImportOverlay(null);

    void importMutation
      .mutateAsync()
      .then(async (result) => {
        isPendingRef.current = false;
        setIsImporting(false);
        await queryClient.invalidateQueries({
          queryKey: ["drive", "drive", destinationIdSnapshot],
          exact: true,
        });
        toast.success(t("page.drive.import.success", { count: result.copied }));
      })
      .catch((error: unknown) => {
        isPendingRef.current = false;
        setIsImporting(false);
        const code = getErrorCode(error);
        const message = toErrorMessage(error);
        if (code === "DRIVE_LINK_PASSWORD_REQUIRED" || code === "DRIVE_MOD_PASSWORD_REQUIRED") {
          toast.warning(t("page.drive.import.password_required"));
          return;
        }
        if (code === "DRIVE_LINK_INVALID_PASSWORD" || code === "DRIVE_MOD_INVALID_PASSWORD") {
          toast.error(t("page.drive.import.invalid_password"));
          return;
        }
        if (isNotFoundError(error)) {
          toast.error("링크가 만료되었거나 존재하지 않습니다.");
          return;
        }
        if (message.includes("DRIVE_COPY_CANCELED") || message.includes("canceled")) return;
        if (message.includes("NO_SELECTION")) {
          toast.warning("선택된 폴더가 없습니다.");
          return;
        }
        toast.error(t("page.drive.import.failed"), { description: message });
      });
  };

  const handleBack = () => {
    if (isImporting) return;
    setSourceInfo(null);
    setTreeData(new Map());
    setRootId(null);
    setExpanded(new Set());
    setSelected(new Set());
    setCopyProgress(null);
    setStep(1);
  };

  const handleCancel = async () => {
    const operationId = copyOperationIdRef.current;
    if (operationId && isPendingRef.current) {
      try {
        await window.api.invoke("drive:fn:cancelCopyFromUrl", operationId);
      } catch {}
    }
    isPendingRef.current = false;
    setIsImporting(false);
    setImportOverlay(null);
  };

  if (!importOverlay) return null;

  const isUrlStep = step === 1;
  const isTreeStep = step === 3;

  return (
    <div
      className="absolute inset-0 z-50 flex items-center justify-center bg-black/20 p-4"
      onClick={(e) => e.stopPropagation()}
    >
      <div
        className={cn(
          "flex max-h-[85vh] w-full flex-col overflow-hidden rounded-lg bg-background/80 shadow-lg outline backdrop-blur-lg",
          isTreeStep ? "max-w-5xl" : "max-w-sm",
        )}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b p-2 px-4">
          <h3 className="text-base font-semibold">{t("page.drive.import.title")}</h3>
          <Button variant="ghost" size="icon" className="size-7 shrink-0" onClick={handleCancel}>
            <XIcon className="size-4" />
          </Button>
        </div>

        {isUrlStep ? (
          <div className="flex flex-1 flex-col gap-4 p-4">
            <div className="space-y-1">
              <Label htmlFor="drive-import-url" className="flex items-center gap-1.5">
                <LinkIcon className="size-3" />
                {t("page.drive.import.url_label")}
              </Label>
              <div className="flex gap-2">
                <Input
                  ref={urlInputRef}
                  id="drive-import-url"
                  type="url"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder={t("page.drive.import.url_placeholder")}
                  className="flex-1"
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && !resolving) void handleResolve();
                  }}
                />
              </div>
              <p className="text-xs text-muted-foreground">
                https://nahida.live/akasha/link/... 또는 /akasha/mod/... 형식
              </p>
            </div>

            {requiresPassword && (
              <div className="space-y-1">
                <Label htmlFor="drive-import-password" className="flex items-center gap-1.5">
                  <LockIcon className="size-3" />
                  {t("page.drive.import.password_label")}
                </Label>
                <InputGroup>
                  <InputGroupInput
                    ref={passwordInputRef}
                    id="drive-import-password"
                    type="password"
                    value={password}
                    aria-invalid={passwordInvalid || undefined}
                    onChange={(e) => {
                      setPassword(e.target.value);
                      setPasswordInvalid(false);
                    }}
                    placeholder={t("page.drive.import.password_placeholder")}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && !resolving) void handleResolve();
                    }}
                  />
                  <InputGroupAddon align="inline-end">
                    <InputGroupButton
                      size="icon-xs"
                      disabled={!password.trim() || resolving}
                      aria-label={t("page.drive.import.authenticate")}
                      onClick={() => void handleResolve()}
                    >
                      {resolving ? (
                        <Loader2Icon className="size-3.5 animate-spin" />
                      ) : (
                        <CheckIcon className="size-3.5" />
                      )}
                    </InputGroupButton>
                  </InputGroupAddon>
                </InputGroup>
                {passwordInvalid && (
                  <p className="text-xs text-destructive">
                    {t("page.drive.import.invalid_password")}
                  </p>
                )}
                <p className="text-xs text-muted-foreground">비밀번호가 필요한 링크입니다.</p>
              </div>
            )}

            <div className="space-y-1">
              <p className="text-sm font-medium">{t("page.drive.import.destination_label")}</p>
              <div className="flex items-center gap-2">
                <FolderIcon className="size-4 shrink-0 text-yellow-400" />
                <Input
                  value={
                    destinationQuery.data?.content?.name ?? t("page.drive.import.loading_folder")
                  }
                  className="w-full"
                  readOnly
                />
              </div>
            </div>

            {resolving && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2Icon className="size-4 animate-spin" />
                링크 정보를 불러오는 중...
              </div>
            )}
          </div>
        ) : isTreeStep ? (
          <div className="flex min-h-0 flex-1">
            <div className="flex w-[320px] shrink-0 flex-col border-r">
              <div className="border-b px-4 py-3">
                <h4 className="text-sm font-medium">가져오기 설정</h4>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  폴더만 선택 가능합니다. 파일은 비활성화됩니다.
                </p>
              </div>

              <div className="flex-1 space-y-4 p-4">
                <div className="space-y-1">
                  <Label className="text-xs">공유 URL</Label>
                  <Input value={url} readOnly className="text-xs" />
                </div>

                <div className="space-y-1">
                  <p className="text-xs font-medium">{t("page.drive.import.destination_label")}</p>
                  <div className="flex items-center gap-2">
                    <FolderIcon className="size-4 shrink-0 text-yellow-400" />
                    <span className="truncate text-sm">
                      {destinationQuery.data?.content?.name ??
                        t("page.drive.import.loading_folder")}
                    </span>
                  </div>
                </div>

                <div className="flex items-center justify-between text-xs">
                  <span className="text-muted-foreground">선택된 폴더</span>
                  <span className="font-medium tabular-nums">{selectedCount}개</span>
                </div>

                {isImporting && copyProgress && (
                  <div className="space-y-2 rounded-md border p-3">
                    <div className="flex items-center justify-between gap-3 text-sm">
                      <span className="truncate">
                        {copyProgress.itemName ??
                          t(`page.drive.import.progress.${copyProgress.phase}`)}
                      </span>
                      <span className="shrink-0 text-muted-foreground">
                        {copyProgress.current}/{copyProgress.total}
                      </span>
                    </div>
                    <Progress
                      value={
                        copyProgress.total > 0
                          ? (copyProgress.current / copyProgress.total) * 100
                          : null
                      }
                    />
                    <p className="text-xs text-muted-foreground">
                      {t(`page.drive.import.progress.${copyProgress.phase}`)}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      전송 탭에서도 진행률을 확인할 수 있습니다.
                    </p>
                  </div>
                )}
              </div>

              <div className="border-t p-3">
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    onClick={handleBack}
                    disabled={isImporting}
                    className="flex-1"
                  >
                    <ArrowLeftIcon className="size-4" />
                    이전
                  </Button>
                  <Button onClick={handleImport} disabled={!canImport} className="flex-1">
                    {isImporting ? (
                      <Loader2Icon className="mr-2 size-4 animate-spin" />
                    ) : (
                      <CloudDownloadIcon className="mr-2 size-4" />
                    )}
                    가져오기
                  </Button>
                </div>
              </div>
            </div>

            <div className="flex flex-1 flex-col">
              <div className="flex items-center justify-between border-b px-4 py-2.5">
                <span className="text-sm font-medium">폴더 선택</span>
                <span className="text-xs text-muted-foreground">{selectedCount}개 선택됨</span>
              </div>
              <ScrollArea className="flex-1">
                <div className="p-2">
                  {resolving ? (
                    <div className="flex items-center justify-center p-8 text-sm text-muted-foreground">
                      <Loader2Icon className="mr-2 size-4 animate-spin" />
                      폴더 목록을 불러오는 중...
                    </div>
                  ) : (
                    <ImportFolderTree
                      visibleNodes={visibleNodes}
                      expanded={expanded}
                      selected={selected}
                      loadingIds={loadingIds}
                      descendantMap={descendantMap}
                      onToggle={handleToggle}
                      onExpand={handleExpand}
                      onCollapse={handleCollapse}
                    />
                  )}
                </div>
              </ScrollArea>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}
