// @vitest-environment jsdom

import { viewStore } from "@renderer/store/drive";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const backend = vi.hoisted(() => ({
  GetItem: vi.fn(),
  ResolveImportSource: vi.fn(),
  ListLinkChildren: vi.fn(),
  ListModChildren: vi.fn(),
  CopyFromURL: vi.fn(),
  CancelCopyFromURL: vi.fn(),
}));
const messages = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), warning: vi.fn() }));

vi.mock("@bindings/drive", () => ({ Drive: backend }));
vi.mock("@renderer/hooks/use-auth", () => ({
  useAuth: () => ({ session: { id: "user" }, sessionInitialized: true, startLogin: vi.fn() }),
}));
vi.mock("@renderer/lib/settings", () => ({
  getSetting: vi
    .fn()
    .mockResolvedValue({ "drive.autoTryPasswords": false, "drive.passwordList": [] }),
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: messages }));
vi.mock("@wailsio/runtime", () => ({ Events: { On: () => () => {} } }));
vi.mock("@renderer/components/ui/scroll-area", () => ({
  ScrollArea: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock("./import-folder-tree", () => ({
  ImportFolderTree: ({
    visibleNodes,
    onToggle,
  }: {
    visibleNodes: { id: string }[];
    onToggle: (id: string) => void;
  }) => (
    <div>
      {visibleNodes.map((node) => (
        <button key={node.id} type="button" onClick={() => onToggle(node.id)}>
          select-{node.id}
        </button>
      ))}
    </div>
  ),
}));

import { DriveImportOverlay } from "./drive-import-overlay";

const url = "https://nahida.live/akasha/link/share";

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal("requestAnimationFrame", vi.fn());
  backend.GetItem.mockResolvedValue({ content: { name: "Destination" } });
  backend.ResolveImportSource.mockResolvedValue({
    source: "link",
    linkId: "share",
    token: "token",
    parent: { id: "root", name: "Root" },
  });
  backend.ListLinkChildren.mockResolvedValue({
    children: [{ id: "child", parentId: "root", name: "Child", isDir: true, size: null }],
  });
  act(() => viewStore.getState().setImportOverlay({ url: `  ${url}  ` }));
});

afterEach(() => {
  cleanup();
  viewStore.getState().setImportOverlay(null);
  vi.unstubAllGlobals();
});

async function openTree(client: QueryClient) {
  render(
    <QueryClientProvider client={client}>
      <DriveImportOverlay destinationId="destination" />
    </QueryClientProvider>,
  );
  fireEvent.keyDown(screen.getByLabelText("page.drive.import.url_label"), { key: "Enter" });
  await screen.findByRole("button", { name: "select-child" });
  fireEvent.click(screen.getByRole("button", { name: "select-child" }));
  fireEvent.click(screen.getByRole("button", { name: "select-root" }));
}

it("copies the pruned selection to the destination and refreshes that folder", async () => {
  const client = new QueryClient();
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const copy = Promise.withResolvers<{ copied: number }>();
  backend.CopyFromURL.mockReturnValue(copy.promise);
  await openTree(client);

  fireEvent.click(screen.getByRole("button", { name: "가져오기" }));
  await waitFor(() => expect(backend.CopyFromURL).toHaveBeenCalledTimes(1));
  expect(backend.CopyFromURL).toHaveBeenCalledWith({
    url,
    password: undefined,
    destinationId: "destination",
    createCollectionFolders: true,
    operationId: expect.any(String),
    selectedIds: ["root"],
  });
  expect(viewStore.getState().importOverlay).toBeNull();
  expect(invalidate).not.toHaveBeenCalled();

  await act(async () => {
    copy.resolve({ copied: 1 });
    await copy.promise;
  });
  await waitFor(() =>
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["drive", "drive", "destination"],
      exact: true,
    }),
  );
  expect(invalidate).toHaveBeenCalledTimes(1);
  expect(messages.success).toHaveBeenCalled();
});

it("reports copy failure without refreshing the destination", async () => {
  const client = new QueryClient();
  const invalidate = vi.spyOn(client, "invalidateQueries");
  backend.CopyFromURL.mockRejectedValue(new Error("copy failed"));
  await openTree(client);

  fireEvent.click(screen.getByRole("button", { name: "가져오기" }));
  await waitFor(() =>
    expect(messages.error).toHaveBeenCalledWith("page.drive.import.failed", {
      description: "copy failed",
    }),
  );
  expect(viewStore.getState().importOverlay).toBeNull();
  expect(invalidate).not.toHaveBeenCalled();
  expect(messages.success).not.toHaveBeenCalled();
});
