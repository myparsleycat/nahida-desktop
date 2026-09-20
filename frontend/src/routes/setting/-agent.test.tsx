// @vitest-environment jsdom

import { Service as Agent } from "@bindings/agent";
import type {
  AgentCredentialView,
  AgentProviderCatalogView,
  AgentProviderTestView,
  AgentProviderView,
  AgentSettingsView,
  MCPServerView,
} from "@bindings/agent/models";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { CancellablePromise } from "@wailsio/runtime";
import { Suspense, type ComponentType } from "react";
import { toast } from "sonner";
import { afterEach, beforeAll, beforeEach, expect, it, vi } from "vitest";

vi.mock("@bindings/agent", () => ({
  Service: {
    GetSettings: vi.fn(),
    UpdateSettings: vi.fn(),
    TestProvider: vi.fn(),
    ListProviders: vi.fn(),
    UpdateProviderCredential: vi.fn(),
    StartProviderLogin: vi.fn(),
    CompleteProviderLogin: vi.fn(),
    CancelProviderLogin: vi.fn(),
    SignOutProvider: vi.fn(),
    RefreshProviderCatalog: vi.fn(),
    ListMCPServers: vi.fn(),
    ListSkills: vi.fn(),
    TestMCPServer: vi.fn(),
    UpsertMCPServer: vi.fn(),
    DeleteMCPServer: vi.fn(),
  },
}));
vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  createFileRoute: () => (options: { component: ComponentType }) => ({ options }),
}));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { Route } from "./agent";

const view: AgentSettingsView = {
  provider: "custom",
  protocol: "openai-compatible",
  endpoint: "https://example.com/v1",
  model: "test-model",
  contextWindowSize: 131072,
  maxOutputTokens: 4096,
  reasoning: "auto",
  supportsImages: false,
  credential: { kind: "api" },
  headers: [
    { name: "X-Title", value: "Nahida Desktop", secret: false, configured: true },
    { name: "api-key", secret: true, configured: true },
  ],
};

const openaiProvider: AgentProviderView = {
  id: "openai",
  name: "OpenAI",
  endpoint: "https://api.openai.com/v1",
  defaultProtocol: "openai-responses",
  supportsApiKey: true,
  supportsOAuth: true,
  custom: false,
  credential: { kind: "none" },
  models: [
    {
      id: "gpt-5.5",
      name: "GPT-5.5",
      protocol: "openai-responses",
      contextWindow: 1050000,
      maxOutputTokens: 128000,
      supportsImages: true,
      reasoning: ["low", "high"],
      oauth: true,
    },
    {
      id: "gpt-5.4-mini",
      name: "GPT-5.4 mini",
      protocol: "openai-responses",
      contextWindow: 400000,
      maxOutputTokens: 64000,
      supportsImages: false,
      reasoning: ["low"],
      oauth: true,
    },
  ],
};

const goProvider: AgentProviderView = {
  id: "opencode-go",
  name: "OpenCode Go",
  endpoint: "https://opencode.ai/zen/go/v1",
  defaultProtocol: "openai-compatible",
  supportsApiKey: true,
  supportsOAuth: false,
  custom: false,
  credential: { kind: "api" },
  models: [
    {
      id: "kimi-k3",
      name: "Kimi K3",
      protocol: "anthropic",
      contextWindow: 256000,
      maxOutputTokens: 64000,
      supportsImages: false,
      oauth: true,
    },
    {
      id: "qwen3.8-flash",
      name: "Qwen3.8 Flash",
      protocol: "anthropic",
      contextWindow: 131072,
      maxOutputTokens: 32768,
      supportsImages: false,
      reasoning: ["low", "medium"],
      oauth: true,
    },
  ],
};

const customProvider: AgentProviderView = {
  id: "custom",
  name: "",
  endpoint: "",
  defaultProtocol: "",
  supportsApiKey: true,
  supportsOAuth: false,
  custom: true,
  credential: { kind: "none" },
};

const catalogView: AgentProviderCatalogView = {
  catalog: { source: "https://models.dev/api.json", updatedAt: "2026-09-18T00:00:00Z" },
  providers: [openaiProvider, goProvider, customProvider],
};

const catalogWith = (
  providerID: string,
  credential: AgentCredentialView,
): AgentProviderCatalogView => ({
  ...catalogView,
  providers: (catalogView.providers ?? []).map((provider) =>
    provider.id === providerID ? { ...provider, credential } : provider,
  ),
});

const testResult: AgentProviderTestView = {
  provider: "openai",
  model: "gpt-5.5",
  endpoint: "https://api.openai.com/v1",
  latencyMs: 812,
};

const mcpServer: MCPServerView = {
  id: "server-1",
  name: "everything",
  transport: "stdio",
  executable: "npx",
  enabled: true,
  status: "disconnected",
};

beforeEach(() => {
  vi.mocked(Agent.GetSettings).mockResolvedValue(view);
  vi.mocked(Agent.UpdateSettings).mockResolvedValue(view);
  vi.mocked(Agent.TestProvider).mockResolvedValue(testResult);
  vi.mocked(Agent.ListProviders).mockResolvedValue(catalogView);
  vi.mocked(Agent.UpdateProviderCredential).mockResolvedValue(goProvider);
  vi.mocked(Agent.ListMCPServers).mockResolvedValue([]);
  vi.mocked(Agent.ListSkills).mockResolvedValue([]);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

beforeAll(async () => {
  // The route plugin code-splits route components, so warm the lazy import before the first render.
  const Component = Route.options.component as unknown as { preload?: () => Promise<unknown> };
  await Component?.preload?.();
});

async function renderSettings() {
  const Component = Route.options.component;
  if (!Component) throw new Error("agent settings route has no component");
  await act(async () => {
    render(
      <Suspense fallback={null}>
        <Component />
      </Suspense>,
    );
  });
  await screen.findByRole("button", { name: "g.save" });
}

async function clickButton(name: string) {
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name }));
  });
}

async function openSelectList(name: string) {
  const trigger = screen.getByRole("combobox", { name });
  await act(async () => {
    fireEvent.click(trigger);
  });
  // The trigger points at the listbox it opened, so queries stay scoped to that popup.
  const list = document.getElementById(trigger.getAttribute("aria-controls") ?? "");
  if (!list) throw new Error(`select "${name}" did not open`);
  return list;
}

async function clickOption(list: HTMLElement, optionName: string) {
  const option = await within(list).findByRole("option", { name: optionName });
  // Base UI commits a mouse selection only after the item saw a pointer down.
  fireEvent.pointerDown(option, { pointerType: "mouse" });
  await act(async () => {
    fireEvent.click(option);
  });
}

async function chooseOption(selectName: string, optionName: string) {
  await clickOption(await openSelectList(selectName), optionName);
}

async function expectOptions(selectName: string, optionNames: string[]) {
  const list = await openSelectList(selectName);
  expect(within(list).getAllByRole("option")).toHaveLength(optionNames.length);
  for (const optionName of optionNames) {
    expect(within(list).getByRole("option", { name: optionName })).toBeTruthy();
  }
}

// The mocked translator returns the key, so a connected provider keeps the raw suffix.
const providerLabel = (name: string, connected = false) =>
  connected ? `${name} · page.agent.provider_connected` : name;

it("renders the configured headers", async () => {
  await renderSettings();
  expect(screen.getByDisplayValue("X-Title")).toBeTruthy();
  expect(screen.getByDisplayValue("Nahida Desktop")).toBeTruthy();
  expect(screen.getByDisplayValue("api-key")).toBeTruthy();
  expect(screen.getByPlaceholderText("page.agent.configured")).toBeTruthy();
});

it("saves headers and keeps configured secret values", async () => {
  await renderSettings();
  await clickButton("g.save");
  expect(Agent.UpdateSettings).toHaveBeenCalledOnce();
  const input = vi.mocked(Agent.UpdateSettings).mock.calls[0][0];
  expect(input.headers).toEqual([
    expect.objectContaining({ name: "X-Title", value: "Nahida Desktop", secret: false }),
    expect.objectContaining({ name: "api-key", secret: true, secretAction: "keep" }),
  ]);
});

it("marks an edited secret header for replacement", async () => {
  await renderSettings();
  fireEvent.change(screen.getByPlaceholderText("page.agent.configured"), {
    target: { value: "rotated-secret" },
  });
  await clickButton("g.save");
  expect(Agent.UpdateSettings).toHaveBeenCalledOnce();
  const input = vi.mocked(Agent.UpdateSettings).mock.calls[0][0];
  expect(input.headers?.[1]).toEqual(
    expect.objectContaining({
      name: "api-key",
      secret: true,
      secretAction: "replace",
      value: "rotated-secret",
    }),
  );
});

it("saves a newly added header", async () => {
  await renderSettings();
  fireEvent.click(screen.getByRole("button", { name: "page.agent.add_header" }));
  const names = screen.getAllByPlaceholderText("Header");
  fireEvent.change(names[2], { target: { value: "X-Extra" } });
  // The configured secret row keeps its own placeholder, so the new row is the second "Value".
  fireEvent.change(screen.getAllByPlaceholderText("Value")[1], { target: { value: "extra" } });
  await clickButton("g.save");
  expect(Agent.UpdateSettings).toHaveBeenCalledOnce();
  const input = vi.mocked(Agent.UpdateSettings).mock.calls[0][0];
  expect(input.headers?.at(-1)).toEqual(
    expect.objectContaining({ name: "X-Extra", value: "extra", secret: false }),
  );
});

it("moves a header into encrypted storage when marked secret", async () => {
  await renderSettings();
  fireEvent.click(screen.getAllByRole("checkbox", { name: "page.agent.secret" })[0]);
  await clickButton("g.save");
  expect(Agent.UpdateSettings).toHaveBeenCalledOnce();
  const input = vi.mocked(Agent.UpdateSettings).mock.calls[0][0];
  expect(input.headers?.[0]).toEqual(
    expect.objectContaining({ name: "X-Title", secret: true, secretAction: "keep" }),
  );
});

it("saves the image capability checkbox below the model field", async () => {
  await renderSettings();
  const checkbox = screen.getByRole("checkbox", { name: "page.agent.supports_images" });
  expect((checkbox as HTMLInputElement).checked).toBe(false);

  fireEvent.click(checkbox);
  await clickButton("g.save");
  expect(Agent.UpdateSettings).toHaveBeenCalledOnce();
  const input = vi.mocked(Agent.UpdateSettings).mock.calls[0][0];
  expect(input.supportsImages).toBe(true);
});

it("sends configured headers when testing the connection", async () => {
  await renderSettings();
  await clickButton("page.agent.test_connection");
  expect(Agent.TestProvider).toHaveBeenCalledOnce();
  const input = vi.mocked(Agent.TestProvider).mock.calls[0][0];
  expect(input.headers?.[0]).toEqual(
    expect.objectContaining({ name: "X-Title", value: "Nahida Desktop" }),
  );
});

it("drops empty header rows before saving", async () => {
  await renderSettings();
  fireEvent.click(screen.getByRole("button", { name: "page.agent.add_header" }));
  await clickButton("g.save");
  expect(Agent.UpdateSettings).toHaveBeenCalledOnce();
  const input = vi.mocked(Agent.UpdateSettings).mock.calls[0][0];
  expect(input.headers).toHaveLength(2);
});

it("lists built-in providers with their connection state", async () => {
  await renderSettings();
  // The trigger shows the provider name instead of its raw id.
  expect(
    within(screen.getByLabelText("page.agent.provider_select")).getByText(
      "page.agent.provider_custom",
    ),
  ).toBeTruthy();
  const providers = await openSelectList("page.agent.provider_select");
  expect(within(providers).getByRole("option", { name: "OpenAI" })).toBeTruthy();
  expect(
    within(providers).getByRole("option", { name: providerLabel("OpenCode Go", true) }),
  ).toBeTruthy();
  expect(
    within(providers).getByRole("option", { name: providerLabel("page.agent.provider_custom") }),
  ).toBeTruthy();
});

it("fills endpoint, protocol, and model limits when a built-in provider is selected", async () => {
  await renderSettings();
  await chooseOption("page.agent.provider_select", providerLabel("OpenAI"));
  await clickButton("g.save");

  const input = vi.mocked(Agent.UpdateSettings).mock.calls[0][0];
  expect(input).toEqual(
    expect.objectContaining({
      provider: "openai",
      endpoint: "https://api.openai.com/v1",
      protocol: "openai-responses",
      model: "gpt-5.5",
      contextWindowSize: 1050000,
      maxOutputTokens: 128000,
      supportsImages: true,
    }),
  );
});

it("keeps the chosen model when the current provider is picked again", async () => {
  await renderSettings();
  await chooseOption("page.agent.provider_select", providerLabel("OpenAI"));
  fireEvent.change(screen.getByLabelText("page.agent.model"), {
    target: { value: "gpt-5.4-mini" },
  });

  await chooseOption("page.agent.provider_select", providerLabel("OpenAI"));

  expect((screen.getByLabelText("page.agent.model") as HTMLInputElement).value).toBe(
    "gpt-5.4-mini",
  );
});

it("follows the provider for the model, limits, and the key draft", async () => {
  await renderSettings();
  // A key typed for the previous provider must not survive the switch.
  await chooseOption("page.agent.api_key", "page.agent.replace");
  fireEvent.change(screen.getByLabelText("page.agent.api_key_value"), {
    target: { value: "typed-for-custom" },
  });
  await chooseOption("page.agent.provider_select", providerLabel("OpenAI"));
  await chooseOption("page.agent.provider_select", providerLabel("OpenCode Go", true));

  const keyAction = screen.getByLabelText("page.agent.api_key");
  expect(within(keyAction).getByText("page.agent.secret_configured")).toBeTruthy();
  expect(screen.queryByLabelText("page.agent.api_key_value")).toBeNull();

  await clickButton("g.save");
  const input = vi.mocked(Agent.UpdateSettings).mock.calls[0][0];
  expect(input).toEqual(
    expect.objectContaining({
      provider: "opencode-go",
      endpoint: "https://opencode.ai/zen/go/v1",
      model: "kimi-k3",
      contextWindowSize: 256000,
      maxOutputTokens: 64000,
      reasoning: "auto",
      supportsImages: false,
    }),
  );
});

it("hides the custom-only detail settings for a built-in provider", async () => {
  await renderSettings();
  expect(screen.getByLabelText("page.agent.protocol")).toBeTruthy();
  expect(screen.getByLabelText("page.agent.endpoint")).toBeTruthy();
  expect(screen.getByLabelText("page.agent.reasoning")).toBeTruthy();
  expect(screen.getAllByPlaceholderText("Header")).toHaveLength(2);
  // A custom endpoint offers every effort the adapter can send.
  const reasoningList = await openSelectList("page.agent.reasoning");
  expect(within(reasoningList).getByRole("option", { name: "xhigh" })).toBeTruthy();
  await clickOption(reasoningList, "auto");

  await chooseOption("page.agent.provider_select", providerLabel("OpenAI"));

  expect(screen.queryByLabelText("page.agent.protocol")).toBeNull();
  expect(screen.queryByLabelText("page.agent.endpoint")).toBeNull();
  expect(screen.queryByRole("checkbox", { name: "page.agent.supports_images" })).toBeNull();
  expect(screen.queryAllByPlaceholderText("Header")).toHaveLength(0);
  // The built-in model keeps its catalog efforts instead of the full custom list.
  expect(within(screen.getByLabelText("page.agent.reasoning")).getByText("auto")).toBeTruthy();
  await expectOptions("page.agent.reasoning", ["auto", "low", "high"]);
  // Picking a model and connecting the provider stay available.
  expect(screen.getByLabelText("page.agent.model")).toBeTruthy();
  expect(screen.getByLabelText("page.agent.api_key")).toBeTruthy();
});

it("saves the reasoning effort chosen for a built-in model", async () => {
  await renderSettings();
  await chooseOption("page.agent.provider_select", providerLabel("OpenAI"));
  await chooseOption("page.agent.reasoning", "high");
  await clickButton("g.save");
  expect(Agent.UpdateSettings).toHaveBeenCalledOnce();
  const input = vi.mocked(Agent.UpdateSettings).mock.calls[0][0];
  expect(input).toEqual(
    expect.objectContaining({ provider: "openai", model: "gpt-5.5", reasoning: "high" }),
  );
});

it("offers the catalog efforts of an Anthropic-routed built-in model", async () => {
  await renderSettings();
  await chooseOption("page.agent.provider_select", providerLabel("OpenCode Go", true));
  // The selected model declares no effort, so there is nothing to adjust.
  expect(screen.queryByLabelText("page.agent.reasoning")).toBeNull();

  fireEvent.change(screen.getByLabelText("page.agent.model"), {
    target: { value: "qwen3.8-flash" },
  });
  await expectOptions("page.agent.reasoning", ["auto", "low", "medium"]);
});

it("shows the connection state of the provider being selected", async () => {
  vi.mocked(Agent.ListProviders).mockResolvedValue(
    catalogWith("openai", { kind: "oauth", accountLabel: "user@example.com" }),
  );
  await renderSettings();
  // The custom provider is the selected one, so its key form is what renders first.
  expect(screen.getByLabelText("page.agent.api_key")).toBeTruthy();

  await chooseOption("page.agent.provider_select", providerLabel("OpenAI", true));
  expect(screen.getByText("user@example.com")).toBeTruthy();
  expect(screen.getByRole("button", { name: "page.agent.sign_out" })).toBeTruthy();
  expect(screen.queryByLabelText("page.agent.api_key")).toBeNull();
});

it("restores the stored headers when the provider changes", async () => {
  await renderSettings();
  fireEvent.change(screen.getByDisplayValue("Nahida Desktop"), {
    target: { value: "edited-value" },
  });
  await chooseOption("page.agent.provider_select", providerLabel("OpenAI"));
  expect(screen.queryByDisplayValue("edited-value")).toBeNull();

  await chooseOption("page.agent.provider_select", providerLabel("page.agent.provider_custom"));
  expect(screen.getByDisplayValue("Nahida Desktop")).toBeTruthy();
  expect(screen.queryByDisplayValue("edited-value")).toBeNull();
});

it("keeps the custom endpoint for the next time the custom provider is selected", async () => {
  await renderSettings();
  fireEvent.change(screen.getByLabelText("page.agent.endpoint"), {
    target: { value: "https://mine.example/v1" },
  });
  await chooseOption("page.agent.provider_select", providerLabel("OpenAI"));
  await chooseOption("page.agent.provider_select", providerLabel("page.agent.provider_custom"));
  expect((screen.getByLabelText("page.agent.endpoint") as HTMLInputElement).value).toBe(
    "https://mine.example/v1",
  );
});

it("applies catalog limits when a model id is typed", async () => {
  await renderSettings();
  await chooseOption("page.agent.provider_select", providerLabel("OpenAI"));
  fireEvent.change(screen.getByLabelText("page.agent.model"), {
    target: { value: "gpt-5.4-mini" },
  });
  await clickButton("g.save");

  const input = vi.mocked(Agent.UpdateSettings).mock.calls[0][0];
  expect(input).toEqual(
    expect.objectContaining({
      model: "gpt-5.4-mini",
      contextWindowSize: 400000,
      maxOutputTokens: 64000,
      supportsImages: false,
    }),
  );
});

it("stores a replacement API key for the selected provider", async () => {
  await renderSettings();
  await chooseOption("page.agent.provider_select", providerLabel("OpenCode Go", true));
  await chooseOption("page.agent.api_key", "page.agent.replace");
  fireEvent.change(screen.getByLabelText("page.agent.api_key_value"), {
    target: { value: "go-key" },
  });
  await clickButton("g.save");

  expect(Agent.UpdateProviderCredential).toHaveBeenCalledWith({
    providerId: "opencode-go",
    action: "replace",
    apiKey: "go-key",
  });
  expect(toast.success).toHaveBeenCalledWith("page.agent.settings_saved");
});

it("tests a typed API key before it is saved", async () => {
  await renderSettings();
  await chooseOption("page.agent.provider_select", providerLabel("OpenCode Go", true));
  await chooseOption("page.agent.api_key", "page.agent.replace");
  fireEvent.change(screen.getByLabelText("page.agent.api_key_value"), {
    target: { value: "go-key" },
  });
  await clickButton("page.agent.test_connection");

  expect(Agent.UpdateProviderCredential).not.toHaveBeenCalled();
  expect(Agent.TestProvider).toHaveBeenCalledWith(
    expect.objectContaining({ provider: "opencode-go", apiKey: "go-key" }),
  );
});

it("signs in to ChatGPT and reloads the provider list", async () => {
  vi.mocked(Agent.GetSettings).mockResolvedValue({
    ...view,
    provider: "openai",
    credential: { kind: "none" },
  });
  vi.mocked(Agent.StartProviderLogin).mockResolvedValue({
    provider: "openai",
    url: "https://auth.openai.com/oauth/authorize?state=1",
  });
  vi.mocked(Agent.CompleteProviderLogin).mockResolvedValue({
    ...view,
    provider: "openai",
    credential: { kind: "oauth", accountLabel: "user@example.com" },
  });
  vi.mocked(Agent.ListProviders)
    .mockResolvedValueOnce(catalogView)
    .mockResolvedValue(catalogWith("openai", { kind: "oauth", accountLabel: "user@example.com" }));
  await renderSettings();

  fireEvent.click(screen.getByRole("radio", { name: "page.agent.auth_account" }));
  await clickButton("page.agent.connect_chatgpt");

  expect(Agent.StartProviderLogin).toHaveBeenCalledWith("openai");
  expect(Agent.CompleteProviderLogin).toHaveBeenCalledOnce();
  expect(toast.success).toHaveBeenCalledWith("page.agent.login_complete");
  expect(Agent.ListProviders).toHaveBeenCalledTimes(2);
  // The plan does not serve the stored model, so the form moves to the newest one it does.
  expect((screen.getByLabelText("page.agent.model") as HTMLInputElement).value).toBe("gpt-5.5");
});

it("signs out of the ChatGPT account", async () => {
  vi.mocked(Agent.GetSettings).mockResolvedValue({
    ...view,
    provider: "openai",
    credential: { kind: "oauth", accountLabel: "user@example.com" },
  });
  vi.mocked(Agent.SignOutProvider).mockResolvedValue({
    ...(catalogView.providers?.[0] as never),
    credential: { kind: "none" },
  });
  vi.mocked(Agent.ListProviders).mockResolvedValue(
    catalogWith("openai", { kind: "oauth", accountLabel: "user@example.com" }),
  );
  await renderSettings();
  await clickButton("page.agent.sign_out");
  expect(Agent.SignOutProvider).toHaveBeenCalledWith("openai");
});

it("refreshes the model catalog", async () => {
  vi.mocked(Agent.RefreshProviderCatalog).mockResolvedValue(catalogView);
  await renderSettings();
  await clickButton("page.agent.catalog_refresh");
  expect(Agent.RefreshProviderCatalog).toHaveBeenCalledOnce();
  expect(toast.success).toHaveBeenCalledWith("page.agent.catalog_refreshed");
});

it("shows the model that answered the connection test", async () => {
  await renderSettings();
  await clickButton("page.agent.test_connection");
  expect(screen.getByRole("status").textContent).toBe("page.agent.test_result");
  expect(toast.success).toHaveBeenCalledWith("page.agent.connection_ok");
});

async function clickMCPTestButton() {
  // The provider section renders its own test button first, so the MCP row button is the last match.
  const button = screen.getAllByRole("button", { name: "page.agent.test_connection" }).at(-1);
  if (!button) throw new Error("MCP test button is missing");
  await act(async () => {
    fireEvent.click(button);
  });
}

it("reports the tool count after a successful MCP connection test", async () => {
  vi.mocked(Agent.ListMCPServers).mockResolvedValue([mcpServer]);
  vi.mocked(Agent.TestMCPServer).mockResolvedValue({
    ...mcpServer,
    status: "connected",
    toolCount: 4,
  });
  await renderSettings();
  await clickMCPTestButton();
  expect(Agent.TestMCPServer).toHaveBeenCalledWith("server-1");
  expect(toast.success).toHaveBeenCalledWith("page.agent.mcp_connected");
});

it("saves the transport chosen in the MCP editor", async () => {
  vi.mocked(Agent.ListMCPServers).mockResolvedValue([mcpServer]);
  vi.mocked(Agent.UpsertMCPServer).mockResolvedValue(mcpServer);
  await renderSettings();
  // The row button's accessible name also carries the transport and state, so click the name.
  await act(async () => {
    fireEvent.click(screen.getByText(mcpServer.name));
  });

  await chooseOption("Transport", "Streamable HTTP");
  await clickButton("Save");

  expect(Agent.UpsertMCPServer).toHaveBeenCalledWith(
    expect.objectContaining({ id: "server-1", transport: "streamable-http", name: "everything" }),
  );
});

it("surfaces an MCP connection failure reported by the backend", async () => {
  vi.mocked(Agent.ListMCPServers).mockResolvedValue([mcpServer]);
  vi.mocked(Agent.TestMCPServer).mockResolvedValue({
    ...mcpServer,
    status: "error",
    error: "executable file not found",
  });
  await renderSettings();
  await clickMCPTestButton();
  expect(toast.error).toHaveBeenCalledWith("executable file not found");
});

it("surfaces a rejected MCP connection test", async () => {
  vi.mocked(Agent.ListMCPServers).mockResolvedValue([mcpServer]);
  vi.mocked(Agent.TestMCPServer).mockRejectedValue(new Error("connection refused"));
  await renderSettings();
  await clickMCPTestButton();
  expect(toast.error).toHaveBeenCalledWith("connection refused");
});

it("shows the MCP test button as busy while the connection test runs", async () => {
  vi.mocked(Agent.ListMCPServers).mockResolvedValue([mcpServer]);
  let finish: (view: MCPServerView) => void = () => {};
  vi.mocked(Agent.TestMCPServer).mockReturnValue(
    new CancellablePromise<MCPServerView>((resolve) => {
      finish = resolve;
    }),
  );
  await renderSettings();
  await clickMCPTestButton();
  expect(Agent.TestMCPServer).toHaveBeenCalledWith("server-1");
  expect(screen.getAllByRole("button").some((button) => button.hasAttribute("disabled"))).toBe(
    true,
  );

  await act(async () => {
    finish({ ...mcpServer, status: "connected", toolCount: 1 });
  });
  expect(toast.success).toHaveBeenCalledWith("page.agent.mcp_connected");
});
