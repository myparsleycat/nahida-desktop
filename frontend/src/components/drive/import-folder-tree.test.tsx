// @vitest-environment jsdom

import { fireEvent, render, waitFor } from "@testing-library/react";
import { useCallback, useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { ImportFolderTree, type TreeNode } from "./import-folder-tree";

class ResizeObserverMock {
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
}
vi.stubGlobal("ResizeObserver", ResizeObserverMock);

const nodes: TreeNode[] = Array.from({ length: 2_000 }, (_, index) => ({
  id: `node-${index}`,
  name: `Node ${index}`,
  isDir: true,
  size: null,
  depth: 0,
}));

function Harness() {
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null);
  const handleViewport = useCallback((element: HTMLDivElement | null) => {
    if (element) {
      Object.defineProperties(element, {
        clientHeight: { configurable: true, value: 320 },
        clientWidth: { configurable: true, value: 640 },
        offsetHeight: { configurable: true, value: 320 },
        offsetWidth: { configurable: true, value: 640 },
      });
    }
    setViewport(element);
  }, []);
  return (
    <div ref={handleViewport} style={{ height: 320, overflow: "auto" }} data-testid="viewport">
      <ImportFolderTree
        visibleNodes={nodes}
        expanded={new Set()}
        selected={new Set(["node-500"])}
        selectedAncestorIds={new Set()}
        loadingIds={new Set()}
        scrollElement={viewport}
        onToggle={() => {}}
        onExpand={() => {}}
        onCollapse={() => {}}
      />
    </div>
  );
}

describe("ImportFolderTree virtualization", () => {
  it("keeps only viewport and overscan rows in the DOM and updates them after scrolling", async () => {
    const view = render(<Harness />);
    const viewport = view.getByTestId("viewport");

    await waitFor(() =>
      expect(view.container.querySelectorAll("[data-index]").length).toBeGreaterThan(0),
    );
    expect(view.container.querySelectorAll("[data-index]").length).toBeLessThan(40);
    expect(view.getByText("Node 0")).toBeTruthy();
    expect(view.queryByText("Node 500")).toBeNull();

    viewport.scrollTop = 500 * 32;
    fireEvent.scroll(viewport);

    await waitFor(() => expect(view.getByText("Node 500")).toBeTruthy());
    expect(view.container.querySelectorAll("[data-index]").length).toBeLessThan(40);
  });
});
