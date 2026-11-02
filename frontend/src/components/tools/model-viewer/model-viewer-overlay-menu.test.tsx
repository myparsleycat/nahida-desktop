// @vitest-environment jsdom
import type { ViewerOverlayMenu } from "@shared/mod-viewer/types";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { VariableStateValue } from "./model-viewer-dialog-types";

import {
  ModelViewerFloatingOverlayMenu,
  ModelViewerOverlayMenuPanel,
  type ModelViewerOverlayPlacement,
} from "./model-viewer-overlay-menu";

vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("./model-viewer-session", () => ({ modelViewerSourceToUrl: (path: string) => path }));

const rect = (x: number) => ({ x, y: 0, width: 10, height: 10, opacity: 1 });
const toPage = (page: number, x: number) => ({ ...rect(x), transitions: [], page });

const menu: ViewerOverlayMenu = {
  x: 0,
  y: 0,
  width: 100,
  height: 10,
  pages: [
    {
      quads: [{ ...rect(0), path: "first.png" }],
      buttons: [
        toPage(1, 90),
        {
          ...rect(0),
          variable: "hair",
          transitions: [
            { from: 0, to: 1 },
            { from: 1, to: 2 },
          ],
        },
      ],
    },
    {
      quads: [{ ...rect(50), path: "second.png", flipX: true }],
      buttons: [
        toPage(0, 90),
        { ...rect(50), variable: "chest", transitions: [{ from: 0, to: 1 }] },
      ],
    },
  ],
};

function Harness() {
  const [state, setState] = useState<Record<string, VariableStateValue>>({ hair: 0, chest: 0 });
  const [page, setPage] = useState(0);
  return (
    <>
      <output>{JSON.stringify(state)}</output>
      <ModelViewerOverlayMenuPanel
        menu={menu}
        variables={[
          { id: "hair", label: "Hair", defaultValue: 0, values: [], order: 0 },
          { id: "chest", label: "Chest", defaultValue: 0, values: [], order: 1 },
        ]}
        activeState={state}
        onSelect={(variable, value) => setState((current) => ({ ...current, [variable]: value }))}
        page={page}
        onPageChange={setPage}
      />
    </>
  );
}

afterEach(cleanup);

it("swaps the drawing and click areas with the page and keeps the toggle values", () => {
  const { container } = render(<Harness />);
  const images = () => [...container.querySelectorAll("img")].map((img) => img.getAttribute("src"));
  const switchPage = () =>
    fireEvent.click(
      screen.getByRole("button", { name: "page.tools.model_viewer.overlay_switch_page" }),
    );

  fireEvent.click(screen.getByRole("button", { name: "Hair" }));
  expect(screen.getByRole("status")).toHaveProperty("textContent", '{"hair":1,"chest":0}');

  switchPage();
  expect(images()).toEqual(["second.png"]);
  expect(screen.queryByRole("button", { name: "Hair" })).toBeNull();
  expect(container.querySelector("img")?.style.transform).toBe("scale(-1, 1)");
  fireEvent.click(screen.getByRole("button", { name: "Chest" }));
  expect(screen.getByRole("status")).toHaveProperty("textContent", '{"hair":1,"chest":1}');

  switchPage();
  expect(images()).toEqual(["first.png"]);
  expect(screen.queryByRole("button", { name: "Chest" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Hair" }));
  expect(screen.getByRole("status")).toHaveProperty("textContent", '{"hair":2,"chest":1}');
});

it("drags a slider into the variable of the current mode and moves its parts", () => {
  function SliderHarness() {
    const [state, setState] = useState<Record<string, VariableStateValue>>({
      play: 1,
      speed: 60,
      weight: 0,
    });
    return (
      <>
        <output>{JSON.stringify(state)}</output>
        <ModelViewerOverlayMenuPanel
          menu={sliderMenu}
          variables={[]}
          activeState={state}
          onSelect={(variable, value) => setState((current) => ({ ...current, [variable]: value }))}
          page={0}
          onPageChange={() => {}}
        />
      </>
    );
  }

  const { container } = render(<SliderHarness />);
  const panel = container.querySelector("img")!.parentElement!;
  panel.getBoundingClientRect = () => ({ left: 0, width: 100 }) as DOMRect;
  HTMLElement.prototype.setPointerCapture = () => {};
  const images = container.querySelectorAll("img");
  expect(images[2]!.style.left).toBe("45%");
  expect(images[1]!.style.width).toBe("45%");

  const handle = screen.getByRole("slider", { name: "Speed" });
  fireEvent.pointerDown(handle, { clientX: 50, pointerId: 1 });
  fireEvent.pointerMove(handle, { clientX: 95, pointerId: 1 });
  fireEvent.pointerUp(handle, { pointerId: 1 });
  fireEvent.pointerMove(handle, { clientX: 0, pointerId: 1 });
  expect(screen.getByRole("status")).toHaveProperty(
    "textContent",
    '{"play":1,"speed":110,"weight":0}',
  );
  expect(images[2]!.style.left).toBe("90%");

  fireEvent.click(screen.getByRole("button", { name: "Play" }));
  const weight = screen.getByRole("slider", { name: "Weight" });
  expect(images[2]!.style.left).toBe("0%");
  fireEvent.pointerDown(weight, { clientX: 5, pointerId: 1 });
  fireEvent.pointerMove(weight, { clientX: 50, pointerId: 1 });
  expect(screen.getByRole("status")).toHaveProperty(
    "textContent",
    '{"play":0,"speed":110,"weight":0.5}',
  );
});

const sliderMenu: ViewerOverlayMenu = {
  x: 0,
  y: 0,
  width: 100,
  height: 10,
  pages: [
    {
      quads: [
        { ...rect(0), width: 100, path: "panel.png" },
        { ...rect(0), width: 0, path: "bar.png" },
        { ...rect(0), path: "handle.png" },
      ],
      buttons: [{ ...rect(0), variable: "play", label: "Play", transitions: [{ from: 1, to: 0 }] }],
      sliders: [
        {
          handle: 2,
          parts: [
            { quad: 1, startX: 0, startWidth: 0, endX: 0, endWidth: 90 },
            { quad: 2, startX: 0, startWidth: 10, endX: 90, endWidth: 10 },
          ],
          modes: [
            {
              whenVariable: "play",
              whenValue: 1,
              variable: "speed",
              label: "Speed",
              min: 10,
              max: 110,
            },
            {
              whenVariable: "play",
              whenValue: 0,
              variable: "weight",
              label: "Weight",
              min: 0,
              max: 1,
            },
          ],
        },
      ],
    },
  ],
};

describe("in the viewer", () => {
  // A 960x540 viewer draws the menus at half their authored size.
  beforeEach(() => {
    HTMLElement.prototype.setPointerCapture = () => {};
    vi.stubGlobal(
      "ResizeObserver",
      class {
        constructor(private callback: ResizeObserverCallback) {}
        observe() {
          this.callback(
            [{ contentRect: { width: 960, height: 540 } } as ResizeObserverEntry],
            this as unknown as ResizeObserver,
          );
        }
        disconnect() {}
      },
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  function renderFloating(placed: ViewerOverlayMenu = { ...menu, x: 200, y: 100 }) {
    const onSelect = vi.fn();
    const onPlacementChange = vi.fn();
    function FloatingHarness() {
      const [placement, setPlacement] = useState<ModelViewerOverlayPlacement>();
      return (
        <ModelViewerFloatingOverlayMenu
          menu={placed}
          variables={[{ id: "hair", label: "Hair", defaultValue: 0, values: [], order: 0 }]}
          activeState={{ hair: 0, play: 1, speed: 60 }}
          onSelect={onSelect}
          page={0}
          onPageChange={() => {}}
          placement={placement}
          onPlacementChange={(next) => {
            onPlacementChange(next);
            setPlacement(next);
          }}
        />
      );
    }
    const { container } = render(<FloatingHarness />);
    const moved = container.querySelector("img")!.parentElement!.parentElement!;
    return { moved, onSelect, onPlacementChange };
  }

  it("opens at the scaled place and size the mod draws the menu", () => {
    const { moved } = renderFloating();
    expect(moved.style.left).toBe("100px");
    expect(moved.style.top).toBe("50px");
    expect(moved.style.width).toBe("50px");
  });

  it("moves the menu instead of clicking when a press on a button travels", () => {
    const { moved, onSelect, onPlacementChange } = renderFloating();
    const button = screen.getByRole("button", { name: "Hair" });

    fireEvent.pointerDown(button, { clientX: 100, clientY: 100, pointerId: 1 });
    fireEvent.pointerMove(button, { clientX: 130, clientY: 120, pointerId: 1, buttons: 1 });
    expect(moved.style.left).toBe("130px");
    expect(moved.style.top).toBe("70px");
    expect(onPlacementChange).not.toHaveBeenCalled();
    fireEvent.pointerUp(button, { clientX: 130, clientY: 120, pointerId: 1 });
    fireEvent.click(button);

    expect(onSelect).not.toHaveBeenCalled();
    expect(onPlacementChange).toHaveBeenCalledWith({ x: 130 / 960, y: 70 / 540, size: 1 });
    expect(moved.style.left).toBe("130px");

    fireEvent.pointerDown(button, { clientX: 130, clientY: 120, pointerId: 1 });
    fireEvent.pointerUp(button, { clientX: 130, clientY: 120, pointerId: 1 });
    fireEvent.click(button);
    expect(onSelect).toHaveBeenCalledWith("hair", 1);
  });

  it("keeps a press within the threshold a click and the menu inside the viewer", () => {
    const { moved, onSelect } = renderFloating();
    const button = screen.getByRole("button", { name: "Hair" });

    fireEvent.pointerDown(button, { clientX: 100, clientY: 100, pointerId: 1 });
    fireEvent.pointerMove(button, { clientX: 102, clientY: 101, pointerId: 1, buttons: 1 });
    fireEvent.pointerUp(button, { clientX: 102, clientY: 101, pointerId: 1 });
    fireEvent.click(button);
    expect(onSelect).toHaveBeenCalledWith("hair", 1);
    expect(moved.style.left).toBe("100px");

    fireEvent.pointerDown(button, { clientX: 100, clientY: 100, pointerId: 2 });
    fireEvent.pointerMove(button, { clientX: 5000, clientY: -5000, pointerId: 2, buttons: 1 });
    expect(moved.style.left).toBe("910px");
    expect(moved.style.top).toBe("0px");
  });

  it("restores the menu when the drag is cancelled", () => {
    const { moved, onPlacementChange } = renderFloating();
    const button = screen.getByRole("button", { name: "Hair" });

    fireEvent.pointerDown(button, { clientX: 100, clientY: 100, pointerId: 1 });
    fireEvent.pointerMove(button, { clientX: 130, clientY: 120, pointerId: 1, buttons: 1 });
    fireEvent.pointerCancel(button, { pointerId: 1 });
    expect(moved.style.left).toBe("100px");
    expect(onPlacementChange).not.toHaveBeenCalled();
  });

  it("forgets a press released outside the menu before the drag started", () => {
    const { moved, onPlacementChange } = renderFloating();
    const button = screen.getByRole("button", { name: "Hair" });

    fireEvent.pointerDown(button, { clientX: 100, clientY: 100, pointerId: 1 });
    fireEvent.pointerMove(button, { clientX: 300, clientY: 200, pointerId: 1, buttons: 0 });
    expect(moved.style.left).toBe("100px");
    fireEvent.pointerUp(button, { clientX: 300, clientY: 200, pointerId: 1 });
    expect(onPlacementChange).not.toHaveBeenCalled();
  });

  it("resizes on Ctrl + wheel around the cursor, stopping the page zoom", () => {
    const { moved, onPlacementChange } = renderFloating();
    const wheel = (init: WheelEventInit) => {
      const event = new WheelEvent("wheel", { bubbles: true, cancelable: true, ...init });
      moved.dispatchEvent(event);
      return event;
    };

    expect(wheel({ deltaY: -100 }).defaultPrevented).toBe(false);
    expect(onPlacementChange).not.toHaveBeenCalled();

    const ratio = Math.exp(0.1);
    act(() => {
      expect(
        wheel({ deltaY: -100, ctrlKey: true, clientX: 110, clientY: 52 }).defaultPrevented,
      ).toBe(true);
    });
    expect(Number.parseFloat(moved.style.width)).toBeCloseTo(50 * ratio);
    expect(Number.parseFloat(moved.style.left)).toBeCloseTo(110 - 10 * ratio);
    expect(Number.parseFloat(moved.style.top)).toBeCloseTo(52 - 2 * ratio);

    act(() => {
      wheel({ deltaY: 100000, ctrlKey: true, clientX: 110, clientY: 52 });
    });
    expect(moved.style.width).toBe("25px");
  });

  it("adds up wheel events arriving before the next render", () => {
    const { moved } = renderFloating();
    const wheel = () =>
      moved.dispatchEvent(
        new WheelEvent("wheel", {
          bubbles: true,
          cancelable: true,
          deltaY: -100,
          ctrlKey: true,
          clientX: 110,
          clientY: 52,
        }),
      );

    act(() => {
      wheel();
      wheel();
    });
    const ratio = Math.exp(0.2);
    expect(Number.parseFloat(moved.style.width)).toBeCloseTo(50 * ratio);
    expect(Number.parseFloat(moved.style.left)).toBeCloseTo(110 - 10 * ratio);
  });

  it("resizes from the corner without moving the menu or clicking under it", () => {
    const { moved, onSelect, onPlacementChange } = renderFloating();
    const corner = screen.getByRole("slider", {
      name: "page.tools.model_viewer.overlay_resize",
    });

    fireEvent.pointerDown(corner, { clientX: 150, clientY: 55, pointerId: 1 });
    fireEvent.pointerMove(corner, { clientX: 200, clientY: 60, pointerId: 1 });
    expect(moved.style.width).toBe("100px");
    expect(moved.style.left).toBe("100px");
    expect(moved.style.top).toBe("50px");
    fireEvent.pointerUp(corner, { clientX: 200, clientY: 60, pointerId: 1 });
    expect(onPlacementChange).toHaveBeenCalledWith({
      x: expect.closeTo(100 / 960),
      y: expect.closeTo(50 / 540),
      size: expect.closeTo(2),
    });
    expect(onSelect).not.toHaveBeenCalled();

    fireEvent.pointerDown(corner, { clientX: 200, clientY: 60, pointerId: 2 });
    fireEvent.pointerMove(corner, { clientX: 300, clientY: 70, pointerId: 2 });
    fireEvent.pointerCancel(corner, { pointerId: 2 });
    expect(moved.style.width).toBe("100px");

    fireEvent.pointerDown(corner, { clientX: 200, clientY: 60, pointerId: 3 });
    fireEvent.pointerMove(corner, { clientX: 5000, clientY: 60, pointerId: 3 });
    expect(moved.style.width).toBe("150px");
  });

  it("keeps a large menu inside the viewer at any size", () => {
    const { moved } = renderFloating({ ...menu, width: 1000, height: 1000, x: 900, y: 0 });
    const corner = screen.getByRole("slider", {
      name: "page.tools.model_viewer.overlay_resize",
    });
    expect(moved.style.left).toBe("450px");

    fireEvent.pointerDown(corner, { clientX: 0, clientY: 0, pointerId: 1 });
    fireEvent.pointerMove(corner, { clientX: 5000, clientY: 5000, pointerId: 1 });
    expect(moved.style.width).toBe("510px");
    expect(moved.style.left).toBe("450px");
  });

  it("leaves a slider drag to the slider", () => {
    const { moved, onSelect } = renderFloating({ ...sliderMenu, x: 200, y: 100 });
    const handle = screen.getByRole("slider", { name: "Speed" });
    handle.parentElement!.getBoundingClientRect = () => ({ left: 0, width: 50 }) as DOMRect;

    fireEvent.pointerDown(handle, { clientX: 25, clientY: 0, pointerId: 1 });
    fireEvent.pointerMove(handle, { clientX: 40, clientY: 0, pointerId: 1 });
    expect(moved.style.left).toBe("100px");
    expect(onSelect).toHaveBeenCalledWith("speed", expect.any(Number));
  });
});
