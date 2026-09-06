import { Drive } from "@bindings/drive";
import { useAuth } from "@renderer/hooks/use-auth";
import { getSetting } from "@renderer/lib/settings";
import type {
    DriveImportContent,
    DriveListChildrenResult,
    DriveResolveImportSourceResult,
} from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import type { TreeNode } from "./import-folder-tree";

import { getErrorCode, isNotFoundError } from "./drive-import-errors";
import { collectSelectedAncestorIds, toggleSubtreeSelection } from "./import-folder-tree-state";

type WizardStep = 1 | 2 | 3;

type NodeData = {
    content: DriveImportContent;
    children: string[];
    depth: number;
    loaded: boolean;
};

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

export function useDriveImportSession(initialUrl: string) {
    const { t } = useTranslation();
    const { session, sessionInitialized, startLogin } = useAuth();
    const [url, setUrl] = useState(initialUrl);
    const [password, setPassword] = useState("");
    const [requiresPassword, setRequiresPassword] = useState(false);
    const [passwordInvalid, setPasswordInvalid] = useState(false);
    const [step, setStep] = useState<WizardStep>(1);
    const [resolving, setResolving] = useState(false);
    const [sourceInfo, setSourceInfo] = useState<DriveResolveImportSourceResult | null>(null);
    const [treeData, setTreeData] = useState<Map<string, NodeData>>(new Map());
    const [rootId, setRootId] = useState<string | null>(null);
    const [expanded, setExpanded] = useState<Set<string>>(new Set());
    const [selected, setSelected] = useState<Set<string>>(new Set());
    const [loadingIds, setLoadingIds] = useState<Set<string>>(new Set());

    const loadSeqRef = useRef(0);
    const lastResolvedUrlRef = useRef("");

    const getParentId = useCallback((id: string) => treeData.get(id)?.content.parentId, [treeData]);
    const selectedAncestorIds = useMemo(
        () => collectSelectedAncestorIds(selected, getParentId),
        [getParentId, selected],
    );

    const visibleNodes: TreeNode[] = useMemo(() => {
        if (!rootId) return [];
        const result: TreeNode[] = [];
        const virtualModRoot = rootId.startsWith("mod:virtual:");
        const stack: string[] = [];
        // if mod virtual root, start with its children, not itself
        if (virtualModRoot) {
            const rootNode = treeData.get(rootId);
            if (rootNode) {
                for (let i = rootNode.children.length - 1; i >= 0; i--)
                    stack.push(rootNode.children[i]);
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

    const pendingChildrenRef = useRef(new Set<string>());

    const resetSession = useCallback(() => {
        loadSeqRef.current++;
        lastResolvedUrlRef.current = "";
        pendingChildrenRef.current.clear();
        setSourceInfo(null);
        setTreeData(new Map());
        setRootId(null);
        setExpanded(new Set());
        setSelected(new Set());
        setLoadingIds(new Set());
        setStep(1);
        setRequiresPassword(false);
        setPasswordInvalid(false);
        setResolving(false);
    }, []);

    const loadChildren = useCallback(
        async (parentId: string, source: DriveResolveImportSourceResult, seq: number) => {
            if (seq !== loadSeqRef.current || pendingChildrenRef.current.has(parentId)) return;
            pendingChildrenRef.current.add(parentId);
            setLoadingIds((prev) => new Set(prev).add(parentId));
            try {
                const result = (await (source.source === "link"
                    ? Drive.ListLinkChildren({
                          linkId: source.linkId,
                          linkToken: source.token,
                          itemId: parentId,
                      })
                    : Drive.ListModChildren({
                          itemId: parentId,
                          modToken: source.token,
                          modSig: source.sig,
                      }))) as unknown as DriveListChildrenResult;
                if (seq !== loadSeqRef.current) return;
                setTreeData((prev) => {
                    if (seq !== loadSeqRef.current) return prev;
                    const parentNode = prev.get(parentId);
                    if (!parentNode) return prev;
                    // Merge each response into the latest tree; sibling requests complete independently.
                    const next = new Map(prev);
                    for (const child of result.children) {
                        if (!next.has(child.id)) {
                            next.set(child.id, {
                                content: child,
                                children: [],
                                depth: parentNode.depth + 1,
                                loaded: false,
                            });
                        }
                    }
                    next.set(parentId, {
                        ...parentNode,
                        children: result.children.map((child) => child.id),
                        loaded: true,
                    });
                    return next;
                });
            } catch (error) {
                if (seq !== loadSeqRef.current) return;
                if (isNotFoundError(error)) {
                    toast.error(
                        source.source === "link"
                            ? "링크가 만료되었습니다. 처음부터 다시 시도하세요."
                            : "컬렉션을 불러오지 못했습니다. 링크가 만료되었을 수 있습니다.",
                    );
                    resetSession();
                    return;
                }
                toast.error("폴더 목록을 불러오지 못했습니다", {
                    description: toErrorMessage(error),
                });
            } finally {
                if (seq === loadSeqRef.current) {
                    pendingChildrenRef.current.delete(parentId);
                    setLoadingIds((prev) => {
                        const next = new Set(prev);
                        next.delete(parentId);
                        return next;
                    });
                }
            }
        },
        [resetSession],
    );

    const applyResolvedResult = useCallback(
        (resolved: DriveResolveImportSourceResult, seq: number, trimmedUrl: string) => {
            if (seq !== loadSeqRef.current) return;
            lastResolvedUrlRef.current = trimmedUrl;
            setSourceInfo(resolved);
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
                void loadChildren(rid, resolved, seq);
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
                newMap.set(virtualId, {
                    content: virtualContent,
                    children: [],
                    depth: 0,
                    loaded: true,
                });
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
                    collectionNodes.push({
                        content: colContent,
                        children: [],
                        depth: 1,
                        loaded: false,
                    });
                    newMap.get(virtualId)!.children.push(col.rootId);
                    newMap.set(col.rootId, {
                        content: colContent,
                        children: [],
                        depth: 1,
                        loaded: false,
                    });
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
                    void loadChildren(n.content.id, resolved, seq);
                }
            }
            setRequiresPassword(false);
            setPasswordInvalid(false);
        },
        [loadChildren],
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
                        void Drive.ResolveImportSource({
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
        [applyResolvedResult],
    );

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
            if (seqArg === undefined) resetSession();
            const seq = seqArg ?? loadSeqRef.current;
            if (seq !== loadSeqRef.current) return;
            setResolving(true);
            setRequiresPassword(false);
            setPasswordInvalid(false);
            try {
                const resolved = (await Drive.ResolveImportSource({
                    url: trimmed,
                    password: password || undefined,
                })) as unknown as DriveResolveImportSourceResult;
                applyResolvedResult(resolved, seq, trimmed);
            } catch (error) {
                if (seq !== loadSeqRef.current) return;
                const code = getErrorCode(error);
                const msg = toErrorMessage(error);
                if (
                    code === "DRIVE_LINK_PASSWORD_REQUIRED" ||
                    code === "DRIVE_MOD_PASSWORD_REQUIRED"
                ) {
                    const autoTried = await tryAutoPasswords(trimmed, seq);
                    if (autoTried || seq !== loadSeqRef.current) return;
                    setRequiresPassword(true);
                    toast.warning(t("page.drive.import.password_required"));
                    setStep(1);
                    return;
                }
                if (
                    code === "DRIVE_LINK_INVALID_PASSWORD" ||
                    code === "DRIVE_MOD_INVALID_PASSWORD"
                ) {
                    setRequiresPassword(true);
                    setPasswordInvalid(true);
                    toast.error(t("page.drive.import.invalid_password"));
                    setStep(1);
                    return;
                }
                if (
                    code === "DRIVE_INVALID_SOURCE_URL" ||
                    msg.includes("DRIVE_INVALID_SOURCE_URL")
                ) {
                    toast.error("유효하지 않은 링크입니다.", {
                        description: "나히다 공유 링크 또는 컬렉션 URL을 입력하세요.",
                    });
                    return;
                }
                if (isNotFoundError(error)) {
                    toast.error("링크가 만료되었거나 존재하지 않습니다.", { description: msg });
                    resetSession();
                    setPassword("");
                    return;
                }
                if (msg.includes("DRIVE_COPY_CANCELED")) return;
                toast.error(t("page.drive.import.failed"), { description: msg });
            } finally {
                if (seq === loadSeqRef.current) setResolving(false);
            }
        },
        [
            url,
            password,
            session,
            sessionInitialized,
            startLogin,
            t,
            applyResolvedResult,
            tryAutoPasswords,
            resetSession,
        ],
    );
    const handleResolveRef = useRef(handleResolve);
    useLayoutEffect(() => {
        handleResolveRef.current = handleResolve;
    });

    const isLoggedIn = Boolean(session);

    // Invalidate source requests and child loads together, including on unmount.
    useLayoutEffect(() => {
        const trimmed = url.trim();
        if (trimmed === lastResolvedUrlRef.current && isLoggedIn) return;
        // Synchronize the source request lifecycle.
        // oxlint-disable-next-line react/set-state-in-effect
        resetSession();
        const seq = loadSeqRef.current;
        const timer =
            isValidDriveUrl(trimmed) && isLoggedIn
                ? setTimeout(() => {
                      if (seq === loadSeqRef.current) void handleResolveRef.current(seq);
                  }, 500)
                : undefined;
        return () => {
            clearTimeout(timer);
            loadSeqRef.current++;
            pendingChildrenRef.current.clear();
        };
    }, [url, isLoggedIn, resetSession]);

    const handleExpand = useCallback(
        (id: string) => {
            setExpanded((prev) => new Set(prev).add(id));
            const node = treeData.get(id);
            if (!node || node.loaded || !sourceInfo) return;
            void loadChildren(id, sourceInfo, loadSeqRef.current);
        },
        [treeData, sourceInfo, loadChildren],
    );

    const handleCollapse = useCallback((id: string) => {
        setExpanded((prev) => {
            const n = new Set(prev);
            n.delete(id);
            return n;
        });
    }, []);

    const handleToggle = useCallback(
        (id: string) => {
            const node = treeData.get(id);
            if (!node || !node.content.isDir) return;
            setSelected((previous) => toggleSubtreeSelection(previous, id, getParentId));
        },
        [getParentId, treeData],
    );

    return {
        url,
        setUrl,
        password,
        setPassword,
        requiresPassword,
        passwordInvalid,
        setPasswordInvalid,
        step,
        resolving,
        sourceInfo,
        selected,
        expanded,
        loadingIds,
        getParentId,
        selectedAncestorIds,
        visibleNodes,
        handleResolve,
        handleExpand,
        handleCollapse,
        handleToggle,
        resetSession,
    };
}
