// @vitest-environment jsdom

import {
  ImportUserDataMode,
  ImportVersionMode,
  type ImportPackageVersion,
} from "@bindings/xxmi/models";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const backend = vi.hoisted(() => ({ preview: vi.fn() }));
vi.mock("@bindings/xxmi", () => ({
  XXMI: { PreviewExternalLauncherImport: backend.preview },
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

import { XXMIImportDialog } from "./xxmi-import-dialog";

const versions: ImportPackageVersion[] = [
  {
    package: "importer:GIMI",
    installedVersion: "1.2.3",
    latestVersion: "1.3.0",
    latestNotes: "",
    updateAvailable: true,
    checkFailed: false,
  },
  {
    package: "xxmi-libs",
    installedVersion: "1.7.6",
    latestVersion: "1.8.0",
    latestNotes: "",
    updateAvailable: true,
    checkFailed: false,
  },
];
const clients: QueryClient[] = [];

beforeEach(() => {
  backend.preview.mockReset().mockResolvedValue(versions);
});

afterEach(() => {
  cleanup();
  clients.forEach((client) => client.clear());
  clients.length = 0;
});

it("waits for a version preview before allowing import", () => {
  backend.preview.mockReturnValue(new Promise(() => {}));
  renderImport();

  expect(screen.getByRole("status").textContent).toContain("importCheckingVersions");
  expect(screen.getByRole("button", { name: "page.setting.xxmi.builtin.import" })).toHaveProperty(
    "disabled",
    true,
  );
});

it("imports the displayed latest versions with the selected user data mode", async () => {
  const { onImport } = renderImport();
  await screen.findByText("1.3.0");

  fireEvent.click(
    screen.getByRole("radio", { name: "page.setting.xxmi.builtin.importUpdateLatest" }),
  );
  fireEvent.click(screen.getByRole("radio", { name: /importUserDataMove/ }));
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.import" }));

  await waitFor(() =>
    expect(onImport).toHaveBeenCalledWith({
      path: "C:\\External XXMI",
      root: "C:\\Builtin XXMI",
      userData: ImportUserDataMode.ImportUserDataMove,
      versionMode: ImportVersionMode.ImportVersionLatest,
      versions,
    }),
  );
});

it("allows the legacy import path when the preview has no packages", async () => {
  backend.preview.mockResolvedValue([]);
  const { onImport } = renderImport();
  const confirm = screen.getByRole("button", { name: "page.setting.xxmi.builtin.import" });
  await waitFor(() => expect(confirm).toHaveProperty("disabled", false));

  fireEvent.click(confirm);
  await waitFor(() =>
    expect(onImport).toHaveBeenCalledWith({
      path: "C:\\External XXMI",
      root: "C:\\Builtin XXMI",
      userData: ImportUserDataMode.ImportUserDataKeep,
      versionMode: ImportVersionMode.ImportVersionPinned,
      versions: [],
    }),
  );
});

it("blocks pinning when a preview package has no installed version", async () => {
  backend.preview.mockResolvedValue(
    versions.map((version) => ({ ...version, installedVersion: "" })),
  );
  const { onImport } = renderImport();
  await screen.findByText("1.3.0");

  expect(
    screen.getByRole("radio", { name: "page.setting.xxmi.builtin.importPinInstalled" }),
  ).toHaveProperty("disabled", true);
  const confirm = screen.getByRole("button", { name: "page.setting.xxmi.builtin.import" });
  expect(confirm).toHaveProperty("disabled", true);
  fireEvent.click(confirm);
  expect(onImport).not.toHaveBeenCalled();
});

it("defaults to pinning installed versions when updates are available", async () => {
  const { onImport } = renderImport();
  await screen.findByText("1.8.0");

  expect(
    screen.getByRole("radio", { name: "page.setting.xxmi.builtin.importPinInstalled" }),
  ).toHaveProperty("checked", true);
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.import" }));

  await waitFor(() =>
    expect(onImport).toHaveBeenCalledWith(
      expect.objectContaining({
        userData: ImportUserDataMode.ImportUserDataKeep,
        versionMode: ImportVersionMode.ImportVersionPinned,
        versions,
      }),
    ),
  );
});

it("allows pinning after a partial release check failure and offers retry", async () => {
  const partial = versions.map((version) =>
    version.package === "xxmi-libs"
      ? { ...version, latestVersion: "", updateAvailable: false, checkFailed: true }
      : version,
  );
  backend.preview.mockResolvedValueOnce(partial);
  const { onImport } = renderImport();
  await screen.findByText("page.setting.xxmi.builtin.importVersionCheckFailed");

  expect(
    screen.getByRole("radio", { name: "page.setting.xxmi.builtin.importUpdateLatest" }),
  ).toHaveProperty("disabled", true);
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.import" }));
  await waitFor(() =>
    expect(onImport).toHaveBeenCalledWith(
      expect.objectContaining({
        versionMode: ImportVersionMode.ImportVersionPinned,
        versions: partial,
      }),
    ),
  );

  fireEvent.click(
    screen.getByRole("button", { name: "page.setting.xxmi.builtin.importRetryVersions" }),
  );
  await screen.findByText("1.8.0");
  expect(backend.preview).toHaveBeenCalledTimes(2);
  expect(
    screen.getByRole("radio", { name: "page.setting.xxmi.builtin.importUpdateLatest" }),
  ).toHaveProperty("disabled", false);
});

it("blocks import when reading the source fails and recovers after retry", async () => {
  backend.preview.mockRejectedValueOnce(new Error("Source unavailable"));
  renderImport();
  await screen.findByText("Source unavailable");

  expect(screen.getByRole("button", { name: "page.setting.xxmi.builtin.import" })).toHaveProperty(
    "disabled",
    true,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "page.setting.xxmi.builtin.importRetryVersions" }),
  );
  await screen.findByText("1.3.0");
  expect(screen.getByRole("button", { name: "page.setting.xxmi.builtin.import" })).toHaveProperty(
    "disabled",
    false,
  );
});

it("follows latest without asking when the source is already current", async () => {
  backend.preview.mockResolvedValue(
    versions.map((version) => ({
      ...version,
      installedVersion: version.latestVersion,
      updateAvailable: false,
    })),
  );
  const { onImport } = renderImport();
  await screen.findByRole("table");

  expect(
    screen.queryByRole("radio", { name: "page.setting.xxmi.builtin.importUpdateLatest" }),
  ).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.import" }));
  await waitFor(() =>
    expect(onImport).toHaveBeenCalledWith(
      expect.objectContaining({
        versionMode: ImportVersionMode.ImportVersionLatest,
      }),
    ),
  );
});

it("blocks cancellation and duplicate submissions while importing", async () => {
  const onImport = vi.fn().mockReturnValue(new Promise(() => {}));
  const { onClose } = renderImport(onImport);
  await screen.findByText("1.3.0");

  const confirm = screen.getByRole("button", { name: "page.setting.xxmi.builtin.import" });
  fireEvent.click(confirm);
  expect(screen.getByRole("button", { name: "g.cancel" })).toHaveProperty("disabled", true);
  fireEvent.click(confirm);
  fireEvent.click(screen.getByRole("button", { name: "g.cancel" }));
  expect(onImport).toHaveBeenCalledTimes(1);
  expect(onClose).not.toHaveBeenCalled();
});

function renderImport(onImport = vi.fn().mockResolvedValue(undefined)) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  clients.push(client);
  const onClose = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <XXMIImportDialog
        path={"C:\\External XXMI"}
        root={"C:\\Builtin XXMI"}
        onImport={onImport}
        onClose={onClose}
      />
    </QueryClientProvider>,
  );
  return { onImport, onClose };
}
