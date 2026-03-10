import { ThemeProvider } from "./theme-provider";
import { useInitializeAuth } from "@renderer/hooks/use-auth";

export function RootProvider({ children }: { children: React.ReactNode }) {
  useInitializeAuth();

  return <ThemeProvider>{children}</ThemeProvider>;
}
