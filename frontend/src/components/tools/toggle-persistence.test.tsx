// @vitest-environment jsdom

import en from "@renderer/lib/i18n/locales/en.json";
import ja from "@renderer/lib/i18n/locales/ja.json";
import ko from "@renderer/lib/i18n/locales/ko.json";
import zh from "@renderer/lib/i18n/locales/zh.json";
import type { NamespaceIsolationState } from "@shared/types";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const backend = vi.hoisted(() => ({
  getState: vi.fn(),
  rescan: vi.fn(),
  getEnabled: vi.fn(),
  setEnabled: vi.fn(),
  getXXMI: vi.fn(),
  getLogs: vi.fn(),
  on: vi.fn(),
  error: vi.fn(),
  translate: vi.fn(),
}));

vi.mock("@bindings/mod", () => ({
  Mod: {
    GetNamespaceIsolationState: backend.getState,
    RescanNamespaceIsolation: backend.rescan,
  },
}));
vi.mock("@bindings/setting", () => ({
  Setting: { GetPersistToggles: backend.getEnabled, SetPersistToggles: backend.setEnabled },
}));
vi.mock("@bindings/xxmi", () => ({ XXMI: { GetXXMIData: backend.getXXMI } }));
vi.mock("@bindings/tools", () => ({ Tools: { GetPersistLogs: backend.getLogs } }));
vi.mock("@wailsio/runtime", () => ({ Events: { On: backend.on } }));
vi.mock("sonner", () => ({ toast: { error: backend.error } }));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: backend.translate }),
}));

import TogglePersistence from "./toggle-persistence";

const prefix = "page.setting.xxmi.namespaceIsolation.";
const stateKey = ["mod:getNamespaceIsolationState"];
const listeners = new Map<string, (event: { data: unknown }) => void>();
const unsubscribe = vi.fn();
let queryClient: QueryClient;

function state(revision = 1, status = "needs_review") {
  return {
    revision,
    checking: false,
    conflicts: [
      {
        id: "collision-1",
        importerKey: "GIMI",
        namespace: "shared_namespace",
        modPaths: ["C:\\Mods\\Original", "C:\\Mods\\Copy"],
        iniPaths: ["C:\\Mods\\Original\\mod.ini", "C:\\Mods\\Copy\\mod.ini"],
        status,
        reason: "ambiguous_reference",
        detail: "Review the shared reference before retrying.",
      },
    ],
  } satisfies NamespaceIsolationState;
}

function renderScreen() {
  return render(
    <QueryClientProvider client={queryClient}>
      <TogglePersistence />
    </QueryClientProvider>,
  );
}

function emit(channel: string, data: unknown) {
  act(() => listeners.get(channel)?.({ data }));
}

beforeEach(() => {
  vi.resetAllMocks();
  backend.translate.mockImplementation(
    (key: string, options?: { importer?: string; defaultValue?: string }) =>
      options?.importer ? `${key} ${options.importer}` : (options?.defaultValue ?? key),
  );
  listeners.clear();
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  backend.getState.mockResolvedValue(state());
  backend.rescan.mockResolvedValue(state(2));
  backend.getEnabled.mockResolvedValue(true);
  backend.setEnabled.mockResolvedValue(undefined);
  backend.getXXMI.mockResolvedValue({
    xxmiPath: "C:\\XXMI",
    enabledImporters: [{ key: "GIMI" }, { key: "WWMI" }, { key: "NTE" }],
  });
  backend.getLogs.mockResolvedValue(["First log", "Latest log"]);
  backend.on.mockImplementation((channel: string, listener: (event: { data: unknown }) => void) => {
    listeners.set(channel, listener);
    return unsubscribe;
  });
});

afterEach(() => {
  cleanup();
  queryClient.clear();
});

describe("TogglePersistence namespace isolation", () => {
  it("shows collisions, reason, detail, and all paths alongside the existing toggle and logs", async () => {
    renderScreen();
    await screen.findByText("shared_namespace");
    expect(screen.getByText("ambiguous_reference", { exact: false })).toBeTruthy();
    expect(screen.getByText(state().conflicts[0].detail)).toBeTruthy();
    for (const path of [...state().conflicts[0].modPaths, ...state().conflicts[0].iniPaths]) {
      expect(screen.getByText(path)).toBeTruthy();
    }
    expect(screen.getAllByRole("switch")).toHaveLength(1);
    expect(screen.getByText("Latest log")).toBeTruthy();
    emit("setting:xxmi:persistLogs", ["Updated log"]);
    await screen.findByText("Updated log");
    fireEvent.click(screen.getByRole("switch"));
    await waitFor(() => expect(backend.setEnabled).toHaveBeenCalledWith(false));
  });

  it.each(["waiting_for_game_exit", "needs_review", "failed", "recovery_required"])(
    "uses a locale key for %s",
    async (status) => {
      backend.getState.mockResolvedValue(state(1, status));
      renderScreen();
      await screen.findByText(status);
      expect(backend.translate).toHaveBeenCalledWith(prefix + "status." + status, {
        defaultValue: status,
      });
    },
  );

  it("keeps newer events when the initial query resolves late and ignores older or equal events", async () => {
    let resolveQuery!: (value: NamespaceIsolationState) => void;
    backend.getState.mockReturnValue(
      new Promise<NamespaceIsolationState>((resolve) => {
        resolveQuery = resolve;
      }),
    );
    renderScreen();
    emit("mod:namespace-isolation-state", state(5, "recovery_required"));
    await screen.findByText("recovery_required");
    await act(async () => resolveQuery(state(1)));
    emit("mod:namespace-isolation-state", state(4, "failed"));
    emit("mod:namespace-isolation-state", state(5, "failed"));
    await waitFor(() =>
      expect(queryClient.getQueryData(stateKey)).toEqual(state(5, "recovery_required")),
    );
    emit("mod:namespace-isolation-state", state(6, "waiting_for_game_exit"));
    await screen.findByText("waiting_for_game_exit");
  });

  it("offers one rescan per importer even without collisions and excludes NTE", async () => {
    backend.getState.mockResolvedValue({ revision: 1, checking: false, conflicts: [] });
    renderScreen();
    await screen.findByText(prefix + "empty");
    const button = screen.getByRole("button", { name: prefix + "rescanImporter WWMI" });
    expect(screen.getAllByRole("button")).toHaveLength(2);
    fireEvent.click(button);
    await waitFor(() => expect(backend.rescan).toHaveBeenCalledWith("WWMI"));
    await screen.findByText("shared_namespace");
  });

  it("does not replace a newer event with a late rescan response", async () => {
    let resolveRescan!: (value: NamespaceIsolationState) => void;
    backend.rescan.mockReturnValue(
      new Promise<NamespaceIsolationState>((resolve) => {
        resolveRescan = resolve;
      }),
    );
    renderScreen();
    await screen.findByText("shared_namespace");
    fireEvent.click(screen.getByRole("button", { name: prefix + "rescanImporter GIMI" }));
    await waitFor(() => expect(backend.rescan).toHaveBeenCalledTimes(1));
    expect(
      screen.getByRole("button", { name: prefix + "rescanImporter WWMI" }).hasAttribute("disabled"),
    ).toBe(true);
    emit("mod:namespace-isolation-state", state(9, "failed"));
    await act(async () => resolveRescan(state(2)));
    await screen.findByText("failed");
    expect(queryClient.getQueryData(stateKey)).toEqual(state(9, "failed"));
  });

  it("allows recovery rescans with persistence off and keeps the setting-off explanation", async () => {
    backend.getEnabled.mockResolvedValue(false);
    backend.getState.mockResolvedValue(state(1, "recovery_required"));
    backend.rescan.mockResolvedValue({ revision: 2, checking: false, conflicts: [] });
    renderScreen();
    await screen.findByText("recovery_required");
    await screen.findByText(prefix + "disabled");
    const button = screen.getByRole("button", { name: prefix + "rescanImporter GIMI" });
    expect(button.hasAttribute("disabled")).toBe(false);
    fireEvent.click(button);
    await waitFor(() => expect(backend.rescan).toHaveBeenCalledWith("GIMI"));
    await screen.findByText(prefix + "empty");
    expect(screen.getByText(prefix + "disabled")).toBeTruthy();
    expect(backend.setEnabled).not.toHaveBeenCalled();
  });

  it("disables rescan while checking even with persistence off", async () => {
    backend.getEnabled.mockResolvedValue(false);
    backend.getState.mockResolvedValue({ ...state(), checking: true });
    renderScreen();
    await screen.findByText("shared_namespace");
    await screen.findByText(prefix + "checking");
    expect(
      screen.getByRole("button", { name: prefix + "rescanImporter GIMI" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(backend.rescan).not.toHaveBeenCalled();
  });

  it("shows query and rescan errors without losing the existing logs", async () => {
    backend.getState.mockRejectedValue(new Error("status unavailable"));
    backend.rescan.mockRejectedValue(new Error("rescan unavailable"));
    renderScreen();
    await screen.findByText(prefix + "queryFailed");
    expect(screen.getByText("status unavailable")).toBeTruthy();
    expect(screen.getByText("Latest log")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: prefix + "rescanImporter GIMI" }));
    await waitFor(() =>
      expect(backend.error).toHaveBeenCalledWith(prefix + "rescanFailed", {
        description: "rescan unavailable",
      }),
    );
  });

  it("unsubscribes both event handlers on unmount", async () => {
    const view = renderScreen();
    await screen.findByText("shared_namespace");
    view.unmount();
    expect(unsubscribe).toHaveBeenCalledTimes(2);
  });

  it.each([en, ko, ja, zh])("provides the complete namespace section in each locale", (locale) => {
    const strings = locale.page.setting.xxmi.namespaceIsolation;
    expect(Object.keys(strings)).toEqual(Object.keys(en.page.setting.xxmi.namespaceIsolation));
    expect(Object.keys(strings.status)).toEqual([
      "waiting_for_game_exit",
      "needs_review",
      "failed",
      "recovery_required",
    ]);
    for (const value of Object.values(strings.status)) {
      expect(value.length).toBeGreaterThan(0);
    }
    expect(strings.rescanImporter).toContain("{{importer}}");
  });
});
