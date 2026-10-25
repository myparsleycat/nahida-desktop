// @vitest-environment jsdom

import { D3DXOptionManaged, type ImporterConfig } from "@bindings/xxmi/models";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";

vi.mock("@bindings/xxmi", () => ({
  XXMI: {
    GetD3DXOptions: () =>
      Promise.resolve([
        {
          section: "Constants",
          key: "fps_limit",
          value: "120",
          managed: D3DXOptionManaged.D3DXOptionUnmanaged,
        },
      ]),
  },
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

import { XXMID3DXSettings } from "./xxmi-d3dx-settings";

afterEach(cleanup);

it("keeps a row matched by its value while that value is being edited", async () => {
  const input = await renderSettings({});
  fireEvent.change(screen.getByLabelText("g.search"), { target: { value: "120" } });
  act(() => input.focus());
  fireEvent.change(input, { target: { value: "" } });
  expect(input.isConnected).toBe(true);

  act(() => input.blur());
  expect(input.isConnected).toBe(false);
  expect(screen.getByText("page.setting.xxmi.builtin.d3dx.noMatch")).toBeTruthy();
});

it("filters a row again after its override is cleared", async () => {
  const input = await renderSettings({ "Constants.fps_limit": "90" });
  const search = screen.getByLabelText("g.search");
  fireEvent.change(search, { target: { value: "90" } });
  const clear = screen.getByText("page.setting.xxmi.builtin.d3dx.clearOverride");
  act(() => clear.focus());
  fireEvent.click(clear);
  expect(input.isConnected).toBe(true);

  act(() => search.focus());
  fireEvent.change(search, { target: { value: "no-such-option" } });
  expect(input.isConnected).toBe(false);
});

async function renderSettings(d3dxOverrides: Record<string, string>) {
  const initial = {
    migoto: { enforceRendering: false },
    d3dxOverrides,
  } as unknown as ImporterConfig;
  function Settings() {
    const [config, setConfig] = useState(initial);
    return (
      <XXMID3DXSettings
        importer="GIMI"
        config={config}
        savedOverrides={undefined}
        onChange={setConfig}
      />
    );
  }
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <Settings />
    </QueryClientProvider>,
  );
  return screen.findByLabelText("fps_limit");
}
