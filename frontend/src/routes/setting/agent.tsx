import { Service as Agent } from "@bindings/agent";
import type {
  AgentHeaderView,
  AgentLoginView,
  AgentProviderCatalogView,
  AgentProviderTestView,
  AgentProviderView,
  AgentSettingsView,
  CatalogModel,
  MCPEntryInput,
  MCPServerInput,
  MCPServerView,
  SkillView,
  UpdateAgentSettingsInput,
} from "@bindings/agent/models";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Switch } from "@renderer/components/ui/switch";
import { toErrorMessage } from "@shared/utils";
import { createFileRoute } from "@tanstack/react-router";
import {
  FolderOpenIcon,
  Loader2Icon,
  LogOutIcon,
  PlusIcon,
  RefreshCwIcon,
  SaveIcon,
  ServerIcon,
  Trash2Icon,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/agent")({
  component: AgentSettingsRoute,
});

const emptyMCP: MCPServerInput = {
  name: "",
  transport: "stdio",
  executable: "",
  arguments: [],
  workingDirectory: "",
  endpoint: "",
  entries: [],
  enabled: false,
};

interface HeaderDraft {
  name: string;
  value: string;
  secret: boolean;
  secretAction?: string;
  configured: boolean;
}

const headerDrafts = (headers: AgentHeaderView[] | null | undefined): HeaderDraft[] =>
  (headers ?? []).map((header) => ({
    name: header.name,
    value: header.value ?? "",
    secret: header.secret,
    secretAction: header.secret ? "keep" : undefined,
    configured: header.configured,
  }));

const reasoningOrder = ["auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"];

const protocolOptions = [
  { value: "openai-responses", label: "OpenAI Responses" },
  { value: "openai-compatible", label: "OpenAI-compatible" },
  { value: "anthropic", label: "Anthropic Messages" },
];

const providerOf = (
  catalog: AgentProviderCatalogView | undefined,
  providerID: string,
): AgentProviderView | undefined => catalog?.providers?.find((entry) => entry.id === providerID);

const modelOf = (
  provider: AgentProviderView | undefined,
  modelID: string,
): CatalogModel | undefined => provider?.models?.find((entry) => entry.id === modelID);

// applyModelDefaults fills the limits a catalog model declares so the user only picks a model id.
const applyModelDefaults = (
  settings: AgentSettingsView,
  model: CatalogModel,
): AgentSettingsView => ({
  ...settings,
  model: model.id,
  protocol: model.protocol,
  contextWindowSize: model.contextWindow,
  maxOutputTokens: model.maxOutputTokens,
  supportsImages: model.supportsImages,
  // Keep the effort only while the new model declares it; the adapter drops anything else.
  reasoning: model.reasoning?.includes(settings.reasoning) ? settings.reasoning : "auto",
});

function AgentSettingsRoute() {
  const { t } = useTranslation();
  const [settings, setSettings] = useState<AgentSettingsView>();
  const [catalog, setCatalog] = useState<AgentProviderCatalogView>();
  const [apiKey, setAPIKey] = useState("");
  const [apiKeyAction, setAPIKeyAction] = useState("keep");
  const [customDraft, setCustomDraft] = useState<AgentSettingsView>();
  const [authMode, setAuthMode] = useState<"account" | "key">("key");
  const [login, setLogin] = useState<AgentLoginView>();
  const [testResult, setTestResult] = useState<AgentProviderTestView>();
  const [headers, setHeaders] = useState<HeaderDraft[]>([]);
  const [servers, setServers] = useState<MCPServerView[]>([]);
  const [skills, setSkills] = useState<SkillView[]>([]);
  const [mcpDraft, setMCPDraft] = useState<MCPServerInput>();
  const [busy, setBusy] = useState(false);

  const reload = useCallback(async () => {
    const [nextSettings, nextCatalog, nextServers, nextSkills] = await Promise.all([
      Agent.GetSettings(),
      Agent.ListProviders(),
      Agent.ListMCPServers(),
      Agent.ListSkills(),
    ]);
    setSettings(nextSettings);
    setCatalog(nextCatalog);
    setHeaders(headerDrafts(nextSettings.headers));
    setAuthMode(nextSettings.credential.kind === "oauth" ? "account" : "key");
    setServers(nextServers ?? []);
    setSkills(nextSkills ?? []);
  }, []);

  useEffect(() => {
    // oxlint-disable-next-line react/set-state-in-effect -- Wails settings must load after mount.
    void reload().catch((error) => toast.error(String(error)));
  }, [reload]);

  const activeProvider = settings ? providerOf(catalog, settings.provider) : undefined;

  // Stored settings describe the saved provider, so a selection the user has not saved yet keeps
  // its place in the form.
  const applyStoredSettings = (next: AgentSettingsView) => {
    if (next.provider !== settings?.provider) return;
    setSettings(next);
    setHeaders(headerDrafts(next.headers));
  };

  // A plan login narrows the provider to the models the account serves, so a model the plan does
  // not serve is replaced by the newest one it does.
  const followPlanModels = (providers: AgentProviderCatalogView) => {
    if (!settings) return;
    const provider = providerOf(providers, settings.provider);
    const models = provider?.models ?? [];
    if (provider?.credential.kind !== "oauth" || models.length === 0) return;
    if (models.some((model) => model.id === settings.model)) return;
    const model = models[0];
    setSettings(applyModelDefaults({ ...settings, model: model.id }, model));
  };

  const settingsInput = (): UpdateAgentSettingsInput | undefined =>
    settings
      ? {
          provider: settings.provider,
          protocol: settings.protocol,
          endpoint: settings.endpoint,
          model: settings.model,
          contextWindowSize: settings.contextWindowSize,
          maxOutputTokens: settings.maxOutputTokens,
          reasoning: settings.reasoning,
          supportsImages: settings.supportsImages,
          headers: headers
            .filter((header) => header.name.trim() !== "")
            .map((header) => ({
              name: header.name.trim(),
              value: header.secret && header.secretAction !== "replace" ? undefined : header.value,
              secret: header.secret,
              secretAction: header.secret ? header.secretAction : undefined,
            })),
        }
      : undefined;

  const saveSettings = async () => {
    const input = settingsInput();
    if (!input) return;
    setBusy(true);
    try {
      await Agent.UpdateSettings(input);
      if (apiKeyAction !== "keep") {
        const provider = await Agent.UpdateProviderCredential({
          providerId: input.provider,
          action: apiKeyAction,
          apiKey: apiKeyAction === "replace" ? apiKey : undefined,
        });
        setCatalog((current) =>
          current
            ? {
                ...current,
                providers: (current.providers ?? []).map((entry) =>
                  entry.id === provider.id ? provider : entry,
                ),
              }
            : current,
        );
      }
      const next = await Agent.GetSettings();
      applyStoredSettings(next);
      setAPIKey("");
      setAPIKeyAction("keep");
      toast.success(t("page.agent.settings_saved"));
    } catch (error) {
      toast.error(String(error));
    } finally {
      setBusy(false);
    }
  };

  const testProvider = async () => {
    const input = settingsInput();
    if (!input) return;
    setBusy(true);
    try {
      const result = await Agent.TestProvider({
        ...input,
        apiKey: apiKeyAction === "replace" ? apiKey : undefined,
      });
      setTestResult(result);
      toast.success(t("page.agent.connection_ok"));
    } catch (error) {
      setTestResult(undefined);
      toast.error(String(error));
    } finally {
      setBusy(false);
    }
  };

  const selectProvider = (providerID: string) => {
    if (!settings) return;
    const provider = providerOf(catalog, providerID);
    setTestResult(undefined);
    setAPIKey("");
    setAPIKeyAction("keep");
    setHeaders(headerDrafts(settings.headers));
    if (settings.provider === "custom") {
      // Remember what the user typed for the custom provider before the switch replaces it.
      setCustomDraft(settings);
    }
    if (!provider || provider.custom) {
      setSettings(customDraft ?? { ...settings, provider: providerID });
      setAuthMode("key");
      return;
    }
    setAuthMode(provider.credential.kind === "oauth" ? "account" : "key");
    // A provider switch starts from the newest model it serves, never from the previous limits.
    const model = provider.models?.[0];
    const next: AgentSettingsView = {
      ...settings,
      provider: provider.id,
      endpoint: provider.endpoint,
      protocol: model?.protocol ?? provider.defaultProtocol,
      model: model?.id ?? settings.model,
    };
    setSettings(model ? applyModelDefaults(next, model) : next);
  };

  const selectModel = (value: string) => {
    if (!settings) return;
    const model = modelOf(activeProvider, value);
    setSettings(model ? applyModelDefaults(settings, model) : { ...settings, model: value });
  };

  const connectChatGPT = async () => {
    if (!settings) return;
    setBusy(true);
    try {
      setLogin(await Agent.StartProviderLogin(settings.provider));
      await Agent.CompleteProviderLogin();
      const providers = await Agent.ListProviders();
      setCatalog(providers);
      followPlanModels(providers);
      setAuthMode("account");
      toast.success(t("page.agent.login_complete"));
    } catch (error) {
      toast.error(String(error));
    } finally {
      setLogin(undefined);
      setBusy(false);
    }
  };

  const cancelLogin = async () => {
    try {
      await Agent.CancelProviderLogin();
    } catch (error) {
      toast.error(String(error));
    }
  };

  const signOut = async () => {
    if (!settings) return;
    setBusy(true);
    try {
      const provider = await Agent.SignOutProvider(settings.provider);
      setCatalog((current) =>
        current
          ? {
              ...current,
              providers: (current.providers ?? []).map((entry) =>
                entry.id === provider.id ? provider : entry,
              ),
            }
          : current,
      );
      applyStoredSettings(await Agent.GetSettings());
    } catch (error) {
      toast.error(String(error));
    } finally {
      setBusy(false);
    }
  };

  const refreshCatalog = async () => {
    setBusy(true);
    try {
      const providers = await Agent.RefreshProviderCatalog();
      setCatalog(providers);
      followPlanModels(providers);
      toast.success(t("page.agent.catalog_refreshed"));
    } catch (error) {
      toast.error(String(error));
    } finally {
      setBusy(false);
    }
  };

  const editServer = (server: MCPServerView) => {
    setMCPDraft({
      id: server.id,
      name: server.name,
      transport: server.transport,
      executable: server.executable,
      arguments: server.arguments,
      workingDirectory: server.workingDirectory,
      endpoint: server.endpoint,
      enabled: server.enabled,
      entries: server.entries?.map((entry) => ({
        name: entry.name,
        value: entry.value,
        secret: entry.secret,
        secretAction: entry.secret ? "keep" : undefined,
      })),
    });
  };

  const saveServer = async () => {
    if (!mcpDraft) return;
    try {
      await Agent.UpsertMCPServer(mcpDraft);
      setMCPDraft(undefined);
      setServers((await Agent.ListMCPServers()) ?? []);
    } catch (error) {
      toast.error(String(error));
    }
  };

  const testServer = async (id: string) => {
    try {
      const result = await Agent.TestMCPServer(id);
      if (result.error) {
        toast.error(result.error);
        return;
      }
      toast.success(t("page.agent.mcp_connected", { count: result.toolCount ?? 0 }));
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  };

  if (!settings) {
    return <Loader2Icon className="mx-auto mt-16 size-5 animate-spin" />;
  }

  const isBuiltin = !activeProvider?.custom && settings.provider !== "custom";
  const selectedModel = modelOf(activeProvider, settings.model);
  // A built-in model declares the efforts its endpoint accepts; the custom provider has no catalog
  // entry and offers every effort the adapter can send. A stored effort stays listed while its
  // model does not declare it, so the form never hides the current value.
  const reasoningOptions = isBuiltin
    ? reasoningOrder.filter(
        (value) =>
          value === "auto" ||
          value === settings.reasoning ||
          (selectedModel?.reasoning ?? []).includes(value),
      )
    : reasoningOrder;
  // A built-in model that declares no effort leaves nothing to adjust.
  const showReasoning = !isBuiltin || reasoningOptions.length > 1;
  const credential = activeProvider?.credential ?? settings.credential;
  const connected = credential.kind !== "none";
  const providerOptions = (catalog?.providers ?? []).map((provider) => {
    const name = provider.custom ? t("page.agent.provider_custom") : provider.name;
    const linked = (provider.credential?.kind ?? "none") !== "none";
    return {
      value: provider.id,
      label: linked ? `${name} · ${t("page.agent.provider_connected")}` : name,
    };
  });
  const keyActionOptions = [
    { value: "keep", label: connected ? t("page.agent.secret_configured") : t("page.agent.keep") },
    { value: "replace", label: t("page.agent.replace") },
    ...(credential.kind === "oauth" ? [] : [{ value: "clear", label: t("page.agent.clear") }]),
  ];

  return (
    <div className="space-y-6 p-4 pb-12">
      <SettingsSection title={t("page.agent.provider")} description={t("page.agent.provider_hint")}>
        <Field label={t("page.agent.provider_select")}>
          <Select
            value={settings.provider}
            items={providerOptions}
            onValueChange={(value) => {
              // Base UI reports a repeat pick of the current option, which must not reset the form.
              if (value === null || value === settings.provider) return;
              selectProvider(value);
            }}
          >
            <SelectTrigger className="w-full" aria-label={t("page.agent.provider_select")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {providerOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        {!settings.provider && <p className="text-[11px] text-muted-foreground">…</p>}
        <Field label={t("page.agent.model")}>
          <Input
            list={isBuiltin ? "agent-model-options" : undefined}
            aria-label={t("page.agent.model")}
            value={settings.model}
            autoComplete="off"
            onChange={(event) => selectModel(event.target.value)}
          />
          {isBuiltin && (
            <datalist id="agent-model-options">
              {(activeProvider?.models ?? []).map((model) => (
                <option key={model.id} value={model.id}>
                  {model.name}
                </option>
              ))}
            </datalist>
          )}
        </Field>
        <div className="flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
          <span>{t("page.agent.model_hint")}</span>
          {catalog?.catalog.updatedAt && (
            <span className="flex items-center gap-1">
              {t("page.agent.catalog_updated", {
                date: catalog.catalog.updatedAt.slice(0, 10),
              })}
              <Button
                variant="ghost"
                size="icon"
                disabled={busy}
                title={t("page.agent.catalog_refresh")}
                onClick={() => void refreshCatalog()}
              >
                <RefreshCwIcon className="size-3" />
              </Button>
            </span>
          )}
        </div>
        {/* A built-in provider derives the protocol, endpoint, limits, and image support from the
            catalog model, so only the custom provider exposes them. */}
        {!isBuiltin && (
          <>
            <div className="grid grid-cols-2 gap-3">
              <Field label={t("page.agent.protocol")}>
                <Select
                  value={settings.protocol}
                  items={protocolOptions}
                  onValueChange={(value) => {
                    if (value === null) return;
                    setSettings({ ...settings, protocol: value });
                  }}
                >
                  <SelectTrigger className="w-full" aria-label={t("page.agent.protocol")}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {protocolOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
              <Field label={t("page.agent.endpoint")}>
                <Input
                  value={settings.endpoint}
                  aria-label={t("page.agent.endpoint")}
                  onChange={(event) => setSettings({ ...settings, endpoint: event.target.value })}
                />
              </Field>
            </div>
            <div className="space-y-1">
              <label className="flex items-center gap-2 text-xs font-medium">
                <input
                  type="checkbox"
                  checked={settings.supportsImages}
                  onChange={(event) =>
                    setSettings({ ...settings, supportsImages: event.target.checked })
                  }
                />
                {t("page.agent.supports_images")}
              </label>
              <p className="text-[11px] text-muted-foreground">
                {t("page.agent.supports_images_hint")}
              </p>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <Field label={t("page.agent.context_window")}>
                <Input
                  type="number"
                  value={settings.contextWindowSize}
                  onChange={(event) =>
                    setSettings({ ...settings, contextWindowSize: Number(event.target.value) })
                  }
                />
              </Field>
              <Field label={t("page.agent.max_output")}>
                <Input
                  type="number"
                  value={settings.maxOutputTokens}
                  onChange={(event) =>
                    setSettings({ ...settings, maxOutputTokens: Number(event.target.value) })
                  }
                />
              </Field>
            </div>
          </>
        )}
        {showReasoning && (
          <Field label={t("page.agent.reasoning")}>
            <Select
              value={settings.reasoning}
              onValueChange={(value) => {
                if (value === null) return;
                setSettings({ ...settings, reasoning: value });
              }}
            >
              <SelectTrigger className="w-full" aria-label={t("page.agent.reasoning")}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {reasoningOptions.map((value) => (
                    <SelectItem key={value} value={value}>
                      {value}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
        )}
        <div className="space-y-1.5">
          <span className="text-xs font-medium">{t("page.agent.api_key")}</span>
          <div className="space-y-2">
            {activeProvider?.supportsOAuth && (
              <div className="flex gap-3 text-xs">
                <label className="flex items-center gap-1.5">
                  <input
                    type="radio"
                    checked={authMode === "account"}
                    aria-label={t("page.agent.auth_account")}
                    onChange={() => setAuthMode("account")}
                  />
                  {t("page.agent.auth_account")}
                </label>
                <label className="flex items-center gap-1.5">
                  <input
                    type="radio"
                    checked={authMode === "key"}
                    aria-label={t("page.agent.auth_key")}
                    onChange={() => setAuthMode("key")}
                  />
                  {t("page.agent.auth_key")}
                </label>
              </div>
            )}
            {activeProvider?.supportsOAuth && authMode === "account" ? (
              credential.kind === "oauth" ? (
                <div className="flex items-center justify-between gap-2 rounded-lg border p-2 text-xs">
                  <span className="min-w-0 truncate">
                    {credential.accountLabel || t("page.agent.provider_connected")}
                  </span>
                  <Button variant="ghost" size="sm" disabled={busy} onClick={() => void signOut()}>
                    <LogOutIcon className="size-3" /> {t("page.agent.sign_out")}
                  </Button>
                </div>
              ) : (
                <div className="space-y-2">
                  <Button
                    variant="outline"
                    disabled={busy || Boolean(login)}
                    onClick={() => void connectChatGPT()}
                  >
                    {t("page.agent.connect_chatgpt")}
                  </Button>
                  {login && (
                    <div className="space-y-1 text-[11px] text-muted-foreground">
                      <p>{t("page.agent.login_waiting")}</p>
                      <a className="break-all underline" href={login.url}>
                        {login.url}
                      </a>
                      <div>
                        <Button variant="ghost" size="sm" onClick={() => void cancelLogin()}>
                          {t("page.agent.login_cancel")}
                        </Button>
                      </div>
                    </div>
                  )}
                </div>
              )
            ) : (
              <>
                <Select
                  value={apiKeyAction}
                  items={keyActionOptions}
                  onValueChange={(value) => {
                    if (value === null) return;
                    setAPIKeyAction(value);
                  }}
                >
                  <SelectTrigger className="w-full" aria-label={t("page.agent.api_key")}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {keyActionOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                {apiKeyAction === "replace" && (
                  <Input
                    type="password"
                    value={apiKey}
                    autoComplete="off"
                    aria-label={t("page.agent.api_key_value")}
                    onChange={(event) => setAPIKey(event.target.value)}
                  />
                )}
              </>
            )}
          </div>
        </div>
        {!isBuiltin && (
          <div className="space-y-1.5">
            <span className="text-xs font-medium">{t("page.agent.custom_headers")}</span>
            <p className="text-[11px] text-muted-foreground">
              {t("page.agent.custom_headers_hint")}
            </p>
            <div className="space-y-2 pt-1">
              {headers.map((header, index) => (
                <div key={index} className="grid grid-cols-[1fr_1fr_auto_auto] items-center gap-2">
                  <Input
                    placeholder="Header"
                    value={header.name}
                    autoComplete="off"
                    onChange={(event) =>
                      setHeaders((values) =>
                        values.map((value, entryIndex) =>
                          entryIndex === index ? { ...value, name: event.target.value } : value,
                        ),
                      )
                    }
                  />
                  <Input
                    type={header.secret ? "password" : "text"}
                    autoComplete="off"
                    placeholder={
                      header.secret && header.configured && header.secretAction === "keep"
                        ? t("page.agent.configured")
                        : "Value"
                    }
                    value={header.value}
                    onChange={(event) =>
                      setHeaders((values) =>
                        values.map((value, entryIndex) =>
                          entryIndex === index
                            ? {
                                ...value,
                                value: event.target.value,
                                secretAction: value.secret ? "replace" : undefined,
                              }
                            : value,
                        ),
                      )
                    }
                  />
                  <label className="flex items-center gap-1 text-xs">
                    <input
                      type="checkbox"
                      checked={header.secret}
                      onChange={(event) =>
                        setHeaders((values) =>
                          values.map((value, entryIndex) =>
                            entryIndex === index
                              ? {
                                  ...value,
                                  secret: event.target.checked,
                                  secretAction: event.target.checked ? "keep" : undefined,
                                  value: "",
                                }
                              : value,
                          ),
                        )
                      }
                    />{" "}
                    {t("page.agent.secret")}
                  </label>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() =>
                      setHeaders((values) => values.filter((_, entryIndex) => entryIndex !== index))
                    }
                  >
                    <Trash2Icon className="size-3" />
                  </Button>
                </div>
              ))}
              <Button
                variant="ghost"
                size="sm"
                onClick={() =>
                  setHeaders((values) => [
                    ...values,
                    { name: "", value: "", secret: false, configured: false },
                  ])
                }
              >
                <PlusIcon className="size-3" /> {t("page.agent.add_header")}
              </Button>
            </div>
          </div>
        )}
        {testResult && (
          <p className="text-[11px] text-muted-foreground" role="status">
            {t("page.agent.test_result", {
              model: testResult.model,
              latency: testResult.latencyMs,
            })}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <Button variant="outline" disabled={busy} onClick={() => void testProvider()}>
            {t("page.agent.test_connection")}
          </Button>
          <Button disabled={busy} onClick={() => void saveSettings()}>
            <SaveIcon className="size-4" /> {t("g.save")}
          </Button>
        </div>
      </SettingsSection>

      <SettingsSection title="MCP" description={t("page.agent.mcp_warning")}>
        <div className="space-y-2">
          {servers.map((server) => (
            <div key={server.id} className="flex items-center gap-2 rounded-lg border p-2">
              <ServerIcon className="size-4 text-muted-foreground" />
              <button className="min-w-0 flex-1 text-left" onClick={() => editServer(server)}>
                <div className="truncate text-sm font-medium">{server.name}</div>
                <div className="text-[11px] text-muted-foreground">
                  {server.transport} ·{" "}
                  {server.enabled ? t("page.agent.enabled") : t("page.agent.disabled")}
                </div>
              </button>
              <Button variant="ghost" size="sm" onClickPromise={() => testServer(server.id)}>
                {t("page.agent.test_connection")}
              </Button>
              <Button
                variant="ghost"
                size="icon"
                onClick={() =>
                  void Agent.DeleteMCPServer(server.id).then(() =>
                    setServers((values) => values.filter((value) => value.id !== server.id)),
                  )
                }
              >
                <Trash2Icon className="size-4" />
              </Button>
            </div>
          ))}
          <Button variant="outline" onClick={() => setMCPDraft({ ...emptyMCP })}>
            <PlusIcon className="size-4" /> {t("page.agent.add_server")}
          </Button>
        </div>
        {mcpDraft && (
          <MCPEditor
            value={mcpDraft}
            onChange={setMCPDraft}
            onCancel={() => setMCPDraft(undefined)}
            onSave={() => void saveServer()}
          />
        )}
      </SettingsSection>

      <SettingsSection title={t("page.agent.skills")} description={t("page.agent.skills_hint")}>
        <div className="space-y-2">
          {skills.map((skill) => (
            <div key={`${skill.source}-${skill.name}`} className="rounded-lg border p-3">
              <div className="flex items-center justify-between text-sm font-medium">
                {skill.name}
                <span className="text-[10px] text-muted-foreground">{skill.source}</span>
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                {skill.error || skill.description}
              </p>
            </div>
          ))}
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            onClick={() => void Agent.ReloadSkills().then((value) => setSkills(value ?? []))}
          >
            <RefreshCwIcon className="size-4" /> {t("page.agent.reload")}
          </Button>
          <Button variant="outline" onClick={() => void Agent.OpenUserSkillsFolder()}>
            <FolderOpenIcon className="size-4" /> {t("page.agent.open_skills_folder")}
          </Button>
        </div>
      </SettingsSection>
    </div>
  );
}

function MCPEditor({
  value,
  onChange,
  onCancel,
  onSave,
}: {
  value: MCPServerInput;
  onChange: (value: MCPServerInput) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const { t } = useTranslation();
  const transportOptions = [
    { value: "stdio", label: "stdio" },
    { value: "streamable-http", label: "Streamable HTTP" },
    { value: "blender", label: t("page.agent.transport_blender") },
  ];
  const entries = value.entries ?? [];
  const updateEntry = (index: number, patch: Partial<MCPEntryInput>) =>
    onChange({
      ...value,
      entries: entries.map((entry, entryIndex) =>
        entryIndex === index ? { ...entry, ...patch } : entry,
      ),
    });
  return (
    <div className="space-y-3 rounded-lg border bg-muted/20 p-3">
      <div className="grid grid-cols-2 gap-2">
        <Input
          placeholder="Name"
          value={value.name}
          onChange={(event) => onChange({ ...value, name: event.target.value })}
        />
        <Select
          value={value.transport}
          items={transportOptions}
          onValueChange={(transport) => {
            if (transport === null) return;
            onChange({ ...value, transport });
          }}
        >
          <SelectTrigger className="w-full" aria-label="Transport">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {transportOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>
      {value.transport === "stdio" ? (
        <>
          <Input
            placeholder="Executable"
            value={value.executable}
            onChange={(event) => onChange({ ...value, executable: event.target.value })}
          />
          <Input
            placeholder="Arguments (one per line)"
            value={(value.arguments ?? []).join("\n")}
            onChange={(event) =>
              onChange({ ...value, arguments: event.target.value.split("\n").filter(Boolean) })
            }
          />
          <Input
            placeholder="Working directory"
            value={value.workingDirectory}
            onChange={(event) => onChange({ ...value, workingDirectory: event.target.value })}
          />
        </>
      ) : value.transport === "blender" ? (
        <p className="text-[11px] text-muted-foreground">{t("page.agent.blender_hint")}</p>
      ) : (
        <Input
          placeholder="https://…"
          value={value.endpoint}
          onChange={(event) => onChange({ ...value, endpoint: event.target.value })}
        />
      )}
      <div className="space-y-2">
        {entries.map((entry, index) => (
          <div key={index} className="grid grid-cols-[1fr_1fr_auto_auto] gap-2">
            <Input
              placeholder="Name"
              value={entry.name}
              onChange={(event) => updateEntry(index, { name: event.target.value })}
            />
            <Input
              type={entry.secret ? "password" : "text"}
              placeholder={entry.secret ? "Secret value" : "Value"}
              value={entry.value ?? ""}
              onChange={(event) =>
                updateEntry(index, {
                  value: event.target.value,
                  secretAction: entry.secret ? "replace" : undefined,
                })
              }
            />
            <label className="flex items-center gap-1 text-xs">
              <input
                type="checkbox"
                checked={entry.secret}
                onChange={(event) =>
                  updateEntry(index, {
                    secret: event.target.checked,
                    secretAction: event.target.checked ? "keep" : undefined,
                  })
                }
              />{" "}
              Secret
            </label>
            <Button
              variant="ghost"
              size="icon"
              onClick={() =>
                onChange({
                  ...value,
                  entries: entries.filter((_, entryIndex) => entryIndex !== index),
                })
              }
            >
              <Trash2Icon className="size-3" />
            </Button>
          </div>
        ))}
        <Button
          variant="ghost"
          size="sm"
          onClick={() =>
            onChange({ ...value, entries: [...entries, { name: "", value: "", secret: false }] })
          }
        >
          <PlusIcon className="size-3" /> Entry
        </Button>
      </div>
      <label className="flex items-center justify-between text-sm">
        Enabled{" "}
        <Switch
          checked={value.enabled}
          onCheckedChange={(enabled) => onChange({ ...value, enabled })}
        />
      </label>
      <div className="flex justify-end gap-2">
        <Button variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button onClick={onSave}>Save</Button>
      </div>
    </div>
  );
}

function SettingsSection({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <section className="space-y-4 rounded-xl border bg-card p-4">
      <div>
        <h2 className="text-sm font-semibold">{title}</h2>
        <p className="mt-1 text-xs text-muted-foreground">{description}</p>
      </div>
      {children}
    </section>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium">{label}</span>
      {children}
    </label>
  );
}
