// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ComponentType, ReactNode } from "react";
import { Suspense } from "react";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const backend = vi.hoisted(() => ({
  ApproveAction: vi.fn(),
  Cancel: vi.fn(),
  CreateSession: vi.fn(),
  DeleteSession: vi.fn(),
  GetSession: vi.fn(),
  ListSessions: vi.fn(),
  OpenSession: vi.fn(),
  RejectAction: vi.fn(),
  RenameSession: vi.fn(),
  RevertSession: vi.fn(),
  Send: vi.fn(),
  UnrevertSession: vi.fn(),
}));
const navigate = vi.hoisted(() => vi.fn());
const subscriptions = vi.hoisted(() => ({
  active: [] as ((event: { data: unknown }) => void)[],
  total: 0,
}));
// Stands in for the router's search state so navigating actually re-renders the route into the
// other conversation, the way the real router does.
const routeSearch = vi.hoisted(() => {
  const listeners = new Set<() => void>();
  let session: string | undefined = "session-1";
  return {
    current: () => session,
    set(next: string | undefined) {
      if (next === session) return;
      session = next;
      for (const listener of listeners) listener();
    },
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    reset() {
      session = "session-1";
    },
  };
});

vi.mock("@bindings/agent", () => ({ Service: backend }));
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const { useSyncExternalStore } = await import("react");
  return {
    ...(await importOriginal<typeof import("@tanstack/react-router")>()),
    createFileRoute: () => (options: { component: ComponentType }) => ({
      options,
      useSearch: () => ({
        session: useSyncExternalStore(routeSearch.subscribe, routeSearch.current),
      }),
      useNavigate: () => navigate,
    }),
    useNavigate: () => navigate,
  };
});
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { language: "en" } }),
}));
vi.mock("@renderer/components/ui/scroll-area", () => ({
  ScrollArea: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("@wailsio/runtime", () => ({
  Events: {
    On: (_name: string, handler: (event: { data: unknown }) => void) => {
      subscriptions.total += 1;
      subscriptions.active.push(handler);
      return () => {
        const index = subscriptions.active.indexOf(handler);
        if (index >= 0) subscriptions.active.splice(index, 1);
      };
    },
  },
}));

import { Route } from "./agent";

const SESSION_ID = "session-1";
const RUN_ID = "run-1";
const ROUTE_LOAD_TIMEOUT_MS = 5_000;

// jsdom has no layout engine, so the chat auto-scroll is a no-op here
Element.prototype.scrollIntoView = vi.fn();

function makeSnapshot(entries: unknown[] = [], overrides: Record<string, unknown> = {}) {
  return {
    summary: { id: SESSION_ID, title: "Nahida", running: false, scope: { type: "global" } },
    entries,
    approvals: [],
    roots: [],
    supportsImages: false,
    ...overrides,
  };
}

let sequence = 0;

function emit(type: string, payload?: Record<string, unknown>, sessionId = SESSION_ID) {
  sequence += 1;
  const update = { sessionId, runId: RUN_ID, sequence, type, payload };
  act(() => {
    for (const handler of [...subscriptions.active]) handler({ data: update });
  });
}

async function renderAgent(entries: unknown[] = [], getSession?: (id: string) => Promise<unknown>) {
  if (getSession) {
    backend.GetSession.mockImplementation(getSession);
  } else {
    backend.GetSession.mockResolvedValue(makeSnapshot(entries));
  }
  const Component = Route.options.component;
  if (!Component) throw new Error("Agent route has no component");
  await act(async () => {
    render(
      <Suspense fallback={null}>
        <Component />
      </Suspense>,
    );
  });
  await screen.findByText("Nahida", undefined, { timeout: ROUTE_LOAD_TIMEOUT_MS });
  // The route reads the session on screen when an event arrives, so an emit after the
  // conversation has loaded reaches it whatever the subscription lifecycle looks like.
  await waitFor(() => expect(subscriptions.total).toBeGreaterThan(0), {
    timeout: ROUTE_LOAD_TIMEOUT_MS,
  });
}

function reasoningPanel() {
  return screen.getByText("page.agent.reasoning").closest("details");
}

beforeEach(() => {
  vi.clearAllMocks();
  sequence = 0;
  subscriptions.active.length = 0;
  subscriptions.total = 0;
  routeSearch.reset();
  navigate.mockImplementation(async (options: { search?: { session?: string } }) => {
    routeSearch.set(options?.search?.session);
  });
  backend.ListSessions.mockResolvedValue([]);
});

afterEach(cleanup);

function makeSession(id: string, title: string) {
  return {
    id,
    title,
    running: false,
    updatedAt: new Date().toISOString(),
    scope: { type: "global" },
  };
}

describe("Agent session list", () => {
  it("exposes one menu trigger per session instead of inline actions", async () => {
    backend.ListSessions.mockResolvedValue([
      makeSession(SESSION_ID, "Current chat"),
      makeSession("session-2", "Second chat"),
    ]);
    await renderAgent();

    expect(screen.getAllByRole("button", { name: "page.agent.session_menu" })).toHaveLength(2);
    expect(screen.queryByRole("button", { name: "page.agent.rename_session" })).toBeNull();
    expect(screen.queryByRole("button", { name: "page.agent.delete_session" })).toBeNull();
  });

  it("renames a session from the row menu dialog", async () => {
    backend.RenameSession.mockResolvedValue(undefined);
    backend.ListSessions.mockResolvedValue([
      makeSession(SESSION_ID, "Current chat"),
      makeSession("session-2", "Second chat"),
    ]);
    await renderAgent();

    fireEvent.click(screen.getAllByRole("button", { name: "page.agent.session_menu" })[1]);
    fireEvent.click(await screen.findByRole("menuitem", { name: "page.agent.rename_session" }));

    const input = await screen.findByRole("textbox", { name: "page.agent.rename_placeholder" });
    expect((input as HTMLInputElement).value).toBe("Second chat");
    expect(backend.RenameSession).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: "  Renamed chat  " } });
    fireEvent.click(screen.getByRole("button", { name: "page.agent.rename_confirm" }));

    await waitFor(() =>
      expect(backend.RenameSession).toHaveBeenCalledWith("session-2", "Renamed chat"),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("shows a generated session title while the turn is still running", async () => {
    backend.ListSessions.mockResolvedValue([makeSession(SESSION_ID, "New conversation")]);
    await renderAgent();

    emit("session-title", { title: "Broken mod texture diagnosis" });

    // The title lands in the sidebar row of the session and in the open conversation's header.
    await waitFor(() =>
      expect(screen.getAllByText("Broken mod texture diagnosis")).toHaveLength(2),
    );
  });

  it("keeps a generated title for another conversation out of the open one", async () => {
    backend.ListSessions.mockResolvedValue([
      makeSession(SESSION_ID, "Current chat"),
      makeSession("session-2", "Second chat"),
    ]);
    await renderAgent();

    emit("session-title", { title: "Background summary" }, "session-2");

    expect(screen.getByText("Background summary")).toBeTruthy();
    expect(screen.queryByText("Second chat")).toBeNull();
    expect(screen.getByText("Nahida")).toBeTruthy();
  });

  it("keeps the row menu-open until the closing popup finishes animating", async () => {
    backend.ListSessions.mockResolvedValue([
      makeSession(SESSION_ID, "Current chat"),
      makeSession("session-2", "Second chat"),
    ]);
    await renderAgent();

    // jsdom has no CSS animations, so the exit animation would end immediately; hold it open to
    // observe the state the row is in while the popup is still on screen.
    let finishExit = () => {};
    const exitFinished = new Promise<void>((resolve) => {
      finishExit = resolve;
    });
    const realGetAnimations = Element.prototype.getAnimations;
    Element.prototype.getAnimations = () => [{ finished: exitFinished } as unknown as Animation];

    try {
      fireEvent.click(screen.getAllByRole("button", { name: "page.agent.session_menu" })[1]);
      await screen.findByRole("menu");

      const row = screen.getByText("Second chat").closest("[data-session-id]");
      expect(row?.hasAttribute("data-menu-open")).toBe(true);

      fireEvent.click(screen.getByRole("menuitem", { name: "page.agent.rename_session" }));
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 50));
      });

      // The dialog is up, but the popup is still animating out, so the trigger it is anchored to
      // has to stay in place and visible. Query the DOM directly: the modal dialog hides the rest
      // of the page from the accessibility tree.
      expect(document.querySelector('[data-slot="dropdown-menu-content"]')).toBeTruthy();
      expect(row?.hasAttribute("data-menu-open")).toBe(true);

      await act(async () => {
        finishExit();
      });

      await waitFor(() =>
        expect(document.querySelector('[data-slot="dropdown-menu-content"]')).toBeNull(),
      );
      expect(row?.hasAttribute("data-menu-open")).toBe(false);
    } finally {
      Element.prototype.getAnimations = realGetAnimations;
    }
  });

  it("keeps the previous title when the rename dialog is cancelled", async () => {
    backend.ListSessions.mockResolvedValue([
      makeSession(SESSION_ID, "Current chat"),
      makeSession("session-2", "Second chat"),
    ]);
    await renderAgent();

    fireEvent.click(screen.getAllByRole("button", { name: "page.agent.session_menu" })[1]);
    fireEvent.click(await screen.findByRole("menuitem", { name: "page.agent.rename_session" }));

    const input = await screen.findByRole("textbox", { name: "page.agent.rename_placeholder" });
    fireEvent.change(input, { target: { value: "Discarded" } });
    fireEvent.click(screen.getByRole("button", { name: "g.cancel" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(backend.RenameSession).not.toHaveBeenCalled();
  });

  it("deletes a session after the row menu confirmation", async () => {
    backend.ListSessions.mockResolvedValue([
      makeSession(SESSION_ID, "Current chat"),
      makeSession("session-2", "Second chat"),
    ]);
    backend.DeleteSession.mockResolvedValue(undefined);
    await renderAgent();

    fireEvent.click(screen.getAllByRole("button", { name: "page.agent.session_menu" })[1]);
    fireEvent.click(await screen.findByRole("menuitem", { name: "page.agent.delete_session" }));

    expect(await screen.findByRole("alertdialog")).toBeTruthy();
    expect(backend.DeleteSession).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "page.agent.delete_confirm" }));

    await waitFor(() => expect(backend.DeleteSession).toHaveBeenCalledWith("session-2"));
  });

  it("keeps the session when the delete confirmation is cancelled", async () => {
    backend.ListSessions.mockResolvedValue([
      makeSession(SESSION_ID, "Current chat"),
      makeSession("session-2", "Second chat"),
    ]);
    await renderAgent();

    fireEvent.click(screen.getAllByRole("button", { name: "page.agent.session_menu" })[1]);
    fireEvent.click(await screen.findByRole("menuitem", { name: "page.agent.delete_session" }));
    await screen.findByRole("alertdialog");

    fireEvent.click(screen.getByRole("button", { name: "g.cancel" }));

    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(backend.DeleteSession).not.toHaveBeenCalled();
    expect(screen.getByText("Second chat")).toBeTruthy();
  });

  it("keeps the current empty conversation when starting a new chat", async () => {
    await renderAgent();

    fireEvent.click(screen.getByRole("button", { name: "page.agent.new_chat" }));

    await waitFor(() => expect(backend.GetSession).toHaveBeenCalledTimes(1));
    expect(backend.CreateSession).not.toHaveBeenCalled();
    expect(backend.ListSessions).toHaveBeenCalledTimes(1);
    expect(navigate).not.toHaveBeenCalled();
  });

  it("reuses an empty conversation in the scope instead of creating another", async () => {
    backend.ListSessions.mockResolvedValue([
      makeSession(SESSION_ID, "Current chat"),
      makeSession("session-2", "Empty chat"),
    ]);
    await renderAgent([], async (id: string) =>
      id === SESSION_ID ? makeSnapshot([{ sequence: 1, type: "message/user" }]) : makeSnapshot(),
    );

    fireEvent.click(screen.getByRole("button", { name: "page.agent.new_chat" }));

    // The empty session is detected by loading each session in the scope, so a navigation to
    // session-2 can only come from the reuse path and not from the empty-current-session guard.
    await waitFor(() => expect(backend.GetSession).toHaveBeenCalledWith("session-2"));
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/agent",
        search: { session: "session-2" },
        replace: true,
      }),
    );
    expect(backend.CreateSession).not.toHaveBeenCalled();
  });

  it("creates a conversation when the scope has no empty one", async () => {
    backend.ListSessions.mockResolvedValue([makeSession(SESSION_ID, "Current chat")]);
    backend.CreateSession.mockResolvedValue(makeSession("session-new", "New conversation"));
    await renderAgent([{ sequence: 1, type: "message/user" }]);

    fireEvent.click(screen.getByRole("button", { name: "page.agent.new_chat" }));

    await waitFor(() => expect(backend.CreateSession).toHaveBeenCalledWith({ type: "global" }));
    expect(navigate).toHaveBeenCalledWith({
      to: "/agent",
      search: { session: "session-new" },
      replace: true,
    });
  });

  it("keeps the previous conversation's streamed reasoning out of a new conversation", async () => {
    const emptySnapshot = makeSnapshot([], {
      summary: { id: "session-2", title: "Empty chat", running: false, scope: { type: "global" } },
    });
    backend.ListSessions.mockResolvedValue([
      makeSession(SESSION_ID, "Current chat"),
      makeSession("session-2", "Empty chat"),
    ]);
    await renderAgent([], async (id: string) =>
      id === SESSION_ID ? makeSnapshot([{ sequence: 1, type: "message/user" }]) : emptySnapshot,
    );

    emit("reasoning-delta", { delta: "이전 대화 추론" });
    expect(reasoningPanel()?.textContent).toContain("이전 대화 추론");

    // React replaces the subscription only once the new conversation has rendered, so a delta can
    // still be delivered through the handler that was bound to the conversation on screen when
    // the user asked for a new one.
    const staleHandler = subscriptions.active.at(-1);

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "page.agent.new_chat" }));
    });
    await screen.findByText("page.agent.hero_title");

    act(() => {
      staleHandler?.({
        data: {
          sessionId: SESSION_ID,
          runId: RUN_ID,
          sequence: 99,
          type: "reasoning-delta",
          payload: { delta: "이어지는 추론" },
        },
      });
    });

    expect(screen.queryByText("page.agent.reasoning")).toBeNull();
    expect(screen.getByText("page.agent.hero_title")).toBeTruthy();

    // The new conversation still streams its own reasoning.
    emit("reasoning-delta", { delta: "새 대화 추론" }, "session-2");
    expect(reasoningPanel()?.textContent).toContain("새 대화 추론");
  });
});

describe("Agent reasoning panel", () => {
  it("renders the reference-style empty conversation shell", async () => {
    await renderAgent();

    expect(screen.getByText("page.agent.new_chat")).toBeTruthy();
    expect(screen.getByText("page.agent.hero_title")).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "page.agent.placeholder" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "page.agent.send" }).hasAttribute("disabled")).toBe(
      true,
    );
  });

  it("streams reasoning expanded and folds it once the answer starts", async () => {
    await renderAgent();

    emit("reasoning-delta", { delta: "모드 폴더를 확인합니다." });
    expect(reasoningPanel()?.open).toBe(true);
    expect(reasoningPanel()?.textContent).toContain("모드 폴더를 확인합니다.");

    emit("assistant-delta", { delta: "확인했습니다." });

    expect(reasoningPanel()?.open).toBe(false);
  });

  it("keeps a manual collapse while reasoning keeps streaming", async () => {
    await renderAgent();

    emit("reasoning-delta", { delta: "첫 단계" });
    fireEvent.click(screen.getByText("page.agent.reasoning"));
    expect(reasoningPanel()?.open).toBe(false);

    emit("reasoning-delta", { delta: " 둘째 단계" });

    expect(reasoningPanel()?.open).toBe(false);
    expect(reasoningPanel()?.textContent).toContain("첫 단계 둘째 단계");
  });

  it("folds saved assistant reasoning", async () => {
    await renderAgent([
      {
        sequence: 1,
        turnId: RUN_ID,
        type: "assistant",
        role: "assistant",
        text: "정리했습니다.",
        reasoning: "이전 추론 내용",
        createdAt: new Date().toISOString(),
      },
    ]);

    expect(reasoningPanel()?.open).toBe(false);
    expect(reasoningPanel()?.textContent).toContain("이전 추론 내용");
    expect(screen.getByText("정리했습니다.")).toBeTruthy();
  });

  it("keeps streamed assistant text before the tool that follows it", async () => {
    await renderAgent();

    emit("assistant-delta", { delta: "조사 시작하겠습니다" });
    emit("reasoning-delta", { delta: "대상을 확인합니다" });
    emit("tool-start", { id: "tool-1", name: "read_file" });

    const assistant = screen.getByText("조사 시작하겠습니다");
    const reasoning = screen.getByText("대상을 확인합니다");
    const tool = screen.getByText("read_file");
    expect(
      assistant.compareDocumentPosition(reasoning) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(reasoning.compareDocumentPosition(tool) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    emit("tool-end", { toolCallId: "tool-1", toolName: "read_file", result: { ok: true } });
    emit("assistant-delta", { delta: "조사를 마쳤습니다" });

    const completedTool = screen.getByText("read_file");
    const finalAnswer = screen.getByText("조사를 마쳤습니다");
    expect(
      completedTool.compareDocumentPosition(finalAnswer) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });
});

describe("Agent image input", () => {
  const pngBytes = [0x89, 0x50, 0x4e, 0x47];
  const pngData = "iVBORw==";

  async function renderWithImages(entries: unknown[] = []) {
    await renderAgent(entries, async () => makeSnapshot(entries, { supportsImages: true }));
  }

  async function attachFile(name: string, type: string, bytes: number[] = pngBytes) {
    const input = document.querySelector('input[type="file"]');
    if (!input) throw new Error("the composer has no file input");
    fireEvent.change(input, {
      target: { files: [new File([new Uint8Array(bytes)], name, { type })] },
    });
    await waitFor(() => expect(input).toBeTruthy());
  }

  it("hides the attachment control while the model does not accept images", async () => {
    await renderAgent();

    expect(screen.queryByRole("button", { name: "page.agent.attach_image" })).toBeNull();
  });

  it("sends an attached image with the message", async () => {
    backend.Send.mockResolvedValue({ runId: RUN_ID });
    await renderWithImages();

    await attachFile("shot.png", "image/png");
    const thumbnail = await screen.findByRole("img", { name: "shot.png" });
    expect(thumbnail.getAttribute("src")).toBe(`data:image/png;base64,${pngData}`);

    fireEvent.change(screen.getByRole("textbox", { name: "page.agent.placeholder" }), {
      target: { value: "이 스크린샷을 봐줘" },
    });
    fireEvent.click(screen.getByRole("button", { name: "page.agent.send" }));

    await waitFor(() =>
      expect(backend.Send).toHaveBeenCalledWith(SESSION_ID, "이 스크린샷을 봐줘", [
        { name: "shot.png", mimeType: "image/png", data: pngData },
      ]),
    );
  });

  it("sends an image-only message", async () => {
    backend.Send.mockResolvedValue({ runId: RUN_ID });
    await renderWithImages();

    await attachFile("shot.png", "image/png");
    await screen.findByRole("img", { name: "shot.png" });

    fireEvent.click(screen.getByRole("button", { name: "page.agent.send" }));

    await waitFor(() =>
      expect(backend.Send).toHaveBeenCalledWith(SESSION_ID, "", [
        { name: "shot.png", mimeType: "image/png", data: pngData },
      ]),
    );
  });

  it("rejects an unsupported image type", async () => {
    await renderWithImages();

    await attachFile("scan.tiff", "image/tiff");

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("page.agent.image_unsupported_type"),
    );
    expect(screen.queryByRole("img", { name: "scan.tiff" })).toBeNull();
  });

  it("renders stored attachments and tool images from the session snapshot", async () => {
    await renderAgent([
      {
        sequence: 1,
        turnId: RUN_ID,
        type: "turn/start",
        role: "user",
        text: "확인",
        images: [
          {
            name: "stored.png",
            mimeType: "image/png",
            bytes: 4,
            path: "agent/images/session-1/a.png",
            src: "C:\\data\\agent\\images\\session-1\\a.png",
          },
        ],
        createdAt: new Date().toISOString(),
      },
      {
        sequence: 2,
        turnId: RUN_ID,
        type: "tool/end",
        toolName: "mcp__blender__get_screenshot_of_window_as_image",
        toolCallId: "tool-1",
        result: { content: [{ type: "text", text: "[image: image/png, 4 bytes]" }] },
        images: [
          {
            mimeType: "image/png",
            bytes: 4,
            path: "agent/images/session-1/b.png",
            src: "C:\\data\\agent\\images\\session-1\\b.png",
          },
        ],
        createdAt: new Date().toISOString(),
      },
    ]);

    const stored = await screen.findByRole("img", { name: "stored.png" });
    expect(stored.getAttribute("src")).toMatch(/^\/protocol\/local\?path=.*a\.png$/);

    const toolImage = screen.getByRole("img", {
      name: "mcp__blender__get_screenshot_of_window_as_image",
    });
    expect(toolImage.getAttribute("src")).toMatch(/^\/protocol\/local\?path=.*b\.png$/);
  });
});

describe("Agent conversation revert", () => {
  const conversation = [
    {
      sequence: 1,
      turnId: RUN_ID,
      type: "turn/start",
      role: "user",
      text: "first request",
      createdAt: new Date().toISOString(),
    },
    {
      sequence: 2,
      turnId: RUN_ID,
      type: "message/assistant",
      role: "assistant",
      text: "first answer",
      createdAt: new Date().toISOString(),
    },
  ];

  it("stages a revert from a user message and refills the composer", async () => {
    backend.RevertSession.mockResolvedValue(
      makeSnapshot(conversation, {
        revert: { boundarySequence: 1, boundaryTurnId: RUN_ID, revertedCount: 2, createdAt: "now" },
      }),
    );
    await renderAgent(conversation);

    fireEvent.click(screen.getByRole("button", { name: "page.agent.revert_message" }));

    await waitFor(() => expect(backend.RevertSession).toHaveBeenCalledWith(SESSION_ID, 1));
    await waitFor(() =>
      expect(
        (screen.getByRole("textbox", { name: "page.agent.placeholder" }) as HTMLTextAreaElement)
          .value,
      ).toBe("first request"),
    );
    expect(screen.getByText("page.agent.revert_pending")).toBeTruthy();
    expect(screen.getByRole("button", { name: "page.agent.revert_undo" })).toBeTruthy();
  });

  it("clears a staged revert from the banner", async () => {
    const staged = makeSnapshot(conversation, {
      revert: { boundarySequence: 1, boundaryTurnId: RUN_ID, revertedCount: 2, createdAt: "now" },
    });
    backend.UnrevertSession.mockResolvedValue(makeSnapshot(conversation));
    await renderAgent(conversation, () => Promise.resolve(staged));

    fireEvent.click(await screen.findByRole("button", { name: "page.agent.revert_undo" }));

    await waitFor(() => expect(backend.UnrevertSession).toHaveBeenCalledWith(SESSION_ID));
    await waitFor(() => expect(screen.queryByText("page.agent.revert_pending")).toBeNull());
  });

  it("copies a user message", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    await renderAgent(conversation);

    fireEvent.click(screen.getByRole("button", { name: "page.agent.copy_message" }));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith("first request"));
  });

  it("keeps an in-progress composer draft when reverting", async () => {
    backend.RevertSession.mockResolvedValue(
      makeSnapshot(conversation, {
        revert: { boundarySequence: 1, boundaryTurnId: RUN_ID, revertedCount: 2, createdAt: "now" },
      }),
    );
    await renderAgent(conversation);

    const composer = screen.getByRole("textbox", {
      name: "page.agent.placeholder",
    }) as HTMLTextAreaElement;
    fireEvent.change(composer, { target: { value: "still typing" } });
    fireEvent.click(screen.getByRole("button", { name: "page.agent.revert_message" }));

    await waitFor(() => expect(backend.RevertSession).toHaveBeenCalledWith(SESSION_ID, 1));
    expect(composer.value).toBe("still typing");
  });

  it("drops reverted entries from the optimistic view after sending", async () => {
    const reverted = conversation.map((entry) => ({ ...entry, reverted: true }));
    const staged = makeSnapshot(reverted, {
      revert: { boundarySequence: 1, boundaryTurnId: RUN_ID, revertedCount: 2, createdAt: "now" },
    });
    backend.Send.mockResolvedValue({ runId: RUN_ID });
    await renderAgent(reverted, () => Promise.resolve(staged));

    const composer = screen.getByRole("textbox", { name: "page.agent.placeholder" });
    fireEvent.change(composer, { target: { value: "next request" } });
    fireEvent.click(screen.getByRole("button", { name: "page.agent.send" }));

    await waitFor(() => expect(backend.Send).toHaveBeenCalled());
    await waitFor(() => expect(screen.queryByText("first request")).toBeNull());
    expect(screen.getByText("next request")).toBeTruthy();
  });

  it("disables copy for a message with no text", async () => {
    await renderAgent([
      {
        sequence: 1,
        turnId: RUN_ID,
        type: "turn/start",
        role: "user",
        images: [
          {
            name: "only.png",
            mimeType: "image/png",
            bytes: 4,
            path: "agent/images/session-1/only.png",
            src: "C:\\data\\agent\\images\\session-1\\only.png",
          },
        ],
        createdAt: new Date().toISOString(),
      },
    ]);

    const copy = screen.getByRole("button", {
      name: "page.agent.copy_message",
    }) as HTMLButtonElement;
    expect(copy.disabled).toBe(true);
  });
});

describe("Agent context meter", () => {
  const conversation = [
    {
      sequence: 1,
      turnId: RUN_ID,
      type: "turn/start",
      role: "user",
      text: "first request",
      createdAt: new Date().toISOString(),
    },
  ];

  it("renders nothing while the session reports no context usage", async () => {
    await renderAgent(conversation);

    expect(screen.queryByRole("button", { name: "page.agent.context_used" })).toBeNull();
  });

  it("shows the occupancy ring and opens the composition panel", async () => {
    const contextUsage = {
      projectedTokens: 32_000,
      contextWindow: 128_000,
      systemTokens: 120,
      toolsTokens: 21_500,
      messageTokens: 477_000,
    };
    await renderAgent(conversation, () =>
      Promise.resolve(makeSnapshot(conversation, { contextUsage })),
    );

    const trigger = await screen.findByRole("button", { name: "page.agent.context_used" });
    expect(trigger.textContent).toContain("25%");

    fireEvent.click(trigger);
    const panel = await screen.findByRole("dialog");
    expect(panel.textContent).toContain("~32K / 128K");
    expect(panel.textContent).toContain("page.agent.context_system");
    expect(panel.textContent).toContain("~21.5K");
    expect(panel.textContent).toContain("~477K");
  });

  it("prefers live usage from the usage stream event over the snapshot", async () => {
    const contextUsage = {
      projectedTokens: 32_000,
      contextWindow: 128_000,
      systemTokens: 0,
      toolsTokens: 0,
      messageTokens: 0,
    };
    await renderAgent(conversation, () =>
      Promise.resolve(makeSnapshot(conversation, { contextUsage })),
    );
    await screen.findByRole("button", { name: "page.agent.context_used" });

    emit("status", { status: "running" });
    emit("usage", {
      inputTokens: 64_000,
      outputTokens: 10,
      contextUsage: { ...contextUsage, projectedTokens: 64_000 },
    });

    await waitFor(() => expect(screen.getByText("50%")).toBeTruthy());
  });

  it("applies live usage while a restored session is still running", async () => {
    const contextUsage = {
      projectedTokens: 32_000,
      contextWindow: 128_000,
      systemTokens: 0,
      toolsTokens: 0,
      messageTokens: 0,
    };
    await renderAgent(conversation, () =>
      Promise.resolve(
        makeSnapshot(conversation, {
          summary: { id: SESSION_ID, title: "Nahida", running: true, scope: { type: "global" } },
          contextUsage,
        }),
      ),
    );
    await screen.findByRole("button", { name: "page.agent.context_used" });

    // A restored running session only knows the "restored" run sentinel, so live usage is matched by
    // session instead; the streamed reading must still replace the snapshot one.
    emit("usage", {
      inputTokens: 64_000,
      outputTokens: 5,
      contextUsage: { ...contextUsage, projectedTokens: 64_000 },
    });

    await waitFor(() => expect(screen.getByText("50%")).toBeTruthy());
  });
});
