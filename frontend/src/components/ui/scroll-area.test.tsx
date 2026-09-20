// @vitest-environment jsdom

import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ScrollArea } from "./scroll-area";

class ResizeObserverMock {
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
}
vi.stubGlobal("ResizeObserver", ResizeObserverMock);

// jsdom has no animations, and Base UI probes them on a timer after the viewport mounts.
Element.prototype.getAnimations = () => [];

describe("ScrollArea", () => {
  // Base UI gives the content wrapper an inline `min-width: fit-content`, which grows the scroll
  // content past the viewport when a child cannot shrink. The Agent route pins it back to the
  // viewport width through this slot, so the slot has to exist in the DOM.
  it("exposes the content slot callers use to pin the intrinsic width", () => {
    const { container } = render(
      <ScrollArea>
        <div>content</div>
      </ScrollArea>,
    );

    expect(container.querySelector('[data-slot="scroll-area-content"]')).toBeTruthy();
    expect(container.querySelector('[data-slot="scroll-area-viewport"]')).toBeTruthy();
  });
});
