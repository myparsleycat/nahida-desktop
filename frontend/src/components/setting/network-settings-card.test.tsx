// @vitest-environment jsdom
import { Window as AppWindow } from "@bindings/app";
import { Setting, type ProxySettings } from "@bindings/setting";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { NetworkSettingsCard } from "./network-settings-card";

vi.mock("@bindings/app", () => ({
  Window: { Restart: vi.fn() },
}));
vi.mock("@bindings/setting", () => ({
  Setting: { GetProxySettings: vi.fn(), SetProxySettings: vi.fn() },
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

const initial: ProxySettings = {
  enabled: true,
  type: "http",
  host: "localhost",
  port: 8080,
  username: "user",
  hasPassword: true,
  restartRequired: false,
  configurationInvalid: false,
};

beforeEach(() => {
  vi.mocked(Setting.GetProxySettings).mockResolvedValue(initial);
  vi.mocked(Setting.SetProxySettings).mockResolvedValue(undefined);
  vi.mocked(AppWindow.Restart).mockResolvedValue(undefined);
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

async function openSettings() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <NetworkSettingsCard />
    </QueryClientProvider>,
  );
  await screen.findByLabelText("page.setting.network.host");
}

it("keeps the saved password and saves only after clicking save", async () => {
  await openSettings();
  fireEvent.change(screen.getByLabelText("page.setting.network.host"), {
    target: { value: "proxy.example" },
  });
  expect(Setting.SetProxySettings).not.toHaveBeenCalled();
  vi.mocked(Setting.GetProxySettings).mockResolvedValue({
    ...initial,
    host: "proxy.example",
    restartRequired: true,
  });
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.save" }));
  await waitFor(() =>
    expect(Setting.SetProxySettings).toHaveBeenCalledWith(
      expect.objectContaining({ host: "proxy.example", password: "", passwordAction: "keep" }),
    ),
  );
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  expect(screen.getByRole("status", { hidden: true })).toHaveProperty(
    "textContent",
    "page.setting.network.restart",
  );
  expect(
    screen.getByRole("button", { name: "page.setting.network.restartDialog.action" }),
  ).toBeTruthy();
});

it("keeps the restart banner when the restart dialog is cancelled", async () => {
  await openSettings();
  vi.mocked(Setting.GetProxySettings).mockResolvedValue({ ...initial, restartRequired: true });
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.save" }));
  await screen.findByRole("alertdialog");
  fireEvent.click(screen.getByRole("button", { name: "g.cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(screen.getByRole("status")).toHaveProperty("textContent", "page.setting.network.restart");
  expect(AppWindow.Restart).not.toHaveBeenCalled();
});

it("restarts the app from the saved proxy dialog", async () => {
  await openSettings();
  vi.mocked(Setting.GetProxySettings).mockResolvedValue({ ...initial, restartRequired: true });
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.save" }));
  await screen.findByRole("alertdialog");
  fireEvent.click(
    screen.getByRole("button", { name: "page.setting.network.restartDialog.action" }),
  );
  await waitFor(() => expect(AppWindow.Restart).toHaveBeenCalledOnce());
});

it("keeps the restart dialog open when restart fails", async () => {
  await openSettings();
  vi.mocked(Setting.GetProxySettings).mockResolvedValue({ ...initial, restartRequired: true });
  vi.mocked(AppWindow.Restart).mockRejectedValue(new Error("restart.failed"));
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.save" }));
  await screen.findByRole("alertdialog");
  fireEvent.click(
    screen.getByRole("button", { name: "page.setting.network.restartDialog.action" }),
  );
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    "page.setting.network.restartDialog.failed",
  );
  expect(screen.getByRole("alertdialog")).toBeTruthy();
});

it("does not prompt to restart when the saved proxy is already active", async () => {
  await openSettings();
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.save" }));
  await waitFor(() => expect(Setting.GetProxySettings).toHaveBeenCalledTimes(2));
  expect(screen.queryByRole("alertdialog")).toBeNull();
});

it("replaces and clears credentials explicitly", async () => {
  await openSettings();
  const password = screen.getByLabelText("page.setting.network.password");
  expect(password.getAttribute("type")).toBe("password");
  fireEvent.change(password, { target: { value: "replacement" } });
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.save" }));
  await waitFor(() =>
    expect(Setting.SetProxySettings).toHaveBeenCalledWith(
      expect.objectContaining({ password: "replacement", passwordAction: "replace" }),
    ),
  );
  await waitFor(() => expect(password).toHaveProperty("value", ""));
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.clearPassword" }));
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.save" }));
  await waitFor(() =>
    expect(Setting.SetProxySettings).toHaveBeenLastCalledWith(
      expect.objectContaining({ password: "", passwordAction: "clear" }),
    ),
  );
});

it("rejects an invalid host before saving", async () => {
  await openSettings();
  fireEvent.change(screen.getByLabelText("page.setting.network.host"), {
    target: { value: "http://proxy.example" },
  });
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.save" }));
  expect(Setting.SetProxySettings).not.toHaveBeenCalled();
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    "page.setting.network.errors.host",
  );
});

it("skips host checks when the proxy is disabled", async () => {
  await openSettings();
  fireEvent.click(screen.getByRole("switch"));
  fireEvent.change(screen.getByLabelText("page.setting.network.host"), {
    target: { value: "http://proxy.example" },
  });
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.save" }));
  await waitFor(() =>
    expect(Setting.SetProxySettings).toHaveBeenCalledWith(
      expect.objectContaining({ enabled: false, host: "http://proxy.example" }),
    ),
  );
});

it("keeps the form on backend validation failure and shows a field error", async () => {
  await openSettings();
  vi.mocked(Setting.SetProxySettings).mockRejectedValue(new Error("proxy.credentials"));
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.save" }));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    "page.setting.network.errors.credentials",
  );
  expect(screen.getByLabelText("page.setting.network.host")).toHaveProperty("value", "localhost");
});

it("retries after GetProxySettings fails once", async () => {
  vi.mocked(Setting.GetProxySettings)
    .mockRejectedValueOnce(new Error("load failed"))
    .mockResolvedValue(initial);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <NetworkSettingsCard />
    </QueryClientProvider>,
  );
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("page.setting.network.loadFailed"),
  );
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.retry" }));
  await screen.findByLabelText("page.setting.network.host");
});

it("allows clearing a damaged configuration", async () => {
  vi.mocked(Setting.GetProxySettings).mockResolvedValue({
    ...initial,
    enabled: false,
    configurationInvalid: true,
    hasPassword: false,
  });
  await openSettings();
  expect(screen.getByRole("alert")).toHaveProperty("textContent", "page.setting.network.invalid");
  fireEvent.click(screen.getByRole("button", { name: "page.setting.network.save" }));
  await waitFor(() =>
    expect(Setting.SetProxySettings).toHaveBeenCalledWith(
      expect.objectContaining({ enabled: false, passwordAction: "clear" }),
    ),
  );
});
