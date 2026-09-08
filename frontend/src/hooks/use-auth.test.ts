// @vitest-environment jsdom

import { globalStore } from "@renderer/store/global";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const auth = vi.hoisted(() => ({
    Probe: vi.fn().mockResolvedValue("online"),
    GetSession: vi.fn().mockResolvedValue(null),
    HasToken: vi.fn(),
    GetBackendStatus: vi.fn(),
    StartLogin: vi.fn(),
    StartLogout: vi.fn(),
}));

vi.mock("@bindings/auth", () => ({ Auth: auth }));

import { useAuth } from "./use-auth";

function resetAuthState() {
    globalStore.setState({
        session: null,
        sessionInitialized: true,
        hasToken: false,
        backendStatus: "offline",
    });
    auth.Probe.mockClear();
    auth.GetSession.mockClear();
}

beforeEach(resetAuthState);

afterEach(() => {
    cleanup();
    resetAuthState();
});

it("probes the backend when a tokenless session retries", async () => {
    const { result } = renderHook(() => useAuth());
    await act(async () => {
        await result.current.refreshSession();
    });
    expect(auth.Probe).toHaveBeenCalled();
    expect(auth.GetSession).toHaveBeenCalled();
    expect(globalStore.getState().backendStatus).toBe("online");
});

it("refreshes a saved session without a guest probe", async () => {
    globalStore.setState({ hasToken: true, backendStatus: "offline" });
    const { result } = renderHook(() => useAuth());
    await act(async () => {
        await result.current.refreshSession();
    });
    expect(auth.Probe).not.toHaveBeenCalled();
    expect(auth.GetSession).toHaveBeenCalled();
});
