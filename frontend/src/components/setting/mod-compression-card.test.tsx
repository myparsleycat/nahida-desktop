// @vitest-environment jsdom

import { Mod, type CompressionState } from "@bindings/mod";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

let currentState: CompressionState;

vi.mock("@bindings/mod", () => ({
  Mod: {
    DecompressExternalCompression: vi.fn(),
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

import { Logger } from "@renderer/lib/logger";
import { toast } from "sonner";

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
    failedFiles: 0,
    externalFiles: 0,
    canToggle: true,
    canConfigure: true,
    canDecompressExternal: false,
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

  it("locks controls and shows target state while a transition is running", () => {
    currentState = state({
      status: "compressing",
      targetEnabled: true,
      canToggle: false,
      canConfigure: false,
    });
    render(<ModCompressionCard />);
    const toggle = screen.getByRole("switch");
    expect(toggle.getAttribute("aria-checked")).toBe("true");
    expect(toggle.hasAttribute("data-disabled")).toBe(true);
    expect(screen.getByRole("combobox").hasAttribute("disabled")).toBe(true);
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

  it("shows external decompression only for a retryable XPRESS4K block", () => {
    currentState = state({
      status: "blocked",
      externalFiles: 2,
      canToggle: false,
      canDecompressExternal: true,
    });
    const { rerender } = render(<ModCompressionCard />);
    expect(
      screen.getByRole("button", { name: "page.setting.mod.compression.externalDecompress" }),
    ).toBeTruthy();

    currentState = state({
      method: "zstd",
      status: "blocked",
      externalFiles: 2,
      canToggle: false,
      canDecompressExternal: false,
    });
    rerender(<ModCompressionCard />);
    expect(
      screen.queryByRole("button", { name: "page.setting.mod.compression.externalDecompress" }),
    ).toBeNull();
    expect(screen.getByText("page.setting.mod.compression.externalManual")).toBeTruthy();
  });

  it("locks immediately and suppresses duplicate external decompression requests", () => {
    currentState = state({
      status: "blocked",
      externalFiles: 2,
      canToggle: false,
      canDecompressExternal: true,
    });
    vi.mocked(Mod.DecompressExternalCompression).mockReturnValue(new Promise(() => {}));
    render(<ModCompressionCard />);

    const button = screen.getByRole("button", {
      name: "page.setting.mod.compression.externalDecompress",
    });
    fireEvent.click(button);
    fireEvent.click(button);

    expect(Mod.DecompressExternalCompression).toHaveBeenCalledTimes(1);
    expect(button.hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("combobox").hasAttribute("disabled")).toBe(true);
  });

  it("logs and reports an external decompression request failure", async () => {
    currentState = state({
      status: "blocked",
      externalFiles: 1,
      canToggle: false,
      canDecompressExternal: true,
    });
    const error = new Error("locked");
    vi.mocked(Mod.DecompressExternalCompression).mockRejectedValue(error);
    render(<ModCompressionCard />);

    fireEvent.click(
      screen.getByRole("button", {
        name: "page.setting.mod.compression.externalDecompress",
      }),
    );

    await waitFor(() => {
      expect(Logger.error).toHaveBeenCalledWith(error, "ModCompressionCard:decompressExternal");
      expect(toast.error).toHaveBeenCalledWith(
        "page.setting.mod.compression.externalDecompressRequestFailed",
      );
    });
  });

  it("keeps the retry action visible after a partial decompression failure", () => {
    currentState = state({
      status: "blocked",
      externalFiles: 1,
      failedFiles: 1,
      error: "EXTERNAL_DECOMPRESSION_FAILED",
      canToggle: false,
      canDecompressExternal: true,
    });
    render(<ModCompressionCard />);

    expect(screen.getByText("page.setting.mod.compression.externalDecompressFailed")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "page.setting.mod.compression.externalDecompress" }),
    ).toBeTruthy();
  });
});
