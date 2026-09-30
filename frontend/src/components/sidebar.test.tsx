// @vitest-environment jsdom

import { Service as Agent } from "@bindings/agent";
import type { AgentSettingsView } from "@bindings/agent/models";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { CancellablePromise } from "@wailsio/runtime";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const calls = vi.hoisted(() => ({
  navigate: vi.fn(),
}));

vi.mock("@bindings/platform", () => ({ Shell: { OpenExternal: vi.fn() } }));
vi.mock("@bindings/agent", () => ({
  Service: { GetSettings: vi.fn(), OpenSession: vi.fn() },
}));
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => calls.navigate,
  useLocation: () => ({ pathname: "/drive/drive/root-id" }),
}));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { language: "en" } }),
}));
vi.mock("@renderer/hooks/use-auth", () => ({
  useAuth: () => ({
    session: { drive: { rootId: "root-id" } },
    isBackendOffline: true,
  }),
}));
vi.mock("@renderer/store/drive", () => ({
  viewStore: {
    getState: () => ({ lastDriveId: "last-drive", lastShareId: "last-share" }),
  },
}));
vi.mock("@renderer/store/gamebanana", () => ({
  gameBananaStore: { getState: () => ({ requestModGameSync: vi.fn() }) },
}));
vi.mock("@renderer/store/global", () => ({
  useGlobalStore: (select: (state: { appStatus: null; transfers: [] }) => unknown) =>
    select({ appStatus: null, transfers: [] }),
}));

import { Sidebar } from "./sidebar";

const agentSettings: AgentSettingsView = {
  provider: "openai",
  protocol: "openai-responses",
  endpoint: "https://api.openai.com/v1",
  model: "test-model",
  contextWindowSize: 131072,
  maxOutputTokens: 4096,
  reasoning: "auto",
  supportsImages: false,
  credential: { kind: "none" },
};

beforeEach(() => {
  vi.mocked(Agent.GetSettings).mockResolvedValue(agentSettings);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

function renderSidebar() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  vi.spyOn(queryClient, "invalidateQueries");
  render(
    <QueryClientProvider client={queryClient}>
      <Sidebar />
    </QueryClientProvider>,
  );
  return queryClient;
}

it("retries drive queries and navigates when Drive is clicked while offline", () => {
  const queryClient = renderSidebar();
  fireEvent.click(screen.getByRole("button", { name: "page.drive.title_server_error" }));
  expect(queryClient.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["drive"] });
  expect(calls.navigate).toHaveBeenCalledWith({
    to: "/drive/drive/$id",
    params: { id: "last-drive" },
  });
});

it("retries drive queries and navigates when Share is clicked while offline", () => {
  const queryClient = renderSidebar();
  fireEvent.click(screen.getByRole("button", { name: "page.share_drive.title_server_error" }));
  expect(queryClient.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["drive"] });
  expect(calls.navigate).toHaveBeenCalledWith({
    to: "/drive/share/$id",
    params: { id: "last-share" },
  });
});

it("navigates to the backup page when Backup is clicked", () => {
  renderSidebar();
  fireEvent.click(screen.getByRole("button", { name: "page.backup.title" }));
  expect(calls.navigate).toHaveBeenCalledWith({ to: "/backup" });
});

it("hides the agent while settings are loading", () => {
  vi.mocked(Agent.GetSettings).mockReturnValue(new CancellablePromise(() => {}));
  renderSidebar();
  expect(screen.queryByRole("button", { name: "page.agent.title" })).toBeNull();
});

it("hides the agent when the selected built-in provider has no credential", async () => {
  const queryClient = renderSidebar();
  await waitFor(() => expect(queryClient.getQueryData(["agent", "settings"])).toBeDefined());
  expect(screen.queryByRole("button", { name: "page.agent.title" })).toBeNull();
});

it.each(["api", "oauth"])("shows the agent when the selected provider uses %s", async (kind) => {
  vi.mocked(Agent.GetSettings).mockResolvedValue({ ...agentSettings, credential: { kind } });
  renderSidebar();
  expect(await screen.findByRole("button", { name: "page.agent.title" })).toBeTruthy();
});

it("shows a custom endpoint that does not require a provider credential", async () => {
  vi.mocked(Agent.GetSettings).mockResolvedValue({ ...agentSettings, provider: "custom" });
  renderSidebar();
  expect(await screen.findByRole("button", { name: "page.agent.title" })).toBeTruthy();
});

it.each(["endpoint", "model"])("hides a custom provider with no %s", async (field) => {
  vi.mocked(Agent.GetSettings).mockResolvedValue({
    ...agentSettings,
    provider: "custom",
    [field]: " ",
  });
  const queryClient = renderSidebar();
  await waitFor(() => expect(queryClient.getQueryData(["agent", "settings"])).toBeDefined());
  expect(screen.queryByRole("button", { name: "page.agent.title" })).toBeNull();
});

it("hides the agent when settings cannot be loaded", async () => {
  vi.mocked(Agent.GetSettings).mockRejectedValue(new Error("settings unavailable"));
  const queryClient = renderSidebar();
  await waitFor(() =>
    expect(queryClient.getQueryState(["agent", "settings"])?.status).toBe("error"),
  );
  expect(screen.queryByRole("button", { name: "page.agent.title" })).toBeNull();
});

it("updates agent visibility after the saved provider credential changes", async () => {
  const queryClient = renderSidebar();
  await waitFor(() => expect(queryClient.getQueryData(["agent", "settings"])).toBeDefined());
  expect(screen.queryByRole("button", { name: "page.agent.title" })).toBeNull();

  vi.mocked(Agent.GetSettings).mockResolvedValue({ ...agentSettings, credential: { kind: "api" } });
  await act(async () => {
    await queryClient.invalidateQueries({ queryKey: ["agent", "settings"] });
  });
  expect(await screen.findByRole("button", { name: "page.agent.title" })).toBeTruthy();

  vi.mocked(Agent.GetSettings).mockResolvedValue(agentSettings);
  await act(async () => {
    await queryClient.invalidateQueries({ queryKey: ["agent", "settings"] });
  });
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "page.agent.title" })).toBeNull(),
  );
});
