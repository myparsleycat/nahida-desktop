<script lang="ts">
  import { ModsHelper } from "$lib/helpers";
  import { FSH } from "$lib/helpers/fs.helper";
  import { cn, getSearchScore } from "$lib/utils";
  import {
    EllipsisIcon,
    FolderOpenIcon,
    ImageOffIcon,
    KeyboardIcon,
    LayoutGridIcon,
    ListIcon,
    SearchIcon,
    Trash2Icon,
    WrenchIcon,
  } from "@lucide/svelte";
  import { Input } from "$lib/components/ui/input";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import { Button, buttonVariants } from "$lib/components/ui/button";
  import { _ } from "svelte-i18n";
  import { getChosung } from "$lib/utils";
  import { flip } from "svelte/animate";
  import { fade } from "svelte/transition";
  import { sineOut } from "svelte/easing";
  import { toast } from "svelte-sonner";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import PreviewModal from "./PreviewModalEntry.svelte";
  import { createQuery, createMutation } from "@tanstack/svelte-query";
  import * as Dialog from "$lib/components/ui/dialog/index";
  import * as Table from "$lib/components/ui/table/index";
  import { onMount } from "svelte";
  import type { DirectChildren } from "@shared/types/mods.types";
  import Validator from "@shared/utils/Validator";
  import { clickWithoutDrag } from "$lib/utils/global.utils";

  let currentCharPath = ModsHelper.currentCharPath;
  let layout = $state<"grid" | "list">("grid");
  let searchQuery = $state("");
  let modsContainerElement = $state<HTMLDivElement>();
  let previewDragState = $state(new Map<string, boolean>());
  let modDragState = $state(false);
  let timestamp = $state(Date.now());

  let deleteDialog = $state<{ open: boolean; mod: DirectChildren | null }>({
    open: false,
    mod: null,
  });
  const deleteDialogClear = () => (deleteDialog = { open: false, mod: null });
  let showOverwriteDialog = $state(false);
  let fileToOverwrite = $state<ArrayBuffer | null>(null);
  let previewPathToOverwrite = $state<string | null>(null);

  onMount(async () => {
    layout = await ModsHelper.ui.layout.mod.get();
  });

  const getMods = async (path: string) => {
    return ModsHelper.mod.read(path);
  };

  const data = $derived(
    createQuery({
      queryKey: ["mods", $currentCharPath],
      queryFn: async () => {
        if ($currentCharPath) {
          return await getMods($currentCharPath);
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

  const sortedMods = $derived(
    $data.data
      ? [...$data.data].sort((a, b) => {
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

  $effect(() => {
    if ($currentCharPath && modsContainerElement) {
      modsContainerElement.scrollTo({
        top: 0,
        behavior: "smooth",
      });
    }
  });

  const processModName = (name: string) => {
    return name.replace(/^disabled /i, "");
  };

  const setDragState = (modPath: string, isDragging: boolean) => {
    previewDragState.set(modPath, isDragging);
    previewDragState = new Map(previewDragState);
  };

  const savePreviewImage = async (path: string, _data: ArrayBuffer) => {
    await FSH.writeFile(path, _data)
      .then((resp) => {
        if (resp) {
          toast.success($_("mods.r.body.mod.misc.save_preview.toast.success"));
          timestamp = Date.now();
          $data.refetch();
        }
      })
      .catch((e: any) => {
        toast.error($_("mods.r.body.mod.misc.save_preview.toast.error"), {
          description: e.message,
        });
      });
  };

  const getImageFromClipboard = async (): Promise<File | null> => {
    try {
      const clipboardItems = await navigator.clipboard.read();

      for (const clipboardItem of clipboardItems) {
        const imageTypes = clipboardItem.types.filter((type) =>
          type.startsWith("image/"),
        );

        if (imageTypes.length > 0) {
          const blob = await clipboardItem.getType(imageTypes[0]);
          return new File(
            [blob],
            `clipboard-image.${imageTypes[0].split("/")[1]}`,
            { type: imageTypes[0] },
          );
        }
      }

      return null;
    } catch (err: any) {
      toast.error($_("mods.r.body.mod.misc.clipboard.toast.error"), {
        description: err.message,
      });
      throw err;
    }
  };
</script>

<div class="h-full w-full flex flex-col">
  <div class="flex items-center p-1 px-4 dark:bg-[#111115] w-full h-12">
    <div class="flex-1 min-w-0"></div>

    <div class="flex gap-1 ml-4 flex-shrink-0">
      <div class="w-full relative flex items-center">
        <SearchIcon
          class="w-5 h-5 absolute left-2 top-1.5 text-gray-500 dark:text-gray-400"
        />
        <Input
          class="pl-8 w-[200px] h-8"
          placeholder={$_("global.search")}
          bind:value={searchQuery}
        />
      </div>

      <div>
        <Button
          class="border size-8"
          variant="outline"
          size="icon"
          onclick={async () => {
            if (layout === "grid") {
              layout = "list";
            } else {
              layout = "grid";
            }
            await ModsHelper.ui.layout.mod.set(layout);
          }}
        >
          {#if layout === "grid"}
            <ListIcon />
          {:else}
            <LayoutGridIcon />
          {/if}
        </Button>
      </div>

      <DropdownMenu.Root>
        <DropdownMenu.Trigger
          class={cn(
            buttonVariants({ variant: "outline", size: "icon" }),
            "size-8",
          )}
        >
          <EllipsisIcon />
        </DropdownMenu.Trigger>
        <DropdownMenu.Content>
          <DropdownMenu.Group>
            <DropdownMenu.Item
              class="cursor-pointer"
              onclick={() => {
                if (!$currentCharPath) {
                  toast.warning($_("mods.r.head.dm.toast.ccpm"));
                  return;
                }

                ModsHelper.folder.dir
                  .enableAll($currentCharPath)
                  .then((resp) => {
                    if (resp) $data.refetch();
                  });
              }}>{$_("mods.r.head.dm.allon")}</DropdownMenu.Item
            >
            <DropdownMenu.Item
              class="cursor-pointer"
              onclick={() => {
                if (!$currentCharPath) {
                  toast.warning($_("mods.r.head.dm.toast.ccpm"));
                  return;
                }

                ModsHelper.folder.dir
                  .disableAll($currentCharPath)
                  .then((resp) => {
                    if (resp) $data.refetch();
                  });
              }}>{$_("mods.r.head.dm.alloff")}</DropdownMenu.Item
            >
          </DropdownMenu.Group>
        </DropdownMenu.Content>
      </DropdownMenu.Root>
    </div>
  </div>

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="pl-3 pb-3 pt-1 pr-1.5 flex flex-col flex-1 overflow-auto relative"
    ondragover={(e) => {
      e.preventDefault();
      modDragState = true;
    }}
    ondragleave={(e) => {
      e.preventDefault();
      modDragState = false;
    }}
    ondrop={async (e) => {
      e.preventDefault();
      if (!e?.dataTransfer) return;

      try {
        const dropItems: string[] = [];

        const urlCandidate =
          e.dataTransfer.getData("text/uri-list") ||
          e.dataTransfer.getData("text/plain");
        if (Validator.url(urlCandidate)) {
          dropItems.push(urlCandidate);
        }

        dropItems.push(
          ...Array.from(e.dataTransfer.files).map((file) =>
            window.webUtils.getPathForFile(file),
          ),
        );

        if (dropItems.length > 0) {
          ModsHelper.intx.drop(dropItems).then((resp) => {
            if (resp) $data.refetch();
          });
        }
      } finally {
        modDragState = false;
      }
    }}
  >
    <div
      class={cn(
        "absolute inset-0 bg-black/30 z-30 flex items-center justify-center pointer-events-none duration-200 outline-dotted rounded-lg m-2",
        modDragState ? "opacity-100" : "opacity-0",
      )}
    >
      <div class="text-white text-center">
        <span class="text-2xl">📁</span>
        <p class="font-medium mt-2">{$_("global.drop_here")}</p>
      </div>
    </div>

    <div
      bind:this={modsContainerElement}
      class="grid grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-4 h-full overflow-y-auto pr-1.5"
    >
      {#if filteredMods}
        {#each filteredMods as mod (mod.path)}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            animate:flip={{ duration: 200, easing: sineOut }}
            class={cn(
              "border-2 rounded shadow hover:shadow-lg duration-200 transition-all h-min",
              mod.name.toLowerCase().startsWith("disabled")
                ? "border-neutral-300 dark:border-neutral-500"
                : "border-green-700 dark:border-green-800",
              mod.name.toLowerCase().startsWith("disabled")
                ? "bg-neutral-300 dark:bg-neutral-500"
                : "bg-green-700 dark:bg-green-800",
            )}
            onclick={() => {
              ModsHelper.mod
                .toggle(mod.path)
                .then(() => {
                  $data.refetch();
                })
                .catch((e) => {
                  toast.error("모드 토글중 오류가 발생했어요", {
                    description: e.message,
                  });
                });
            }}
          >
            <div class="flex items-center justify-between p-1">
              <p
                class="font-semibold text-sm truncate overflow-hidden max-w-[70%] select-text"
              >
                {processModName(mod.name)}
              </p>

              <div class="buttons flex items-center space-x-1 shrink-0">
                <DropdownMenu.Root>
                  <DropdownMenu.Trigger
                    class="rounded-lg p-1 hover:bg-muted/50 duration-200"
                  >
                    <WrenchIcon size={20} />
                  </DropdownMenu.Trigger>
                  <DropdownMenu.Content>
                    <DropdownMenu.Group>
                      <DropdownMenu.Item
                        onclick={async (e) => {
                          e.stopPropagation();
                          ModsHelper.mod.fix(mod.path, "genshin");
                        }}>{`${$_("g.genshin")} Hash`}</DropdownMenu.Item
                      >
                      <DropdownMenu.Item
                        onclick={async (e) => {
                          e.stopPropagation();
                          await ModsHelper.mod.fix(mod.path, "starrail:hash");
                        }}>{`${$_("g.starrail")} Hash`}</DropdownMenu.Item
                      >
                      <DropdownMenu.Item
                        onclick={async (e) => {
                          e.stopPropagation();
                          await ModsHelper.mod.fix(
                            mod.path,
                            "starrail:pipeline",
                          );
                        }}>{`${$_("g.starrail")} Pipeline`}</DropdownMenu.Item
                      >
                      <DropdownMenu.Item
                        onclick={async (e) => {
                          e.stopPropagation();
                          await ModsHelper.mod.fix(mod.path, "zzz");
                        }}>{`${$_("g.zzz")} Hash`}</DropdownMenu.Item
                      >
                      <DropdownMenu.Item
                        onclick={async (e) => {
                          e.stopPropagation();
                          await ModsHelper.mod.fix(mod.path, "wuwa");
                        }}>{`${$_("g.wuwa")} Hash`}</DropdownMenu.Item
                      >
                    </DropdownMenu.Group>
                  </DropdownMenu.Content>
                </DropdownMenu.Root>

                {#if mod.ini && mod.ini.data.length > 0}
                  <Dialog.Root>
                    <Dialog.Trigger
                      class="rounded-lg p-1 hover:bg-muted/50 duration-200"
                      onclick={(e) => e.stopPropagation()}
                    >
                      <KeyboardIcon size={20} />
                    </Dialog.Trigger>

                    <Dialog.Content
                      autofocus={false}
                      class="max-h-[80vh] flex flex-col min-w-max"
                    >
                      <Dialog.Header>
                        <Dialog.Title class="mb-4"
                          >{mod.name} Toggles</Dialog.Title
                        >
                      </Dialog.Header>

                      <Dialog.Description
                        class="flex-1 overflow-hidden flex flex-col"
                      >
                        <Table.Root>
                          <Table.Header>
                            <Table.Row>
                              <Table.Head class="w-[100px]">section</Table.Head>
                              <Table.Head>var</Table.Head>
                              <Table.Head class="whitespace-nowrap"
                                >cycle</Table.Head
                              >
                              <Table.Head>key</Table.Head>
                              <Table.Head>back</Table.Head>
                            </Table.Row>
                          </Table.Header>
                        </Table.Root>

                        <div class="flex-1 overflow-y-auto max-h-[40vh]">
                          <Table.Root>
                            <Table.Body>
                              {#each mod.ini.data as ini}
                                <Table.Row>
                                  <Table.Cell class="font-medium w-[100px]"
                                    >{ini.sectionName}</Table.Cell
                                  >
                                  <Table.Cell>{ini.varName}</Table.Cell>
                                  <Table.Cell class="whitespace-nowrap"
                                    >{ini.cycle}</Table.Cell
                                  >
                                  <Table.Cell>
                                    <Input
                                      defaultValue={ini.key}
                                      autofocus={false}
                                      onchange={(e) => {
                                        ModsHelper.ini
                                          .update(
                                            mod.ini!.path,
                                            ini.sectionName,
                                            "key",
                                            e.currentTarget.value,
                                          )
                                          .then((resp) => {
                                            if (resp) $data.refetch();
                                          });
                                      }}
                                    />
                                  </Table.Cell>
                                  <Table.Cell>
                                    <Input
                                      defaultValue={ini.back}
                                      autofocus={false}
                                      onchange={(e) => {
                                        ModsHelper.ini
                                          .update(
                                            mod.ini!.path,
                                            ini.sectionName,
                                            "back",
                                            e.currentTarget.value,
                                          )
                                          .then((resp) => {
                                            if (resp) $data.refetch();
                                          });
                                      }}
                                    />
                                  </Table.Cell>
                                </Table.Row>
                              {/each}
                            </Table.Body>
                          </Table.Root>
                        </div>
                      </Dialog.Description>
                    </Dialog.Content>
                  </Dialog.Root>
                {/if}

                <button
                  class="rounded-lg p-1 hover:bg-muted/50 duration-200"
                  onclick={(e) => {
                    e.stopPropagation();
                    FSH.openPath(mod.path);
                  }}
                >
                  <FolderOpenIcon size={20} />
                </button>

                <button
                  class="rounded-lg p-0.5 hover:bg-muted/50 duration-200"
                  onclick={(e) => {
                    e.stopPropagation();
                    deleteDialog = { open: true, mod };
                  }}
                >
                  <Trash2Icon size={20} class="text-destructive" />
                </button>
              </div>
            </div>

            <div
              class={cn(
                "relative flex justify-center items-center aspect-square duration-200 transition-all overflow-hidden group",
              )}
              ondragover={(e) => {
                e.preventDefault();
                e.stopPropagation();
                setDragState(mod.path, true);
              }}
              ondragleave={(e) => {
                e.preventDefault();
                e.stopPropagation();
                setDragState(mod.path, false);
              }}
              ondrop={async (e) => {
                e.preventDefault();
                e.stopPropagation();
                setDragState(mod.path, false);

                const files = e.dataTransfer?.files;
                console.log(e.dataTransfer);
                if (!files || files.length < 1) {
                  toast.warning($_("mods.r.body.mod.misc.drop.toast.!files"));
                  return;
                } else if (files?.length > 1) {
                  toast.warning($_("mods.r.body.mod.misc.drop.toast.files>1"));
                  return;
                }

                const file = files[0];

                if (!file.type.startsWith("image/")) {
                  toast.warning($_("mods.r.body.mod.misc.drop.toast.!image"));
                  return;
                }

                const fileData = await file.arrayBuffer();

                if (
                  mod.preview &&
                  mod.preview.path
                    .split("\\")
                    .pop()
                    ?.toLowerCase()
                    .startsWith("preview")
                ) {
                  fileToOverwrite = fileData;
                  previewPathToOverwrite = mod.preview.path;
                  showOverwriteDialog = true;
                } else {
                  const ext = file.name.split(".").pop();
                  const path = `${mod.path}/preview.${ext}`;
                  await savePreviewImage(path, fileData);
                }
              }}
            >
              {#if mod.preview}
                <div class="absolute inset-0 w-full h-full">
                  <img
                    class="w-full h-full object-cover blur scale-110"
                    src={`nahida://image-local?path=${encodeURIComponent(`${mod.preview.path}`)}&t=${timestamp}`}
                    alt={mod.name}
                    loading="lazy"
                  />
                </div>
                <img
                  class="relative object-contain w-full h-full z-10"
                  src={`nahida://image-local?path=${encodeURIComponent(`${mod.preview.path}`)}&t=${timestamp}`}
                  alt={mod.name}
                  loading="lazy"
                />
                <div
                  class="absolute left-1 top-1 z-20 opacity-0 group-hover:opacity-100 duration-200"
                >
                  <PreviewModal
                    src={`nahida://image-local?path=${encodeURIComponent(`${mod.preview.path}`)}&t=${timestamp}`}
                    alt={`${mod.name} Modal`}
                  />
                </div>

                <div
                  class={cn(
                    "absolute inset-0 bg-black/60 z-30 flex items-center justify-center pointer-events-none duration-200",
                    previewDragState.get(mod.path)
                      ? "opacity-100"
                      : "opacity-0",
                  )}
                >
                  <div class="text-white text-center">
                    <span class="text-2xl">📁</span>
                    <p class="font-medium mt-2">{$_("global.drop_here")}</p>
                  </div>
                </div>
              {:else}
                <div class="flex flex-col justify-center items-center gap-2">
                  <ImageOffIcon size={50} />
                  <button
                    class={cn(
                      "border border-black dark:border-white bg-transparent py-0.5 px-1.5 rounded-lg duration-200 hover:bg-white/10",
                    )}
                    onclick={async (e: MouseEvent) => {
                      e.stopPropagation();

                      const file = await getImageFromClipboard();
                      if (!file) {
                        toast.warning(
                          $_("mods.r.body.mod.misc.clipboard.toast.!file"),
                        );
                        return;
                      }

                      const arrbuf = await file.arrayBuffer();

                      if (
                        mod.preview &&
                        mod.preview.path
                          .split("\\")
                          .pop()
                          ?.toLowerCase()
                          .startsWith("preview")
                      ) {
                        fileToOverwrite = arrbuf;
                        previewPathToOverwrite = mod.preview.path;
                        showOverwriteDialog = true;
                      } else {
                        const ext = file.name.split(".").pop();
                        const path = `${mod.path}/preview.${ext}`;
                        await savePreviewImage(path, arrbuf);
                      }
                    }}>{$_("global.paste")}</button
                  >
                </div>

                <div
                  class={cn(
                    "absolute inset-0 bg-black/60 z-30 flex items-center justify-center pointer-events-none duration-200",
                    previewDragState.get(mod.path)
                      ? "opacity-100"
                      : "opacity-0",
                  )}
                >
                  <div class="text-white text-center">
                    <span class="text-2xl">📁</span>
                    <p class="font-medium mt-2">{$_("global.drop_here")}</p>
                  </div>
                </div>
              {/if}
            </div>
          </div>
        {/each}
      {/if}
    </div>
  </div>
</div>

<AlertDialog.Root bind:open={deleteDialog.open}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title
        >{$_("mods.r.body.mod.head.delmod.a")}</AlertDialog.Title
      >
      <AlertDialog.Description>
        {$_("mods.r.body.mod.head.delmod.b")}
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel>{$_("global.cancel")}</AlertDialog.Cancel>
      <AlertDialog.Action
        class={buttonVariants({ variant: "destructive" })}
        onclick={() => {
          if (!deleteDialog.mod) {
            toast.warning($_("mods.r.body.mod.head.delmod.toast.ddme"));
            return;
          }

          FSH.deletePath(deleteDialog.mod.path)
            .then((resp) => {
              if (resp) {
                toast.success(
                  $_("mods.r.body.mod.head.delmod.toast.success", {
                    values: { mod: deleteDialog.mod?.name },
                  }),
                );
                $data.refetch();
              }
            })
            .finally(() => {
              deleteDialogClear();
            });
        }}>{$_("global.delete")}</AlertDialog.Action
      >
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root open={showOverwriteDialog}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{$_("mods.r.body.mod.misc.ow.a")}</AlertDialog.Title>
      <AlertDialog.Description>
        {$_("mods.r.body.mod.misc.ow.b")}
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel
        onclick={() => {
          showOverwriteDialog = false;
          fileToOverwrite = null;
          previewPathToOverwrite = null;
        }}>{$_("global.cancel")}</AlertDialog.Cancel
      >
      <AlertDialog.Action
        onclick={async () => {
          if (fileToOverwrite && previewPathToOverwrite) {
            await savePreviewImage(previewPathToOverwrite, fileToOverwrite);
            showOverwriteDialog = false;
            fileToOverwrite = null;
            previewPathToOverwrite = null;
          }
        }}>{$_("global.continue")}</AlertDialog.Action
      >
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
