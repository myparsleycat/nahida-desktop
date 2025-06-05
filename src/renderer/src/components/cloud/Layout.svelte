<script lang="ts">
  import * as ContextMenu from "$lib/components/ui/context-menu/index";
  import * as Tooltip from "$lib/components/ui/tooltip";
  import { cn } from "$lib/utils";
  import { Share2Icon, Trash2Icon } from "@lucide/svelte";
  import { _ } from "svelte-i18n";

  let { children } = $props();
</script>

<div class="flex flex-row w-full">
  <div
    class="flex flex-col overflow-y-auto overflow-x-hidden dragselect-start-allowed px-1 py-2 border-r flex-shrink-0"
  >
    <div class="space-y-2">
      <div class="flex">
        <Tooltip.Provider>
          <Tooltip.Root delayDuration={50}>
            <Tooltip.Trigger>
              <button
                class={cn(
                  "flex flex-row gap-2.5 w-full p-2 rounded-md transition-all items-center hover:bg-secondary text-primary cursor-pointer active",
                )}
              >
                <div class="flex flex-row gap-2 items-center">
                  <div>
                    <Share2Icon />
                  </div>
                </div>
              </button>
            </Tooltip.Trigger>
            <Tooltip.Content side="right">
              <p>{$_("drive.ui.share")}</p>
            </Tooltip.Content>
          </Tooltip.Root>
        </Tooltip.Provider>
      </div>

      <div class="flex">
        <Tooltip.Provider>
          <Tooltip.Root delayDuration={50}>
            <Tooltip.Trigger>
              <ContextMenu.Root>
                <ContextMenu.Trigger>
                  <button
                    class={cn(
                      "flex flex-row gap-2.5 w-full p-2 rounded-md transition-all items-center hover:bg-secondary text-primary cursor-pointer bg-transparent",
                    )}
                  >
                    <Trash2Icon />
                  </button>
                </ContextMenu.Trigger>
                <ContextMenu.Content>
                  <ContextMenu.Item class="gap-x-2 cursor-pointer">
                    <Trash2Icon size={20} />
                    {$_("drive.ui.empty_trash")}
                  </ContextMenu.Item>
                </ContextMenu.Content>
              </ContextMenu.Root>
            </Tooltip.Trigger>
            <Tooltip.Content side="right">
              <p>{$_("drive.ui.trash")}</p>
            </Tooltip.Content>
          </Tooltip.Root>
        </Tooltip.Provider>
      </div>
    </div>
  </div>

  <div class="flex w-full">
    {@render children()}
  </div>
</div>
