// @vitest-environment jsdom

import { TooltipProvider } from "@renderer/components/ui/tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { TruncatedText } from "./truncated-text";

// jsdom performs no layout, so the widths that decide truncation are supplied per test.
function renderText(textWidth: number, boxWidth: number) {
  Range.prototype.getBoundingClientRect = () => new DOMRect(0, 0, textWidth, 20);
  render(
    <TooltipProvider delay={0}>
      <TruncatedText text="Very Long Mod Name" />
    </TooltipProvider>,
  );

  const trigger = screen.getByText("Very Long Mod Name");
  vi.spyOn(trigger, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, boxWidth, 20));
  return trigger;
}

function hover(element: HTMLElement) {
  fireEvent.pointerMove(element, { pointerType: "mouse" });
  fireEvent.mouseEnter(element);
}

function findTooltip() {
  return screen.findByText("Very Long Mod Name", { selector: "[data-slot='tooltip-content'] p" });
}

afterEach(cleanup);

describe("TruncatedText", () => {
  it("shows the full text in a tooltip when the text is truncated", async () => {
    hover(renderText(300, 100));

    expect(await findTooltip()).toBeTruthy();
  });

  it("shows the tooltip when the text overflows by less than a pixel", async () => {
    hover(renderText(100.4, 100));

    expect(await findTooltip()).toBeTruthy();
  });

  it("shows no tooltip when the text fits", async () => {
    hover(renderText(100, 100));
    await new Promise((resolve) => setTimeout(resolve, 100));

    expect(document.querySelector("[data-slot='tooltip-content']")).toBeNull();
  });

  it("does not render a button, which mod rows treat as a click target of its own", () => {
    expect(renderText(300, 100).tagName).toBe("SPAN");
  });
});
