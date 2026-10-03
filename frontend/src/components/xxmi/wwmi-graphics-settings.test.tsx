// @vitest-environment jsdom

import type { WWMIOptions } from "@bindings/xxmi/models";
import { WWMIGraphicsSettings } from "@renderer/components/xxmi/wwmi-graphics-settings";
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

it("preserves WWMI values while editing graphics settings", () => {
  const changed = vi.fn();
  const initial: WWMIOptions = {
    unlockFPS: false,
    forceMaxLODBias: false,
    disableWoundedFX: false,
    meshLODDistanceBaseFOV: 165,
    resourceTier: "HD",
    resourceTierDecided: false,
  };

  function Settings() {
    const [options, setOptions] = useState(initial);
    return (
      <WWMIGraphicsSettings
        options={options}
        onChange={(next) => {
          changed(next);
          setOptions(next);
        }}
      />
    );
  }

  render(<Settings />);
  fireEvent.change(screen.getByLabelText("page.setting.xxmi.builtin.meshLODDistanceBaseFOV"), {
    target: { value: "170" },
  });

  expect(changed).toHaveBeenLastCalledWith({ ...initial, meshLODDistanceBaseFOV: 170 });
});
