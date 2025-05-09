<script lang="ts">
  import { Mods } from "@/lib/helpers";
  import { FSH } from "@/lib/helpers/fs.helper";
  import { cn, getSearchScore } from "@/lib/utils";
  import {
    ExpandIcon,
    FolderOpenIcon,
    ImageOffIcon,
    KeyboardIcon,
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
  import PreviewModal from "./PreviewModal.svelte";
  import { createQuery, createMutation } from "@tanstack/svelte-query";
  import * as Dialog from "$lib/components/ui/dialog";
  import * as Table from "$lib/components/ui/table";

  let currentCharPath = Mods.currentCharPath;
  let layout = $state<"grid" | "list">("grid");
  let searchQuery = $state("");
  let modsContainerElement = $state<HTMLDivElement>();
  let dragStates = $state(new Map<string, boolean>());
  let timestamp = $state(Date.now());

  let showOverwriteDialog = $state(false);
  let fileToOverwrite = $state<ArrayBuffer | null>(null);
  let previewPathToOverwrite = $state<string | null>(null);

  const getMods = async (path: string) => {
    return await Mods.getDirectChildren(path, { recursive: 2 });
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
          return 60000 * 1; // 1분 (백그라운드)
        }
        return 10000; // 10초 (포그라운드)
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
    dragStates.set(modPath, isDragging);
    dragStates = new Map(dragStates);
  };

  const savePreviewImage = async (path: string, _data: ArrayBuffer) => {
    try {
      await FSH.saveFile(path, _data);
      toast.success("프리뷰 이미지가 저장되었습니다");
      timestamp = Date.now();
      $data.refetch();
    } catch (e: any) {
      toast.error("이미지 저장 중 오류 발생", {
        description: e.message,
      });
    }
  };

  const getImageFromClipboard = async (): Promise<File | null> => {
    try {
      // 클립보드에서 데이터 가져오기
      const clipboardItems = await navigator.clipboard.read();

      for (const clipboardItem of clipboardItems) {
        // 이미지 타입 확인
        const imageTypes = clipboardItem.types.filter((type) =>
          type.startsWith("image/"),
        );

        if (imageTypes.length > 0) {
          // 이미지 데이터 추출
          const blob = await clipboardItem.getType(imageTypes[0]);
          // Blob에서 File로 변환
          return new File(
            [blob],
            `clipboard-image.${imageTypes[0].split("/")[1]}`,
            { type: imageTypes[0] },
          );
        }
      }

      // 이미지가 없는 경우
      return null;
    } catch (err) {
      console.error("클립보드에서 이미지를 가져오는 중 오류 발생:", err);
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
          class="w-5 h-5 absolute left-2 top-2 text-gray-500 dark:text-gray-400"
        />
        <Input
          class="pl-8 w-[200px] h-8"
          placeholder={$_("g.search")}
          bind:value={searchQuery}
        />
      </div>

      <div>
        <Button
          class="border size-8 p-0.5"
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
    <div class="pl-3 pb-3 pt-1 pr-1.5 flex flex-col flex-1 overflow-auto">
      <div
        bind:this={modsContainerElement}
        class="grid grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-3 h-full overflow-y-auto pr-1.5"
      >
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
              Mods.mod
                .toggle(mod.path)
                .then(() => {
                  $data.refetch();
                })
                .catch((e: any) => {
                  toast.error("모드 토글중 오류가 발생했어요", {
                    description: e.message,
                  });
                });
            }}
          >
            <div class="flex items-center justify-between p-1">
              <p
                class="font-semibold text-sm truncate overflow-hidden max-w-[70%]"
              >
                {processModName(mod.name)}
              </p>
              <div class="buttons flex items-center space-x-1 shrink-0">
                {#if mod.ini && mod.ini.data.length > 0}
                  <Dialog.Root>
                    <Dialog.Trigger
                      class="rounded-lg p-1 hover:bg-muted/50 duration-200"
                      onclick={(e) => e.stopPropagation()}
                    >
                      <KeyboardIcon size={20} />
                    </Dialog.Trigger>
                    <Dialog.Content autofocus={false}>
                      <Dialog.Header>
                        <Dialog.Title class="mb-4"
                          >{mod.name} 토글 수정</Dialog.Title
                        >
                        <Dialog.Description>
                          <Table.Root>
                            <Table.Header>
                              <Table.Row>
                                <Table.Head class="w-[100px]"
                                  >section</Table.Head
                                >
                                <Table.Head>var</Table.Head>
                                <Table.Head class="whitespace-nowrap"
                                  >cycle</Table.Head
                                >
                                <Table.Head>key</Table.Head>
                              </Table.Row>
                            </Table.Header>
                            <Table.Body>
                              {#each mod.ini.data as ini}
                                <Table.Row>
                                  <Table.Cell class="font-medium"
                                    >{ini.sectionName}</Table.Cell
                                  >
                                  <Table.Cell>{ini.varName}</Table.Cell>
                                  <Table.Cell>{ini.cycle}</Table.Cell>
                                  <Table.Cell>
                                    <Input
                                      defaultValue={ini.key}
                                      autofocus={false}
                                      onchange={(e) => {
                                        Mods.ini
                                          .update(
                                            mod.ini!.path,
                                            ini.sectionName,
                                            "key",
                                            e.currentTarget.value,
                                          )
                                          .then(() => {
                                            toast.success(
                                              `토글 키가 변경되었습니다`,
                                            );
                                            $data.refetch();
                                          })
                                          .catch((e: any) => {
                                            toast.error(
                                              "토글 수정중 오류 발생",
                                              {
                                                description: e.message,
                                              },
                                            );
                                          });
                                      }}
                                    />
                                  </Table.Cell>
                                </Table.Row>
                              {/each}
                            </Table.Body>
                          </Table.Root>
                        </Dialog.Description>
                      </Dialog.Header>
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
                              $data.refetch();
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
              </div>
            </div>

            <div
              class={cn(
                "relative flex justify-center items-center aspect-square duration-200 transition-all overflow-hidden",
              )}
              ondragover={(e) => {
                e.preventDefault();
                setDragState(mod.path, true);
              }}
              ondragleave={(e) => {
                e.preventDefault();
                setDragState(mod.path, false);
              }}
              ondrop={async (e) => {
                e.preventDefault();
                setDragState(mod.path, false);

                const files = e.dataTransfer?.files;
                console.log(e.dataTransfer);
                if (!files || files.length < 1) {
                  toast.warning("선택된 파일이 없습니다");
                  return;
                } else if (files?.length > 1) {
                  toast.warning("한개의 파일만 드랍할 수 있습니다");
                  return;
                }

                const file = files[0];

                if (!file.type.startsWith("image/")) {
                  toast.warning("이미지 파일만 드랍할 수 있습니다");
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
                    src={`nahida://external-image?path=${encodeURIComponent(`${mod.preview.path}`)}&t=${timestamp}`}
                    alt={mod.name}
                    loading="lazy"
                  />
                </div>
                <img
                  class="relative object-contain w-full h-full z-10"
                  src={`nahida://external-image?path=${encodeURIComponent(`${mod.preview.path}`)}&t=${timestamp}`}
                  alt={mod.name}
                  loading="lazy"
                />
                <div class="absolute left-1 top-1 z-20">
                  <PreviewModal
                    src={`nahida://external-image?path=${encodeURIComponent(`${mod.preview.path}`)}&t=${timestamp}`}
                    alt={`${mod.name} Modal`}
                    onOpenChange={() => {}}
                  />
                </div>

                <div
                  class={cn(
                    "absolute inset-0 bg-black/60 z-30 flex items-center justify-center pointer-events-none duration-200",
                    dragStates.get(mod.path) ? "opacity-100" : "opacity-0",
                  )}
                >
                  <div class="text-white text-center">
                    <span class="text-2xl">📁</span>
                    <p class="font-medium mt-2">파일을 여기에 드롭하세요</p>
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
                        toast.warning("클립보드에 이미지가 없습니다");
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
                    }}>Paste</button
                  >
                </div>

                <div
                  class={cn(
                    "absolute inset-0 bg-black/60 z-30 flex items-center justify-center pointer-events-none duration-200",
                    dragStates.get(mod.path) ? "opacity-100" : "opacity-0",
                  )}
                >
                  <div class="text-white text-center">
                    <span class="text-2xl">📁</span>
                    <p class="font-medium mt-2">파일을 여기에 드롭하세요</p>
                  </div>
                </div>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>

<AlertDialog.Root open={showOverwriteDialog}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>계속 진행</AlertDialog.Title>
      <AlertDialog.Description>
        모드 폴더에 이미 프리뷰 이미지가 있습니다. 이미지를 덮어쓸까요?
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel
        onclick={() => {
          showOverwriteDialog = false;
          fileToOverwrite = null;
          previewPathToOverwrite = null;
        }}>취소</AlertDialog.Cancel
      >
      <AlertDialog.Action
        onclick={async () => {
          if (fileToOverwrite && previewPathToOverwrite) {
            await savePreviewImage(previewPathToOverwrite, fileToOverwrite);
            showOverwriteDialog = false;
            fileToOverwrite = null;
            previewPathToOverwrite = null;
          }
        }}>계속</AlertDialog.Action
      >
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
