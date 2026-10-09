import { XXMI } from "@bindings/xxmi";
import { LauncherMode } from "@bindings/xxmi/models";
import { Button } from "@renderer/components/ui/button";
import { ButtonGroup } from "@renderer/components/ui/button-group";
import { XXMIImporterList } from "@renderer/components/xxmi/xxmi-importer-list";
import { DEFAULT_BG } from "@renderer/const";
import { cn } from "@renderer/lib/utils";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/xxmi")({ component: XXMILayout });

export function XXMILayout() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { data: overview } = useQuery({ queryKey: ["xxmi:overview"], queryFn: XXMI.GetOverview });
  const external = overview?.launcherMode === LauncherMode.LauncherExternal;

  const switchLauncher = async (mode: LauncherMode) => {
    try {
      await XXMI.SetLauncherMode(mode);
      void queryClient.invalidateQueries({
        predicate: (query) =>
          typeof query.queryKey[0] === "string" && query.queryKey[0].startsWith("xxmi:"),
      });
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  };

  return (
    <div className="flex h-full min-h-0 overflow-hidden text-foreground select-none">
      {overview && !external && <XXMIImporterList />}

      <div className={cn("flex min-w-0 flex-1 flex-col", DEFAULT_BG)}>
        <header className="flex h-10 shrink-0 items-center gap-3 border-b border-border px-4">
          <span className="font-mono text-xs font-medium text-foreground">XXMI</span>
          <ButtonGroup
            className="ml-auto"
            aria-label={t("page.setting.xxmi.launcherMode.title")}
            title={t("page.setting.xxmi.launcherMode.description")}
          >
            <Button
              size="xs"
              variant={overview && !external ? "default" : "outline"}
              aria-pressed={!!overview && !external}
              disabled={!overview || !external}
              onClickPromise={() => switchLauncher(LauncherMode.LauncherBuiltin)}
            >
              {t("page.setting.xxmi.launcherMode.builtin")}
            </Button>
            <Button
              size="xs"
              variant={external ? "default" : "outline"}
              aria-pressed={external}
              disabled={!overview || external}
              onClickPromise={() => switchLauncher(LauncherMode.LauncherExternal)}
            >
              {t("page.setting.xxmi.launcherMode.external")}
            </Button>
          </ButtonGroup>
        </header>

        <div className="flex min-h-0 flex-1 flex-col">
          <Outlet />
        </div>
      </div>
    </div>
  );
}
