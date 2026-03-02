import { Button } from "@renderer/components/ui/button";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { sortBy } from "es-toolkit";
import { TrashIcon } from "lucide-react";
import { toast } from "sonner";

export function PresetViewer() {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["ftm:presets"],
    queryFn: async () => {
      const presets = await window.api.invoke("ftm:getPresets");
      return sortBy(presets, ["name"]);
    },
  });

  return (
    <div
      className={`border rounded-lg bg-card transition-all overflow-hidden duration-200 flex flex-col h-full min-h-0`}
    >
      <div className="flex flex-col gap-2 p-3 border-b">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-base font-semibold">Preset List</p>
          </div>
        </div>
      </div>

      <ScrollArea className="flex-1 overflow-hidden">
        <div className="flex flex-col space-y-2 p-3">
          {!query.data || query.data?.length === 0 ? (
            <p className="text-sm text-muted-foreground">No presets found</p>
          ) : (
            query.data.map((preset) => (
              <div
                key={preset.id}
                className="grid grid-cols-[1fr_auto] items-center gap-2 p-3 rounded-lg border border-border bg-card hover:border-accent/40 hover:bg-card/80 transition-colors"
              >
                <p className="font-medium text-sm text-foreground truncate min-w-0">
                  {preset.name}
                </p>

                <div className="flex items-center gap-2">
                  <Button
                    variant="destructive"
                    size="icon"
                    onClickPromise={() =>
                      window.api
                        .invoke("ftm:deletePreset", preset.id)
                        .then(() => queryClient.invalidateQueries({ queryKey: ["ftm:presets"] }))
                        .catch((error) => {
                          toast.error(error.message);
                        })
                    }
                  >
                    <TrashIcon className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            ))
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
