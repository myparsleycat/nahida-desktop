// @vitest-environment jsdom
import type { ViewerOverlayMenu } from "@shared/mod-viewer/types";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";

import type { VariableStateValue } from "./model-viewer-dialog-types";

import { ModelViewerOverlayMenuPanel } from "./model-viewer-overlay-menu";

vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("./model-viewer-session", () => ({ modelViewerSourceToUrl: (path: string) => path }));

const rect = (x: number) => ({ x, y: 0, width: 10, height: 10, opacity: 1 });
const toPage = (page: number, x: number) => ({ ...rect(x), transitions: [], page });

const menu: ViewerOverlayMenu = {
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
  const sliderMenu: ViewerOverlayMenu = {
    width: 100,
    height: 10,
    pages: [
      {
        quads: [
          { ...rect(0), width: 100, path: "panel.png" },
          { ...rect(0), width: 0, path: "bar.png" },
          { ...rect(0), path: "handle.png" },
        ],
        buttons: [
          { ...rect(0), variable: "play", label: "Play", transitions: [{ from: 1, to: 0 }] },
        ],
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
