// @vitest-environment jsdom

import { Tools } from "@bindings/tools";
import { XXMI } from "@bindings/xxmi";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import FourThousandOneFixer from "./4001-fixer";

vi.mock("@bindings/tools", () => ({
  Tools: {
    FourThousandOneFixerUpdateReleases: vi.fn(),
    FourThousandOneFixerGetProviderReleases: vi.fn(),
    FourThousandOneFixerGetState: vi.fn(),
  },
}));
vi.mock("@bindings/xxmi", () => ({ XXMI: { GetXXMIData: vi.fn() } }));
vi.mock("@renderer/components/tools/4001-fixer-build-tools-path", () => ({
  FourThousandOneFixerBuildToolsPath: () => null,
}));
vi.mock("@renderer/lib/logger", () => ({ Logger: { capture: vi.fn() } }));
vi.mock("@wailsio/runtime", () => ({ Events: { On: () => vi.fn() } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

beforeEach(() => {
  vi.mocked(Tools.FourThousandOneFixerGetState).mockResolvedValue(null);
  vi.mocked(XXMI.GetXXMIData).mockResolvedValue(null);
  vi.mocked(Tools.FourThousandOneFixerGetProviderReleases).mockResolvedValue([]);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("4001 Fixer release loading", () => {
  it("loads the normal provider list on every mount without forcing a refresh", async () => {
    const view = render(<FourThousandOneFixer />);
    await screen.findByText("page.tools.4001_fixer.no_versions");
    view.unmount();
    render(<FourThousandOneFixer />);
    await screen.findByText("page.tools.4001_fixer.no_versions");

    expect(Tools.FourThousandOneFixerGetProviderReleases).toHaveBeenCalledTimes(2);
    expect(Tools.FourThousandOneFixerGetProviderReleases).toHaveBeenCalledWith("SpectrumQT");
    expect(Tools.FourThousandOneFixerUpdateReleases).not.toHaveBeenCalled();
  });

  it("keeps the loading state until the normal provider list resolves", async () => {
    let resolveReleases!: (releases: string[]) => void;
    vi.mocked(Tools.FourThousandOneFixerGetProviderReleases).mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveReleases = resolve;
        }),
    );
    render(<FourThousandOneFixer />);
    expect(screen.getByText("page.tools.4001_fixer.loading")).toBeTruthy();

    await act(async () => resolveReleases([]));
    expect(screen.getByText("page.tools.4001_fixer.no_versions")).toBeTruthy();
    expect(screen.queryByText("page.tools.4001_fixer.loading")).toBeNull();
  });

  it("shows the existing load error when the normal list fails", async () => {
    vi.mocked(Tools.FourThousandOneFixerGetProviderReleases).mockRejectedValue(
      new Error("GitHub rate limit"),
    );
    render(<FourThousandOneFixer />);

    await screen.findByText("page.tools.4001_fixer.load_failed");
    expect(Tools.FourThousandOneFixerGetProviderReleases).toHaveBeenCalledOnce();
    expect(Tools.FourThousandOneFixerUpdateReleases).not.toHaveBeenCalled();
  });
});
