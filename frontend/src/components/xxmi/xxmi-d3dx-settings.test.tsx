// @vitest-environment jsdom

import { D3DXOptionManaged } from "@bindings/xxmi/models";
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
  const input = await renderSettings();
  fireEvent.change(screen.getByLabelText("g.search"), { target: { value: "120" } });
  act(() => input.focus());
  fireEvent.change(input, { target: { value: "" } });
  expect(input.isConnected).toBe(true);

  act(() => input.blur());
  expect(input.isConnected).toBe(false);
  expect(screen.getByText("page.setting.xxmi.builtin.d3dx.noMatch")).toBeTruthy();
});

async function renderSettings() {
  function Settings() {
    const [edits, setEdits] = useState<Record<string, string>>({});
    return (
      <XXMID3DXSettings
        importer="GIMI"
        enforceRendering={false}
        edits={edits}
        onEditsChange={setEdits}
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
