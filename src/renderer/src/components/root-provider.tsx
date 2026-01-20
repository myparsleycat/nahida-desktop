import { ThemeProvider } from "./theme-provider";

export function RootProvider({ children }: { children: React.ReactNode }) {
  return <ThemeProvider>{children}</ThemeProvider>;
}
