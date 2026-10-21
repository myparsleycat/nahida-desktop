// @vitest-environment jsdom

import type { Look } from "@bindings/tools/liquid_emitter";
import { act, cleanup, fireEvent, render, renderHook, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { LookSection, useLookDraft } from "./liquid-emitter-look";

vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

afterEach(cleanup);

const saved: Look = {
  force: 1.4,
  amount: 0.6,
  customColor: true,
  tint: [0.2, 0.4, 1],
  absorptionMm: 30,
  viscosity: 4,
  nozzle: "ooze",
  scattering: 0,
  roughness: 0,
};

function renderLook(look: Look) {
  const onDraftChange = vi.fn<(look: Look | null) => void>();
  render(<LookSection saved={look} draft={null} onDraftChange={onDraftChange} locked={false} />);
  // The section remembers being opened for the rest of the run.
  if (!screen.queryByText("page.tools.liquid_emitter.look.reset")) {
    fireEvent.click(screen.getByText("page.tools.liquid_emitter.look.title"));
  }
  return { onDraftChange };
}

describe("liquid look material", () => {
  it("sets only scattering and roughness from a preset", () => {
    const { onDraftChange } = renderLook(saved);
    fireEvent.click(screen.getByText("page.tools.liquid_emitter.look.material.preset.cloudy"));

    expect(onDraftChange).toHaveBeenCalledWith({ ...saved, scattering: 0.85, roughness: 0.35 });
  });

  it("marks a combination no preset sets as custom", () => {
    renderLook({ ...saved, scattering: 0.5, roughness: 0.2 });

    expect(screen.getByText("page.tools.liquid_emitter.look.material.preset.custom")).toBeTruthy();
    for (const preset of ["clear", "translucent", "cloudy"]) {
      const button = screen.getByText(`page.tools.liquid_emitter.look.material.preset.${preset}`);
      expect(button.closest("button")?.getAttribute("aria-pressed")).toBe("false");
    }
  });

  it("resets the material with the rest of the look", () => {
    const { onDraftChange } = renderLook({ ...saved, scattering: 0.85, roughness: 0.35 });
    fireEvent.click(screen.getByText("page.tools.liquid_emitter.look.reset"));

    expect(onDraftChange).toHaveBeenCalledWith({
      ...saved,
      force: 1,
      amount: 1,
      customColor: false,
      viscosity: 1,
      nozzle: "spray",
      scattering: 0,
      roughness: 0,
    });
  });
});

describe("liquid look draft", () => {
  it("drops an unsaved look once another emitter is selected", () => {
    const { result, rerender } = renderHook(({ emitter }) => useLookDraft(emitter), {
      initialProps: { emitter: "mod:1" },
    });
    act(() => result.current[1]({ ...saved, scattering: 0.35 }));
    expect(result.current[0]).toEqual({ ...saved, scattering: 0.35 });

    rerender({ emitter: "mod:2" });
    expect(result.current[0]).toBeNull();
    rerender({ emitter: "mod:1" });
    expect(result.current[0]).toBeNull();
  });
});
