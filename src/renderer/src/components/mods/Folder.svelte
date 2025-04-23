<script lang="ts">
  import { Mods } from "@/lib/helpers";
  import { FSH } from "@/lib/helpers/fs.helper";
  import type { FileInfo } from "../../../../types/fs.types";
  import type { DirectChildren } from "../../../../types/mods.types";
  import { cn } from "@/lib/utils";
  import { ImageOffIcon } from "lucide-svelte";

  let currentFolderPath = Mods.currentFolderPath;
  let currentCharPath = Mods.currentCharPath;
  let mods = $state<DirectChildren[] | null>(null);

  const sortedMods = $derived(
    mods
      ? [...mods].sort((a, b) => {
          const aDisabled = a.name.toLowerCase().startsWith("disabled");
          const bDisabled = b.name.toLowerCase().startsWith("disabled");

          if (aDisabled && !bDisabled) return 1;
          if (!aDisabled && bDisabled) return -1;
          return 0;
        })
      : null,
  );

  const getMods = async (path: string) => {
    Mods.getDirectChildren(path).then((resp) => {
      mods = resp;
    });
  };

  $effect(() => {
    if ($currentCharPath) {
      getMods($currentCharPath);
    }
  });

  const processModName = (name: string) => {
    return name.replace(/^disabled /i, "");
  };
</script>

<div class="h-full overflow-y-auto p-4">
  {#if sortedMods}
    <div
      class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4"
    >
      {#each sortedMods as mod}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class={cn(
            "border-2 rounded shadow hover:shadow-lg duration-200 transition-all",
            mod.name.toLowerCase().startsWith("disabled")
              ? "border-neutral-500"
              : "border-green-700",
          )}
          onclick={() => {
            Mods.mod.toggle(mod.path).then(() => {
              getMods($currentCharPath);
            });
          }}
        >
          <div
            class={cn(
              "relative flex justify-center items-center aspect-square duration-200 transition-all",
              mod.name.toLowerCase().startsWith("disabled")
                ? "bg-neutral-500"
                : "bg-green-700",
            )}
          >
            {#if mod.previewB64}
              <img
                class="relative object-contain w-full h-full"
                src={mod.previewB64}
                alt={mod.name}
                loading="lazy"
              />
            {:else}
              <ImageOffIcon size={50} />
            {/if}

            <div
              class="absolute flex flex-row justify-center items-center bottom-2 left-1/2 -translate-x-1/2 w-full"
            >
              <div
                class="flex flex-row h-full items-center rounded-full px-[8px] py-[3px] justify-center gap-2 bg-neutral-100 dark:bg-zinc-900"
              >
                <p
                  class="dragselect-start-disallowed line-clamp-1 text-ellipsis break-all text-sm text-primary"
                >
                  {processModName(mod.name)}
                </p>
              </div>
            </div>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
