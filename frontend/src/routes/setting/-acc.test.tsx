// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { act, Suspense, type ComponentType } from "react";
import { afterEach, expect, it, vi } from "vitest";

const auth = vi.hoisted(() => ({
  session: null as null,
  sessionInitialized: true,
  hasToken: false,
  isBackendOffline: true,
  startLogin: vi.fn(),
  startLogout: vi.fn(),
  refreshSession: vi.fn().mockResolvedValue(null),
}));

vi.mock("@renderer/hooks/use-auth", () => ({
  useAuth: () => auth,
}));
vi.mock("@bindings/platform", () => ({ Shell: { OpenExternal: vi.fn() } }));
vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  createFileRoute: () => (options: { component: ComponentType }) => ({ options }),
}));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

import { Route } from "./acc";

async function renderAccount() {
  const Component = Route.options.component;
  if (!Component) throw new Error("account route has no component");
  await act(async () => {
    render(
      <Suspense fallback={null}>
        <Component />
      </Suspense>,
    );
  });
}

afterEach(() => {
  cleanup();
  auth.hasToken = false;
  auth.startLogin.mockClear();
  auth.startLogout.mockClear();
  auth.refreshSession.mockClear();
});

it("lets an offline guest retry and log in", async () => {
  await renderAccount();
  fireEvent.click(await screen.findByRole("button", { name: "page.setting.acc.retry" }));
  expect(auth.refreshSession).toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "page.setting.acc.not_logged_in.login" }));
  expect(auth.startLogin).toHaveBeenCalled();
});

it("lets an offline saved session retry and log out", async () => {
  auth.hasToken = true;
  await renderAccount();
  fireEvent.click(await screen.findByRole("button", { name: "page.setting.acc.retry" }));
  expect(auth.refreshSession).toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "page.setting.acc.logout" }));
  expect(auth.startLogout).toHaveBeenCalled();
});
