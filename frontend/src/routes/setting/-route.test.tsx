// @vitest-environment jsdom

import { Updater } from "@bindings/infra";
import { globalStore } from "@renderer/store/global";
import type { UpdaterStatus } from "@shared/updater";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { Suspense, type ComponentType } from "react";
import { afterEach, beforeAll, beforeEach, expect, it, vi } from "vitest";

import "./route";

const mocks = vi.hoisted(() => ({
  component: null as (ComponentType & { preload?: () => Promise<void> }) | null,
  toastPromise: vi.fn<(promise: Promise<UpdaterStatus>) => void>(),
}));

vi.mock("@bindings/infra", () => ({
  Updater: { CheckForUpdates: vi.fn(), GetStatus: vi.fn() },
}));
vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  createFileRoute: () => (options: { component: ComponentType }) => {
    mocks.component = options.component;
    return { options };
  },
  useLocation: () => ({ pathname: "/setting/gen" }),
  useNavigate: () => vi.fn(),
  Outlet: () => null,
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { promise: mocks.toastPromise } }));

const status: UpdaterStatus = {
  mode: "notify",
  updateAvailable: true,
  updateDownloaded: false,
  releaseVersion: "2.0.0",
  releaseNotes: null,
  shouldPromptForUpdate: false,
  isChecking: false,
  isDownloading: false,
};

beforeAll(async () => {
  await mocks.component?.preload?.();
}, 30_000);

beforeEach(() => {
  globalStore.setState(globalStore.getInitialState(), true);
  globalStore.getState().setUpdaterStatus(status);
  vi.mocked(Updater.GetStatus).mockResolvedValue(status);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  globalStore.setState(globalStore.getInitialState(), true);
});

async function clickCheck() {
  const Component = mocks.component;
  if (!Component) throw new Error("Setting route component is missing");
  await act(async () => {
    render(
      <Suspense>
        <Component />
      </Suspense>,
    );
  });
  fireEvent.click(await screen.findByRole("button", { name: "updater.actions.check" }));
  return { result: mocks.toastPromise.mock.calls[0][0] };
}

it.each(["success", "failure"])(
  "waits for a stale candidate refresh to settle with %s",
  async (outcome) => {
    const check = Promise.withResolvers<void>();
    vi.mocked(Updater.CheckForUpdates).mockReturnValue(check.promise);
    const { result } = await clickCheck();
    const settled = vi.fn();
    void result.then(settled, settled);

    await act(async () => {
      globalStore.getState().setUpdaterChecking(true);
      globalStore.getState().setUpdaterChecking(false);
    });
    expect(settled).not.toHaveBeenCalled();
    expect(Updater.GetStatus).not.toHaveBeenCalled();

    if (outcome === "failure") {
      const error = new Error("refresh failed");
      check.reject(error);
      await expect(result).rejects.toBe(error);
      expect(Updater.GetStatus).not.toHaveBeenCalled();
      return;
    }
    check.resolve();
    await expect(result).resolves.toEqual(status);
  },
);

it("reports a confirmed release without waiting for the automatic download", async () => {
  const check = Promise.withResolvers<void>();
  vi.mocked(Updater.CheckForUpdates).mockReturnValue(check.promise);
  const { result } = await clickCheck();
  await act(async () => {
    globalStore.getState().setUpdaterChecking(true);
    globalStore.getState().setUpdaterStatus({ ...status, mode: "auto", isDownloading: true });
  });
  await expect(result).resolves.toEqual(status);
  check.reject(new Error("download failed"));
  await act(async () => {});
});
