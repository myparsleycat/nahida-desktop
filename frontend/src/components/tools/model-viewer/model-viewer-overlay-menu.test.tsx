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
