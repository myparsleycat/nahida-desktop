// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const calls = vi.hoisted(() => ({
  navigate: vi.fn(),
  invalidate: vi.fn(),
}));

vi.mock("@bindings/platform", () => ({ Shell: { OpenExternal: vi.fn() } }));
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => calls.navigate,
  useLocation: () => ({ pathname: "/drive/drive/root-id" }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: calls.invalidate }),
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

afterEach(() => {
  cleanup();
  calls.navigate.mockClear();
  calls.invalidate.mockClear();
});

it("retries drive queries and navigates when Drive is clicked while offline", () => {
  render(<Sidebar />);
  fireEvent.click(screen.getByRole("button", { name: "page.drive.title_server_error" }));
  expect(calls.invalidate).toHaveBeenCalledWith({ queryKey: ["drive"] });
  expect(calls.navigate).toHaveBeenCalledWith({
    to: "/drive/drive/$id",
    params: { id: "last-drive" },
  });
});

it("retries drive queries and navigates when Share is clicked while offline", () => {
  render(<Sidebar />);
  fireEvent.click(screen.getByRole("button", { name: "page.share_drive.title_server_error" }));
  expect(calls.invalidate).toHaveBeenCalledWith({ queryKey: ["drive"] });
  expect(calls.navigate).toHaveBeenCalledWith({
    to: "/drive/share/$id",
    params: { id: "last-share" },
  });
});
