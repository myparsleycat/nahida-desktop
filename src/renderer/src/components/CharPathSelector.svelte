<script lang="ts">
  import { Button } from "$lib/components/ui/button";
  import { ModsHelper } from "$lib/helpers";
  import { cn } from "$lib/utils";
  import { ChevronRightIcon, FolderOpenIcon } from "@lucide/svelte";
  import autoAnimate from "@formkit/auto-animate";
  import { _ } from "svelte-i18n";
  import { Separator } from "$lib/components/ui/separator";
  import type { ModFolders } from "@shared/types/fs.types";
  import { createQuery } from "@tanstack/svelte-query";
  import { FSH } from "$lib/helpers/fs.helper";
  import { CharPathSelector } from "$lib/stores/global.store";
  import { fade } from "svelte/transition";

  let CharPathSelectorStore = CharPathSelector.store;
  let currentFolderPath = $derived($CharPathSelectorStore.currentFolderPath);
  let currentCharPath = $derived($CharPathSelectorStore.currentCharPath);
  let folders = $state<ModFolders[]>([]);

  const getFolderChildren = async (path: string) => {
    const resp = await ModsHelper.folder.read(path);
    const filteredResp = resp.filter((item) => !item.ini);
    return filteredResp;
    // folderChildren.set(filteredRes);
  };

  const getFolders = async () => {
    folders = await ModsHelper.folder.getAll();
  };

  $effect(() => {
    if ($CharPathSelectorStore.isOpen) {
      getFolders();
    }
  });

  const routing = async (folder: ModFolders) => {
    if (currentFolderPath === folder.path) {
      currentFolderPath = "";
      currentCharPath = "";
      return;
    }

    currentCharPath = folder.path;
    currentFolderPath = folder.path;
  };

  const data = $derived(
    createQuery({
      queryKey: ["folders", currentFolderPath],
      queryFn: async () => {
        if (currentFolderPath) {
          return getFolderChildren(currentFolderPath);
        } else return [];
      },
      refetchOnWindowFocus: "always",
      refetchIntervalInBackground: true,
      refetchInterval: () => {
        if (typeof document !== "undefined" && document.hidden) {
          return 5000; //
        }
        return 2500; //
      },
    }),
  );
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === "Escape" && $CharPathSelectorStore.isOpen) {
      CharPathSelector.close();
    }
  }}
/>

{#if $CharPathSelectorStore.isOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 bg-black/75 flex items-center justify-center z-10"
    onclick={(e) => {
      e.stopPropagation();
      CharPathSelector.close();
    }}
    transition:fade={{ duration: 100 }}
  >
    <div
      class="flex flex-col p-4 h-[80vh] border rounded-xl gap-3 bg-background min-w-[50vw]"
      onclick={(e) => e.stopPropagation()}
    >
      <div
        class="flex-col justify-center duration-200 space-y-1 select-none h-full overflow-y-auto overflow-x-hidden pr-1"
      >
        {#each folders as folder, idx}
          <button
            draggable={true}
            class={cn(
              "flex gap-2 p-2 rounded-lg w-full hover:bg-muted duration-200 justify-between transition-all group",
              currentFolderPath === folder.path && "bg-muted",
            )}
            onclick={(e) => {
              e.stopPropagation();
              routing(folder);
            }}
          >
            <p>
              {folder.name}
            </p>

            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div
              class="rounded-lg duration-200 opacity-0 group-hover:opacity-100"
              onclick={(e) => {
                e.stopPropagation();
                FSH.openPath(folder.path);
              }}
            >
              <FolderOpenIcon size={20} />
            </div>
          </button>

          <div
            class="flex flex-col gap-1 ml-4"
            use:autoAnimate={{ easing: "ease-in-out" }}
          >
            {#if folder.path === currentFolderPath}
              {#each $data.data! as char}
                <button
                  class="flex w-full items-center min-h-8 group pr-2"
                  onclick={(e) => {
                    e.stopPropagation();
                    currentCharPath = char.path;
                  }}
                  use:autoAnimate={{ duration: 150 }}
                >
                  {#if char.path === currentCharPath}
                    <div class="flex-shrink-0">
                      <ChevronRightIcon color="green" />
                    </div>
                  {/if}

                  <p class="truncate">
                    {char.name}
                  </p>

                  <!-- svelte-ignore a11y_click_events_have_key_events -->
                  <!-- svelte-ignore a11y_no_static_element_interactions -->
                  <div
                    class="absolute right-2 rounded-lg duration-200 opacity-0 group-hover:opacity-100"
                    onclick={(e) => {
                      e.stopPropagation();
                      FSH.openPath(char.path);
                    }}
                  >
                    <FolderOpenIcon size={20} />
                  </div>
                </button>
              {/each}
            {/if}
          </div>
        {/each}
      </div>
      <Separator />
      <div>
        <p>{currentCharPath || currentFolderPath}</p>
      </div>
      <Button
        disabled={!currentCharPath || !currentFolderPath}
        onclick={(e: MouseEvent) => {
          e.stopPropagation();
          const path = currentCharPath || currentFolderPath;
          CharPathSelector.setPath(path);
        }}>저장</Button
      >
    </div>
  </div>
{/if}
