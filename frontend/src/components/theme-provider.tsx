import { useSetting } from "@renderer/hooks/use-settings";
import { Logger } from "@renderer/lib/logger";
import { setSetting } from "@renderer/lib/settings";
import { isRendererStateLoaded, rendererState } from "@renderer/wails/renderer-state";
import type { Theme } from "@shared/settings";
import { useQueryClient } from "@tanstack/react-query";
import { createContext, useCallback, useContext, useEffect, useMemo } from "react";

export type { Theme };

type ThemeProviderState = {
  theme: Theme;
  setTheme: (theme: Theme) => void;
};

const initialState: ThemeProviderState = {
  theme: "system",
  setTheme: () => null,
};

const ThemeProviderContext = createContext<ThemeProviderState>(initialState);

// The theme lived in localStorage before it became a setting.
const LEGACY_STORAGE_KEY = "vite-ui-theme";
// A damaged profile can bring the legacy value back after it was removed, so the database
// records that it was already handled.
const LEGACY_MIGRATED_KEY = "nahida.theme.legacy-migrated";
let legacyChecked = false;

function retireLegacyTheme() {
  if (!isRendererStateLoaded() || localStorage.getItem(LEGACY_STORAGE_KEY) === null) return;
  void rendererState.setItem(LEGACY_MIGRATED_KEY, "1");
  localStorage.removeItem(LEGACY_STORAGE_KEY);
}

// Bound calls are handled concurrently, so saves are sent one at a time to keep the last theme last.
let saves = Promise.resolve();

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const queryClient = useQueryClient();
  const { data, isPending } = useSetting("general.theme");
  const theme = data ?? "system";

  const setTheme = useCallback(
    (next: Theme) => {
      queryClient.setQueryData(["settings", "general.theme"], next);
      saves = saves
        .then(() => setSetting("general.theme", next))
        .then(
          // The legacy value stays until a choice is stored, so a failed migration is retried.
          retireLegacyTheme,
          (error: unknown) =>
            Logger.capture("components/theme-provider.tsx", "Failed to save the theme", error),
        );
    },
    [queryClient],
  );

  useEffect(() => {
    // Without the stored marker there is no telling whether the legacy value was already applied.
    if (isPending || legacyChecked || !isRendererStateLoaded()) return;
    legacyChecked = true;

    const legacy = localStorage.getItem(LEGACY_STORAGE_KEY);
    const pending = rendererState.getItem(LEGACY_MIGRATED_KEY) === null;
    if (pending && (legacy === "light" || legacy === "dark")) setTheme(legacy);
    else retireLegacyTheme();
  }, [isPending, setTheme]);

  useEffect(() => {
    if (isPending) return;
    const root = window.document.documentElement;

    root.classList.remove("light", "dark");

    if (theme === "system") {
      const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
      const handleChange = () => {
        root.classList.remove("light", "dark");
        root.classList.add(mediaQuery.matches ? "dark" : "light");
      };

      handleChange();
      mediaQuery.addEventListener("change", handleChange);
      return () => mediaQuery.removeEventListener("change", handleChange);
    }

    root.classList.add(theme);
    return;
  }, [isPending, theme]);

  const value = useMemo(() => ({ theme, setTheme }), [theme, setTheme]);

  // Rendering before the stored theme is known would paint the wrong theme for a frame.
  if (isPending) return null;

  return <ThemeProviderContext.Provider value={value}>{children}</ThemeProviderContext.Provider>;
}

export const useTheme = () => {
  const context = useContext(ThemeProviderContext);

  if (context === undefined) throw new Error("useTheme must be used within a ThemeProvider");

  return context;
};
