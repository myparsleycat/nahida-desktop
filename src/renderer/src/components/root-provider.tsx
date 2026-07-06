import { TooltipProvider } from "@renderer/components/ui/tooltip";
import { useInitializeAuth } from "@renderer/hooks/use-auth";
import { ThemeProvider } from "./theme-provider";

export function RootProvider({ children }: { children: React.ReactNode }) {
  useInitializeAuth();

  return (
    <ThemeProvider>
      <TooltipProvider>{children}</TooltipProvider>
    </ThemeProvider>
  );
}
