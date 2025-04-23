<script lang="ts">
  import { Mods } from "@/lib/helpers";
  import { FSH } from "@/lib/helpers/fs.helper";
  import type { FileInfo } from "../../../../types/fs.types";
  import type { DirectChildren } from "../../../../types/mods.types";
  import { cn, getSearchScore } from "@/lib/utils";
  import {
    FolderOpenIcon,
    ImageOffIcon,
    LayoutGridIcon,
    ListIcon,
    SearchIcon,
    Trash2Icon,
  } from "lucide-svelte";
  import { Input } from "@/lib/components/ui/input";
  import * as DropdownMenu from "@/lib/components/ui/dropdown-menu";
  import { Button, buttonVariants } from "@/lib/components/ui/button";
  import { _ } from "svelte-i18n";
  import { getChosung } from "@/lib/utils";
  import { flip } from "svelte/animate";
  import { fade } from "svelte/transition";
  import { sineOut } from "svelte/easing";
  import { toast } from "svelte-sonner";
  import * as AlertDialog from "@/lib/components/ui/alert-dialog";

  let currentFolderPath = Mods.currentFolderPath;
  let currentCharPath = Mods.currentCharPath;
  let mods = $state<DirectChildren[] | null>(null);
  let layout = $state<"grid" | "list">("grid");
  let searchQuery = $state("");

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

  const filteredMods = $derived(
    sortedMods && searchQuery.trim()
      ? sortedMods
          .map((item) => ({
            item,
            score: getSearchScore(item.name, searchQuery.toLowerCase().trim()),
          }))
          .filter(({ item, score }) => {
            if (score > 0) return true;

            const query = searchQuery.toLowerCase().trim();
            const isChosungSearch = /^[ㄱ-ㅎ]+$/.test(query);

            if (isChosungSearch) {
              const itemChosung = getChosung(item.name.toLowerCase());
              return itemChosung.includes(query);
            }

            return false;
          })
          .sort((a, b) => b.score - a.score)
          .map(({ item }) => item)
      : sortedMods,
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

<div class="h-full overflow-y-auto">
  <div class="flex items-center p-1 sticky top-0 z-50 dark:bg-[#111115] w-full">
    <div class="flex-1 min-w-0"></div>

    <div class="flex gap-1 ml-4 flex-shrink-0">
      <div class="w-full relative flex items-center">
        <SearchIcon
          class="w-5 h-5 absolute left-2.5 top-2.5 text-gray-500 dark:text-gray-400"
        />
        <Input
          class="pl-8 w-[200px] border-none h-8"
          placeholder={$_("g.search")}
          bind:value={searchQuery}
        />
      </div>

      <div>
        <Button
          variant="ghost"
          size="icon"
          onclick={() => {
            if (layout === "grid") {
              layout = "list";
            } else {
              layout = "grid";
            }
          }}
        >
          {#if layout === "grid"}
            <ListIcon />
          {:else}
            <LayoutGridIcon />
          {/if}
        </Button>
      </div>
    </div>
  </div>

  {#if filteredMods}
    <div class="grid grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-3 p-2">
      {#each filteredMods as mod (mod.path)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          in:fade={{ duration: 200 }}
          animate:flip={{ duration: 200, easing: sineOut }}
          class={cn(
            "border-2 rounded shadow hover:shadow-lg duration-200 transition-all",
            mod.name.toLowerCase().startsWith("disabled")
              ? "border-neutral-300 dark:border-neutral-500"
              : "border-green-700 dark:border-green-800",
            mod.name.toLowerCase().startsWith("disabled")
              ? "bg-neutral-300 dark:bg-neutral-500"
              : "bg-green-700 dark:bg-green-800",
          )}
          onclick={() => {
            Mods.mod.toggle(mod.path).then(() => {
              getMods($currentCharPath).catch((e: any) => {
                toast.error("모드 토글중 오류가 발생했어요", {
                  description: e.message,
                });
              });
            });
          }}
        >
          <div class="flex items-center justify-between p-1">
            <p class="font-semibold text-sm">
              {processModName(mod.name)}
            </p>
            <div class="buttons flex items-center space-x-1">
              <AlertDialog.Root>
                <AlertDialog.Trigger
                  class="rounded-lg p-0.5 hover:bg-muted/50 duration-200"
                  onclick={(e) => {
                    e.stopPropagation();
                  }}
                >
                  <Trash2Icon size={20} class="text-destructive" />
                </AlertDialog.Trigger>
                <AlertDialog.Content>
                  <AlertDialog.Header>
                    <AlertDialog.Title>모드 삭제</AlertDialog.Title>
                    <AlertDialog.Description>
                      정말 이 모드를 삭제할까요?
                    </AlertDialog.Description>
                  </AlertDialog.Header>
                  <AlertDialog.Footer>
                    <AlertDialog.Cancel>취소</AlertDialog.Cancel>
                    <AlertDialog.Action
                      class={buttonVariants({ variant: "destructive" })}
                      onclick={() => {
                        FSH.deletePath(mod.path)
                          .then(() => {
                            toast(`${mod.name} 모드가 삭제되었습니다`);
                            getMods($currentCharPath);
                          })
                          .catch((e: any) => {
                            toast.error("모드 삭제중 오류 발생", {
                              description: e.message,
                            });
                          });
                      }}>계속</AlertDialog.Action
                    >
                  </AlertDialog.Footer>
                </AlertDialog.Content>
              </AlertDialog.Root>
              <button
                class="rounded-lg p-1 hover:bg-muted/50 duration-200"
                onclick={(e) => {
                  e.stopPropagation();
                  FSH.openPath(mod.path);
                }}
              >
                <FolderOpenIcon size={20} />
              </button>
            </div>
          </div>
          <div
            class={cn(
              "relative flex justify-center items-center aspect-square duration-200 transition-all",
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
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
