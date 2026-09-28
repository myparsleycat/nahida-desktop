// @vitest-environment jsdom

import type { WWMIOptions } from "@bindings/xxmi/models";
import { WWMIGraphicsSettings } from "@renderer/components/setting/wwmi-graphics-settings";
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

it("preserves WWMI values while editing graphics and performance settings", () => {
  const changed = vi.fn();
  const initial: WWMIOptions = {
    unlockFPS: false,
    applyPerfTweaks: true,
    perfTweaks: { "r.Streaming.HLODStrategy": 2 },
    forceMaxLODBias: false,
    disableWoundedFX: false,
    meshLODDistanceScale: 1,
    meshLODDistanceBaseFOV: 165,
    meshLODDistanceOffset: -10,
    textureStreamingBoost: 20,
    textureStreamingMinBoost: 0,
    textureStreamingUseAllMips: true,
    textureStreamingPoolSize: 0,
    textureStreamingLimitToVRAM: true,
    textureStreamingFixedPoolSize: true,
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
  fireEvent.change(screen.getByLabelText("page.setting.xxmi.builtin.textureStreamingBoost"), {
    target: { value: "12.5" },
  });
  fireEvent.change(screen.getByLabelText("r.Streaming.HLODStrategy"), {
    target: { value: "3" },
  });

  expect(changed).toHaveBeenLastCalledWith({
    ...initial,
    textureStreamingBoost: 12.5,
    perfTweaks: { "r.Streaming.HLODStrategy": 3 },
  });
});
