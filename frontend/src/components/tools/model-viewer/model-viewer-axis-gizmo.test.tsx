// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { createRef } from "react";
import { PerspectiveCamera, Vector3 } from "three";
import { afterEach, expect, it, vi } from "vitest";

import { ModelViewerAxisGizmo, type ModelViewerAxisGizmoHandle } from "./model-viewer-axis-gizmo";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (_key: string, options: { axis: string }) => options.axis,
  }),
}));

afterEach(cleanup);

function renderGizmo(onSelect = vi.fn()) {
  const ref = createRef<ModelViewerAxisGizmoHandle>();
  render(<ModelViewerAxisGizmo ref={ref} onSelect={onSelect} />);
  return ref;
}

function headOffset(label: string) {
  const match = /translate\((.+)px, (.+)px\)/.exec(screen.getByLabelText(label).style.transform);
  return { x: Number(match?.[1]), y: Number(match?.[2]) };
}

function cameraAt(position: Vector3) {
  const camera = new PerspectiveCamera();
  camera.position.copy(position);
  camera.lookAt(0, 0, 0);
  return camera;
}

it("places the axes on screen for a camera looking from the top", () => {
  const ref = renderGizmo();
  ref.current?.update(cameraAt(new Vector3(0, 1, 0.001)).quaternion);

  expect(headOffset("+X").x).toBeGreaterThan(25);
  expect(Math.abs(headOffset("+Y").x)).toBeLessThan(1);
  expect(Math.abs(headOffset("+Y").y)).toBeLessThan(1);
  expect(headOffset("+Z").y).toBeGreaterThan(25);
  expect(Number(screen.getByLabelText("+Y").style.zIndex)).toBeGreaterThan(
    Number(screen.getByLabelText("-Y").style.zIndex),
  );
});

it("selects a view whose spherical angles put the camera on the clicked axis", () => {
  const onSelect = vi.fn();
  renderGizmo(onSelect);

  for (const [label, expected] of [
    ["+X", new Vector3(1, 0, 0)],
    ["-X", new Vector3(-1, 0, 0)],
    ["+Y", new Vector3(0, 1, 0)],
    ["-Y", new Vector3(0, -1, 0)],
    ["+Z", new Vector3(0, 0, 1)],
    ["-Z", new Vector3(0, 0, -1)],
  ] as const) {
    fireEvent.click(screen.getByLabelText(label));
    const view = onSelect.mock.lastCall?.[0];
    const position = new Vector3().setFromSphericalCoords(1, view.phi, view.theta);
    expect(position.distanceTo(expected)).toBeLessThan(0.01);
  }
});
