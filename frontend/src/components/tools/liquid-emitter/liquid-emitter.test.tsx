// @vitest-environment jsdom

import type { Emitter, Look } from "@bindings/tools/liquid_emitter";
import type { ModelViewerWorkspace } from "@renderer/components/tools/model-viewer/model-viewer-workspace";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { afterEach, expect, it, vi } from "vitest";

import type { LiquidEmitterOverlay } from "./liquid-emitter-overlay";

import { LiquidEmitterTool } from "./liquid-emitter";

const session = vi.hoisted(() => vi.fn());

vi.mock("./use-liquid-emitter-session", () => ({
  IN_PLACE: "in_place",
  NEW_MOD: "new_mod",
  SLOT_COUNT_SETTING: "slotCount",
  useLiquidEmitterSession: session,
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("@renderer/components/tools/model-viewer/model-viewer-workspace", () => ({
  ALL_FORMS: "",
  ModelViewerWorkspace: (props: ComponentProps<typeof ModelViewerWorkspace>) => (
    <>
      {props.source?.transport.forms.map((form) => (
        <button key={form.id} onClick={() => props.onFormChange?.(form.id)}>
          {form.label}
        </button>
      ))}
      <button onClick={() => props.onFormChange?.("")}>All forms</button>
      <button onClick={() => props.onVisibleMeshesChange?.(new Set())}>Hide meshes</button>
      {props.overlay}
    </>
  ),
}));
vi.mock("./liquid-emitter-overlay", () => ({
  LiquidEmitterOverlay: (props: ComponentProps<typeof LiquidEmitterOverlay>) => (
    <div
      data-testid={props.muted ? "saved-marker" : "selected-marker"}
      data-mesh={props.surface?.meshIds[0]}
    />
  ),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it("keeps selected and saved overlays on shared meshes across child forms", () => {
  const look: Look = {
    force: 1,
    amount: 1,
    customColor: false,
    tint: [1, 1, 1],
    absorptionMm: 15,
    viscosity: 1,
    nozzle: "spray",
    scattering: 0,
    roughness: 0,
  };
  const emitters: Emitter[] = [1, 2].map((pid) => ({
    pid,
    iniPath: "bundle/mod.ini",
    section: "Body",
    hooked: true,
    look,
    anchor: {
      meshId: "shared-body",
      faceIndex: 0,
      faceVertices: [0, 1, 2],
      trianglePositions: [0, 0, 0, 1, 0, 0, 0, 1, 0],
      barycentric: [1, 0, 0],
      point: [0, 0, 0],
      offset: [0, 0, 0],
      direction: [0, 0, -1],
    },
  }));
  const mutation = { isPending: false, mutate: vi.fn() };
  session.mockReturnValue({
    inspection: {
      data: {
        modPath: "C:/mod",
        runtime: { state: "current" },
        meshUnit: 1,
        meshDown: [0, 0, -1],
        emitters,
        defaultLook: look,
      },
    },
    source: {
      transport: {
        meshes: [{ id: "shared-body", component: "Body", form: "bundle" }],
        forms: [
          { id: "bundle::mod.ini::Human", label: "Human" },
          { id: "bundle::mod.ini::Mermaid", label: "Mermaid" },
          { id: "other", label: "Other" },
        ],
      },
    },
    apply: mutation,
    remove: mutation,
    install: mutation,
    setLook: mutation,
    raiseSlotCount: mutation,
    restore: mutation,
  });
  render(
    <LiquidEmitterTool modPath="C:/mod" onModPathChange={vi.fn()} onUnsavedChange={vi.fn()} />,
  );

  for (const form of ["Human", "Mermaid", "All forms"]) {
    fireEvent.click(screen.getByRole("button", { name: form }));
    expect
      .soft(screen.queryByTestId("selected-marker")?.getAttribute("data-mesh"))
      .toBe("shared-body");
    expect
      .soft(screen.queryByTestId("saved-marker")?.getAttribute("data-mesh"))
      .toBe("shared-body");
  }

  fireEvent.click(screen.getByRole("button", { name: "Other" }));
  expect(screen.queryByTestId("selected-marker")).toBeNull();
  expect(screen.queryByTestId("saved-marker")).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: "Human" }));
  fireEvent.click(screen.getByRole("button", { name: "Hide meshes" }));
  expect(screen.queryByTestId("selected-marker")).toBeNull();
  expect(screen.queryByTestId("saved-marker")).toBeNull();
});
