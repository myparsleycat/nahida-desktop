<script lang="ts">
  import { Button, buttonVariants } from "$lib/components/ui/button";
  import * as Resizable from "$lib/components/ui/resizable/index";
  import * as Dialog from "$lib/components/ui/dialog/index";
  import { ModsHelper } from "$lib/helpers";
  import { cn } from "$lib/utils";
  import { Input } from "$lib/components/ui/input";
  import { Label } from "$lib/components/ui/label";
  import {
    AlignJustifyIcon,
    ArrowUpFromLineIcon,
    ChevronRightIcon,
    ChevronsDownUpIcon,
    DotIcon,
    EditIcon,
    FolderOpenIcon,
    FolderPenIcon,
    FolderPlusIcon,
    FolderSyncIcon,
    ListIcon,
    SearchIcon,
    Trash2Icon,
    WrenchIcon,
  } from "@lucide/svelte";
  import { toast } from "svelte-sonner";
  import Folder from "./Folder.svelte";
  import * as ContextMenu from "$lib/components/ui/context-menu/index";
  import autoAnimate from "@formkit/auto-animate";
  import { _ } from "svelte-i18n";
  import Separator from "$lib/components/ui/separator/separator.svelte";
  import type { ModFolders } from "@shared/types/fs.types";
  import { createQuery } from "@tanstack/svelte-query";
  import { FSH } from "$lib/helpers/fs.helper";
  import { onMount } from "svelte";

  let size = ModsHelper.resizableSize;
  let currentFolderPath = ModsHelper.currentFolderPath;
  let folders = ModsHelper.folders;
  // let folderChildren = ModsHelper.folderChildren;
  let currentCharPath = ModsHelper.currentCharPath;

  let layout = $state<"list" | "align">("align");
  let open = $state(false);
  let temp_name = $state<string | null>(null);
  let temp_path = $state<string | null>(null);
  let searchQuery = $state("");
  let timestamp = $state(Date.now());

  let draggedIdx = $state<number | null>(null);
  let dropTargetIdx = $state<number | null>(null);

  const clear_temp = () => {
    temp_name = null;
    temp_path = null;
  };

  const getResizableSize = async () => {
    size.set(await ModsHelper.ui.resizable.get());
  };

  const getFolderChildren = async (path: string) => {
    const resp = await ModsHelper.folder.read(path);
    const filteredResp = resp.filter((item) => !item.ini);
    return filteredResp;
    // folderChildren.set(filteredRes);
  };

  const getFolders = async () => {
    folders.set(await ModsHelper.folder.getAll());
  };

  const routing = async (folder: ModFolders) => {
    if ($currentFolderPath === folder.path) {
      ModsHelper.clearPath();
      return;
    }

    currentCharPath.set(folder.path);
    currentFolderPath.set(folder.path);
  };

  const data = $derived(
    createQuery({
      queryKey: ["folders", $currentFolderPath],
      queryFn: async () => {
        if ($currentFolderPath) {
          return getFolderChildren($currentFolderPath);
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

  onMount(async () => {
    await getResizableSize();
    await getFolders();
    layout = await ModsHelper.ui.layout.folder.get();
  });
</script>

<div class="flex flex-row w-full h-full">
  <Resizable.PaneGroup direction="horizontal">
    <Resizable.Pane
      defaultSize={$size}
      minSize={20}
      maxSize={35}
      onResize={async (size) => {
        ModsHelper.ui.resizable.set(size);
      }}
    >
      <div class="h-full w-full flex flex-col pt-2 pb-2 pl-2 pr-1 space-y-2">
        <div class="w-full flex items-center gap-2">
          <div class="relative">
            <SearchIcon
              class="w-5 h-5 absolute left-1.5 top-1.5 text-gray-500 dark:text-gray-400"
            />
            <Input
              class="pl-8 border-none h-8 w-full"
              placeholder={$_("global.search")}
              bind:value={searchQuery}
              disabled
            />
          </div>

          <Button
            size="icon"
            variant="outline"
            onclick={async () => {
              if (layout === "align") {
                layout = "list";
              } else {
                layout = "align";
              }
              await ModsHelper.ui.layout.folder.set(layout);
            }}
          >
            {#if layout === "align"}
              <ListIcon />
            {:else}
              <AlignJustifyIcon />
            {/if}
          </Button>
        </div>

        <Separator />

        <div
          class="flex-col justify-center duration-200 space-y-1 select-none h-full overflow-y-auto overflow-x-hidden pr-1"
        >
          {#each $folders as folder, idx}
            <ContextMenu.Root>
              <ContextMenu.Trigger>
                <button
                  draggable={true}
                  class={cn(
                    "flex gap-2 p-2 rounded-lg w-full hover:bg-muted duration-200 justify-between transition-all group",
                    $currentFolderPath === folder.path && "bg-muted",
                    draggedIdx === idx && "opacity-50",
                    dropTargetIdx === idx && "border-t-2 border-blue-500",
                  )}
                  onclick={() => routing(folder)}
                  ondragstart={(e) => {
                    draggedIdx = idx;
                    if (e.dataTransfer) {
                      e.dataTransfer.effectAllowed = "move";
                      e.dataTransfer.setData("text/html", "");
                    }
                  }}
                  ondragend={() => {
                    draggedIdx = null;
                    dropTargetIdx = null;
                  }}
                  ondragover={(e) => {
                    e.preventDefault();
                    if (e.dataTransfer) {
                      e.dataTransfer.dropEffect = "move";
                    }
                  }}
                  ondrop={(e) => {
                    e.preventDefault();

                    if (draggedIdx === null || draggedIdx === idx) {
                      return;
                    }

                    const draggedFolder = $folders[draggedIdx];
                    const newSequence = idx + 1;

                    ModsHelper.folder
                      .changeSeq(draggedFolder.path, newSequence)
                      .then((resp) => {
                        if (resp) getFolders();
                      });

                    draggedIdx = null;
                    dropTargetIdx = null;
                  }}
                  tabindex="0"
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
              </ContextMenu.Trigger>
              <ContextMenu.Content>
                <ContextMenu.Item
                  class="flex items-center gap-2 cursor-pointer"
                >
                  <FolderPenIcon size={18} />
                  이름 변경
                </ContextMenu.Item>
                <ContextMenu.Separator />
                <ContextMenu.Item
                  class="flex items-center gap-2 cursor-pointer"
                >
                  <WrenchIcon size={18} />
                  픽스
                </ContextMenu.Item>
                <ContextMenu.Item
                  class="flex items-center gap-2 cursor-pointer"
                >
                  <FolderSyncIcon size={18} />
                  자동 백업
                </ContextMenu.Item>
                <ContextMenu.Separator />
                <ContextMenu.Item
                  class="flex items-center gap-2 cursor-pointer"
                  onclick={() => {
                    ModsHelper.folder
                      .delete(folder.path)
                      .then(() => {
                        toast.success($_("mods.l.cm.remove.toast.success"));
                        ModsHelper.clearPath();
                        getFolders();
                      })
                      .catch((e: any) => {
                        toast.error($_("mods.l.cm.remove.toast.error"), {
                          description: e.message,
                        });
                      });
                  }}
                >
                  <Trash2Icon size={18} class="text-destructive" />
                  {$_("mods.l.cm.remove.a")}
                </ContextMenu.Item>
              </ContextMenu.Content>
            </ContextMenu.Root>

            <div
              class="flex flex-col gap-1 ml-4"
              use:autoAnimate={{ easing: "ease-in-out" }}
            >
              {#if folder.path === $currentFolderPath}
                {#each $data.data! as char, idx}
                  <button
                    class="flex w-full items-center min-h-8 group pr-2"
                    onclick={() => {
                      currentCharPath.set(char.path);
                    }}
                    use:autoAnimate={{ duration: 150 }}
                  >
                    {#if char.path === $currentCharPath}
                      <div class="flex-shrink-0">
                        <ChevronRightIcon color="green" />
                      </div>
                    {/if}

                    {#if layout === "list"}
                      <img
                        class="object-cover rounded size-18 mr-3"
                        src={char.preview
                          ? `nahida://image-local?path=${encodeURIComponent(`${char.preview?.path}`)}&t=${timestamp}`
                          : `nahida://image-web?url=https://nahida.live/${
                              idx === 0
                                ? "top.jpeg"
                                : idx === $data.data!.length - 1
                                  ? "bottom.jpg"
                                  : "center.jpg"
                            }`}
                        alt={char.name}
                        loading="lazy"
                        decoding="async"
                      />
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

          <Dialog.Root
            {open}
            onOpenChange={(v) => {
              open = v;
              if (!v) clear_temp();
            }}
          >
            <Dialog.Trigger
              class="flex gap-2 p-2 rounded-lg w-full hover:bg-muted duration-200 whitespace-nowrap"
            >
              <FolderPlusIcon />
              {$_("mods.l.mkng.a")}
            </Dialog.Trigger>
            <Dialog.Content class="sm:max-w-[425px]">
              <Dialog.Header>
                <Dialog.Title>{$_("mods.l.mkng.a")}</Dialog.Title>
                <Dialog.Description>
                  {$_("mods.l.mkng.b")}
                </Dialog.Description>
              </Dialog.Header>
              <div class="grid gap-4 py-4">
                <div class="flex w-full max-w-sm flex-col gap-1.5">
                  <Label for="name">{$_("global.name")}</Label>
                  <Input
                    id="name"
                    placeholder={$_("global.name")}
                    bind:value={temp_name}
                  />
                </div>
                <div class="flex w-full max-w-sm flex-col gap-1.5">
                  <Label for="path">{$_("global.path")}</Label>
                  <div class="flex justify-center items-center gap-2">
                    <Input id="path" readonly bind:value={temp_path} />
                    <Button
                      variant="outline"
                      size="default"
                      onclick={async () => {
                        temp_path = await FSH.select({
                          properties: ["openDirectory"],
                        });
                      }}
                    >
                      <EditIcon class="pointer-events-none" />
                    </Button>
                  </div>
                </div>
              </div>
              <Dialog.Footer>
                <Button
                  onclick={async () => {
                    if (!temp_name) {
                      toast.warning($_("mods.l.mkng.toast.nm"));
                      return;
                    } else if (!temp_path) {
                      toast.warning($_("mods.l.mkng.toast.pm"));
                      return;
                    }

                    ModsHelper.folder
                      .create(temp_name, temp_path)
                      .then((resp) => {
                        if (resp) {
                          toast.success($_("mods.l.mkng.toast.success"));
                          clear_temp();
                          open = false;
                          getFolders();
                        }
                      });
                  }}>생성</Button
                >
              </Dialog.Footer>
            </Dialog.Content>
          </Dialog.Root>
        </div>
      </div>
    </Resizable.Pane>

    <Resizable.Handle withHandle />

    <Resizable.Pane>
      <Folder></Folder>
    </Resizable.Pane>
  </Resizable.PaneGroup>
</div>
