// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useDriveImportSession } from "./use-drive-import-session";

const mocks = vi.hoisted(() => ({
    resolve: vi.fn(),
    linkChildren: vi.fn(),
    modChildren: vi.fn(),
    settings: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    auth: {
        session: { id: "user" } as { id: string } | null,
        sessionInitialized: true,
        startLogin: vi.fn(),
    },
    t: (key: string) => key,
}));
vi.mock("@bindings/drive", () => ({
    Drive: {
        ResolveImportSource: mocks.resolve,
        ListLinkChildren: mocks.linkChildren,
        ListModChildren: mocks.modChildren,
    },
}));
vi.mock("@renderer/hooks/use-auth", () => ({ useAuth: () => mocks.auth }));
vi.mock("@renderer/lib/settings", () => ({ getSetting: mocks.settings }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: mocks.t }) }));
vi.mock("sonner", () => ({ toast: { error: mocks.error, warning: mocks.warning } }));

function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (error: Error) => void;
    const promise = new Promise<T>((accept, fail) => {
        resolve = accept;
        reject = fail;
    });
    return { promise, resolve, reject };
}
function children(parent: string, id: string) {
    return { children: [{ id, parentId: parent, name: id, isDir: true, size: null }] };
}
function mod(id: string, roots: string[]) {
    return {
        source: "mod",
        modId: id,
        token: id,
        sig: id,
        modData: {
            title: id,
            collections: roots.map((rootId) => ({
                id: rootId,
                rootId,
                name: rootId,
                private: false,
            })),
        },
    };
}
function link(id: string) {
    return { source: "link", linkId: id, token: id, parent: { id, name: id } };
}
const url = (id: string) => `https://nahida.live/akasha/mod/${id}`;

beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
    mocks.auth.session = { id: "user" };
    mocks.settings.mockResolvedValue({ "drive.autoTryPasswords": false, "drive.passwordList": [] });
});
afterEach(() => {
    cleanup();
    vi.useRealTimers();
});

describe("drive import session", () => {
    it.each([false, true])(
        "merges parallel collections in either completion order (reverse=%s)",
        async (reverse) => {
            const a = deferred<ReturnType<typeof children>>();
            const b = deferred<ReturnType<typeof children>>();
            mocks.resolve.mockResolvedValue(mod("source", ["A", "B"]));
            mocks.modChildren.mockImplementation(({ itemId }) =>
                itemId === "A" ? a.promise : b.promise,
            );
            const { result } = renderHook(() => useDriveImportSession(url("source")));
            await act(() => result.current.handleResolve());
            const responses = reverse
                ? ([
                      [b, "B"],
                      [a, "A"],
                  ] as const)
                : ([
                      [a, "A"],
                      [b, "B"],
                  ] as const);
            for (const [request, parent] of responses) {
                await act(async () => {
                    request.resolve(children(parent, `${parent}-child`));
                });
            }
            expect(result.current.visibleNodes.map((node) => node.id)).toEqual([
                "A",
                "A-child",
                "B",
                "B-child",
            ]);
            expect(result.current.loadingIds.size).toBe(0);
            act(() => result.current.handleExpand("A"));
            expect(mocks.modChildren).toHaveBeenCalledTimes(2);
        },
    );

    it.each(["success", "error"])(
        "ignores an old child %s after changing sources with the same root ID",
        async (outcome) => {
            const old = deferred<ReturnType<typeof children>>();
            const current = deferred<ReturnType<typeof children>>();
            mocks.resolve
                .mockResolvedValueOnce(link("root"))
                .mockResolvedValueOnce(mod("new", ["root"]));
            mocks.linkChildren.mockReturnValue(old.promise);
            mocks.modChildren.mockReturnValue(current.promise);
            const { result } = renderHook(() => useDriveImportSession(url("old")));
            await act(() => result.current.handleResolve());
            act(() => result.current.setUrl(url("new")));
            await act(() => result.current.handleResolve());
            await act(async () => {
                if (outcome === "success") old.resolve(children("root", "old-child"));
                else old.reject(new Error("DRIVE_NOT_FOUND"));
            });
            expect(result.current.loadingIds.has("root")).toBe(true);
            expect(result.current.sourceInfo?.source).toBe("mod");
            expect(mocks.error).not.toHaveBeenCalled();
            await act(async () => {
                current.resolve(children("root", "new-child"));
            });
            expect(result.current.visibleNodes.map((node) => node.id)).toEqual([
                "root",
                "new-child",
            ]);
        },
    );

    it("ignores superseded resolve errors and preserves the newer loading state", async () => {
        const old = deferred<ReturnType<typeof link>>();
        const current = deferred<ReturnType<typeof mod>>();
        mocks.resolve.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise);
        const { result } = renderHook(() => useDriveImportSession(url("old")));
        let first!: Promise<void>;
        let second!: Promise<void>;
        act(() => {
            first = result.current.handleResolve();
        });
        act(() => result.current.setUrl(url("new")));
        act(() => {
            second = result.current.handleResolve();
        });
        await act(async () => {
            old.reject(new Error("DRIVE_LINK_INVALID_PASSWORD"));
            await first;
        });
        expect(result.current.resolving).toBe(true);
        expect(result.current.requiresPassword).toBe(false);
        expect(mocks.error).not.toHaveBeenCalled();
        await act(async () => {
            current.resolve(mod("new", []));
            await second;
        });
        expect(result.current.resolving).toBe(false);
        expect(result.current.sourceInfo).toMatchObject({ modId: "new" });
    });

    it("deduplicates folder expansion and invalidates pending work when going back", async () => {
        const pending = deferred<ReturnType<typeof children>>();
        mocks.resolve.mockResolvedValue(link("root"));
        mocks.linkChildren.mockReturnValue(pending.promise);
        const { result } = renderHook(() => useDriveImportSession(url("source")));
        await act(() => result.current.handleResolve());
        act(() => {
            result.current.handleExpand("root");
            result.current.handleExpand("root");
        });
        expect(mocks.linkChildren).toHaveBeenCalledTimes(1);
        act(() => result.current.resetSession());
        await act(async () => {
            pending.reject(new Error("expired"));
        });
        expect(result.current.step).toBe(1);
        expect(result.current.visibleNodes).toEqual([]);
        expect(result.current.loadingIds.size).toBe(0);
        expect(mocks.error).not.toHaveBeenCalled();
    });

    it("does not notify or auto-resolve after unmount", async () => {
        const pending = deferred<ReturnType<typeof link>>();
        mocks.resolve.mockReturnValue(pending.promise);
        const { result, unmount } = renderHook(() => useDriveImportSession(url("source")));
        let request!: Promise<void>;
        act(() => {
            request = result.current.handleResolve();
        });
        unmount();
        await act(async () => {
            pending.reject(new Error("expired"));
            await request;
            await vi.runAllTimersAsync();
        });
        expect(mocks.error).not.toHaveBeenCalled();
        expect(mocks.resolve).toHaveBeenCalledTimes(1);
    });

    it("automatically resolves a valid URL after the debounce and retains subtree selection", async () => {
        mocks.resolve.mockResolvedValue(link("root"));
        mocks.linkChildren.mockResolvedValue(children("root", "child"));
        const { result } = renderHook(() => useDriveImportSession(url("source")));
        await act(async () => {
            await vi.advanceTimersByTimeAsync(499);
        });
        expect(mocks.resolve).not.toHaveBeenCalled();
        await act(async () => {
            await vi.advanceTimersByTimeAsync(1);
        });
        expect(result.current.visibleNodes.map((node) => node.id)).toEqual(["root", "child"]);
        act(() => result.current.handleToggle("child"));
        expect(result.current.selectedAncestorIds.has("root")).toBe(true);
        act(() => result.current.handleToggle("root"));
        expect([...result.current.selected]).toEqual(["root"]);
    });

    it("keeps a resolved tree when the session object is replaced", async () => {
        mocks.resolve.mockResolvedValue(link("root"));
        mocks.linkChildren.mockResolvedValue(children("root", "child"));
        const { result, rerender } = renderHook(() => useDriveImportSession(url("source")));
        await act(async () => {
            await vi.advanceTimersByTimeAsync(500);
        });
        act(() => result.current.handleToggle("child"));
        const resolveCount = mocks.resolve.mock.calls.length;
        mocks.auth.session = { id: "user" };
        rerender();
        await act(async () => {
            await vi.advanceTimersByTimeAsync(500);
        });
        expect(result.current.visibleNodes.map((node) => node.id)).toEqual(["root", "child"]);
        expect([...result.current.selected]).toEqual(["child"]);
        expect(mocks.resolve).toHaveBeenCalledTimes(resolveCount);
    });

    it("auto-resolves a valid URL after login", async () => {
        mocks.auth.session = null;
        mocks.resolve.mockResolvedValue(link("root"));
        mocks.linkChildren.mockResolvedValue(children("root", "child"));
        const { result, rerender } = renderHook(() => useDriveImportSession(url("source")));
        await act(async () => {
            await vi.advanceTimersByTimeAsync(500);
        });
        expect(mocks.resolve).not.toHaveBeenCalled();
        mocks.auth.session = { id: "user" };
        rerender();
        await act(async () => {
            await vi.advanceTimersByTimeAsync(500);
        });
        expect(result.current.visibleNodes.map((node) => node.id)).toEqual(["root", "child"]);
    });

    it("clears a resolved tree after logout", async () => {
        mocks.resolve.mockResolvedValue(link("root"));
        mocks.linkChildren.mockResolvedValue(children("root", "child"));
        const { result, rerender } = renderHook(() => useDriveImportSession(url("source")));
        await act(async () => {
            await vi.advanceTimersByTimeAsync(500);
        });
        act(() => result.current.handleToggle("child"));
        mocks.auth.session = null;
        rerender();
        expect(result.current.visibleNodes).toEqual([]);
        expect(result.current.step).toBe(1);
        expect(result.current.selected.size).toBe(0);
        expect(mocks.resolve).toHaveBeenCalledTimes(1);
    });

    it("keeps the password prompt mounted while a manual password is submitted", async () => {
        const pending = deferred<ReturnType<typeof mod>>();
        mocks.resolve
            .mockRejectedValueOnce(new Error("DRIVE_LINK_PASSWORD_REQUIRED"))
            .mockReturnValueOnce(pending.promise);
        const { result } = renderHook(() => useDriveImportSession(url("source")));
        await act(() => result.current.handleResolve());
        expect(result.current.requiresPassword).toBe(true);
        act(() => result.current.setPassword("secret"));
        let request!: Promise<void>;
        act(() => {
            request = result.current.handleResolve();
        });
        expect(result.current.requiresPassword).toBe(true);
        expect(result.current.resolving).toBe(true);
        await act(async () => {
            pending.resolve(mod("source", []));
            await request;
        });
        expect(result.current.password).toBe("secret");
        expect(result.current.step).toBe(3);
        expect(result.current.requiresPassword).toBe(false);
        expect(mocks.resolve).toHaveBeenLastCalledWith({
            url: url("source"),
            password: "secret",
        });
    });

    it.each([false, true])(
        "stores the winning auto password when a later candidate succeeds (reverse=%s)",
        async (reverse) => {
            mocks.settings.mockResolvedValue({
                "drive.autoTryPasswords": true,
                "drive.passwordList": ["alpha", "bravo"],
            });
            const alpha = deferred<ReturnType<typeof mod>>();
            const bravo = deferred<ReturnType<typeof mod>>();
            mocks.resolve.mockImplementation((input: { password?: string }) => {
                if (!input.password)
                    return Promise.reject(new Error("DRIVE_LINK_PASSWORD_REQUIRED"));
                return input.password === "alpha" ? alpha.promise : bravo.promise;
            });
            const { result } = renderHook(() => useDriveImportSession(url("source")));
            let request!: Promise<void>;
            act(() => {
                request = result.current.handleResolve();
            });
            await act(async () => {
                await Promise.resolve();
                await Promise.resolve();
            });
            expect(mocks.resolve).toHaveBeenCalledTimes(3);
            const [loser, winner, winnerPassword] = reverse
                ? ([bravo, alpha, "alpha"] as const)
                : ([alpha, bravo, "bravo"] as const);
            await act(async () => {
                loser.reject(new Error("DRIVE_LINK_INVALID_PASSWORD"));
            });
            expect(result.current.sourceInfo).toBeNull();
            expect(result.current.step).toBe(1);
            await act(async () => {
                winner.resolve(mod("source", []));
                await request;
            });
            expect(result.current.password).toBe(winnerPassword);
            expect(result.current.step).toBe(3);
            expect(result.current.requiresPassword).toBe(false);
            expect(result.current.sourceInfo).toMatchObject({ modId: "source" });
        },
    );

    it.each([false, true])(
        "sets requiresPassword when every auto password is rejected (reverse=%s)",
        async (reverse) => {
            mocks.settings.mockResolvedValue({
                "drive.autoTryPasswords": true,
                "drive.passwordList": ["alpha", "bravo"],
            });
            const alpha = deferred<ReturnType<typeof mod>>();
            const bravo = deferred<ReturnType<typeof mod>>();
            mocks.resolve.mockImplementation((input: { password?: string }) => {
                if (!input.password)
                    return Promise.reject(new Error("DRIVE_LINK_PASSWORD_REQUIRED"));
                return input.password === "alpha" ? alpha.promise : bravo.promise;
            });
            const { result } = renderHook(() => useDriveImportSession(url("source")));
            let request!: Promise<void>;
            act(() => {
                request = result.current.handleResolve();
            });
            await act(async () => {
                await Promise.resolve();
                await Promise.resolve();
            });
            expect(mocks.resolve).toHaveBeenCalledTimes(3);
            const [first, second] = reverse ? ([bravo, alpha] as const) : ([alpha, bravo] as const);
            await act(async () => {
                first.reject(new Error("DRIVE_LINK_INVALID_PASSWORD"));
            });
            expect(result.current.requiresPassword).toBe(false);
            expect(result.current.sourceInfo).toBeNull();
            await act(async () => {
                second.reject(new Error("DRIVE_LINK_INVALID_PASSWORD"));
                await request;
            });
            expect(result.current.requiresPassword).toBe(true);
            expect(result.current.sourceInfo).toBeNull();
            expect(result.current.step).toBe(1);
            expect(result.current.password).toBe("");
        },
    );
});
