// @vitest-environment jsdom

import { Mod, type CompressionState } from "@bindings/mod";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

let currentState: CompressionState;

vi.mock("@bindings/mod", () => ({
  Mod: {
    SetCompressionConfig: vi.fn(),
    SetCompressionEnabled: vi.fn(),
  },
}));

vi.mock("@renderer/lib/logger", () => ({
  Logger: { error: vi.fn() },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn() },
}));

vi.mock("@renderer/hooks/use-mod-compression-state", () => ({
  useModCompressionState: () => [currentState, vi.fn()],
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

import { ModCompressionCard } from "./mod-compression-card";

function state(partial: Partial<CompressionState> = {}): CompressionState {
  return {
    enabled: false,
    method: "xpress4k",
    thresholdMiB: 1,
    status: "idle",
    processedFiles: 0,
    totalFiles: 0,
    processedBytes: 0,
    totalBytes: 0,
    canToggle: true,
    canConfigure: true,
    ...partial,
  };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("ModCompressionCard", () => {
  it("uses XPRESS4K state without showing a threshold input", () => {
    currentState = state();
    render(<ModCompressionCard />);
    expect(screen.getByText("XPRESS4K")).toBeTruthy();
    expect(screen.queryByRole("spinbutton")).toBeNull();
  });

  it("shows a bounded threshold only for Zstd", () => {
    currentState = state({ method: "zstd", thresholdMiB: 4 });
    render(<ModCompressionCard />);
    expect(
      screen.getByRole("combobox", { name: "page.setting.mod.compression.method" }),
    ).toBeTruthy();
    const input = screen.getByRole("spinbutton", {
      name: "page.setting.mod.compression.threshold",
    });
    expect(input.getAttribute("min")).toBe("1");
    expect(input.getAttribute("max")).toBe("64");
    expect(input.getAttribute("step")).toBe("1");
    expect(input.getAttribute("value")).toBe("4");
  });

  it.each([
    ["0", 1],
    ["65", 64],
  ])("clamps a blurred Zstd threshold of %s to %i", async (value, expected) => {
    currentState = state({ method: "zstd", thresholdMiB: 4 });
    render(<ModCompressionCard />);
    const input = screen.getByRole("spinbutton", {
      name: "page.setting.mod.compression.threshold",
    });

    fireEvent.change(input, { target: { value } });
    fireEvent.blur(input);

    await waitFor(() => {
      expect(Mod.SetCompressionConfig).toHaveBeenCalledWith({
        method: "zstd",
        thresholdMiB: expected,
      });
    });
  });

  it("allows an active compression to be switched off while configuration stays locked", () => {
    currentState = state({
      status: "compressing",
      targetEnabled: true,
      canToggle: true,
      canConfigure: false,
    });
    render(<ModCompressionCard />);
    const toggle = screen.getByRole("switch");
    expect(toggle.getAttribute("aria-checked")).toBe("true");
    expect(toggle.hasAttribute("data-disabled")).toBe(false);
    expect(screen.getByRole("combobox").hasAttribute("disabled")).toBe(true);
  });

  it("requests one cancellation and locks immediately when compression is switched off", () => {
    currentState = state({
      status: "compressing",
      targetEnabled: true,
      canToggle: true,
      canConfigure: false,
    });
    vi.mocked(Mod.SetCompressionEnabled).mockReturnValue(new Promise(() => {}));
    render(<ModCompressionCard />);

    const toggle = screen.getByRole("switch");
    fireEvent.click(toggle);
    fireEvent.click(toggle);

    expect(Mod.SetCompressionEnabled).toHaveBeenCalledTimes(1);
    expect(Mod.SetCompressionEnabled).toHaveBeenCalledWith(false);
    expect(toggle.hasAttribute("data-disabled")).toBe(true);
  });

  it("keeps the switch off and locked while decompression is running", () => {
    currentState = state({
      enabled: true,
      status: "decompressing",
      targetEnabled: false,
      canToggle: false,
      canConfigure: false,
    });
    render(<ModCompressionCard />);

    const toggle = screen.getByRole("switch");
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    expect(toggle.hasAttribute("data-disabled")).toBe(true);
  });

  it("locks immediately and suppresses duplicate toggle requests", () => {
    currentState = state();
    vi.mocked(Mod.SetCompressionEnabled).mockReturnValue(new Promise(() => {}));
    render(<ModCompressionCard />);

    const toggle = screen.getByRole("switch");
    fireEvent.click(toggle);
    fireEvent.click(toggle);

    expect(Mod.SetCompressionEnabled).toHaveBeenCalledTimes(1);
    expect(toggle.hasAttribute("data-disabled")).toBe(true);
    expect(screen.getByRole("combobox").hasAttribute("disabled")).toBe(true);
  });

  it("shows only the task-level error message", () => {
    currentState = state({ status: "error", error: "MOD_COMPRESSION_FAILED" });
    render(<ModCompressionCard />);

    expect(screen.getByText("page.setting.mod.compression.error")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
