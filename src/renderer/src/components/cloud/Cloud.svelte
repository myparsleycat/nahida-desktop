<script lang="ts">
  import {
    ArrowUpIcon,
    ArrowDownIcon,
    FolderIcon,
    FileIcon,
    EllipsisIcon,
    LoaderIcon,
    MousePointer2Icon,
    Trash2Icon,
    DownloadIcon,
    SquarePenIcon,
    MoveIcon,
    LinkIcon,
    CopyIcon,
    UploadIcon,
    ListIcon,
    LayoutGridIcon,
    SearchIcon,
    EyeIcon,
    FileTextIcon,
    RotateCcwIcon,
    Share2Icon,
  } from "lucide-svelte";
  import { cn } from "$lib/utils";
  import { writable, derived as storeDerived, get } from "svelte/store";
  import { format } from "date-fns";
  import { ko, enUS, zhCN } from "date-fns/locale";
  import * as ContextMenu from "$lib/components/ui/context-menu";
  import * as Breadcrumb from "$lib/components/ui/breadcrumb";
  import { toast } from "svelte-sonner";
  import {
    formatSize,
    getChosung,
    getSearchScore,
    isNameConflict,
    preventEvent,
  } from "$lib/utils";
  import {
    DialogStateStore,
    LoadingStateStore,
  } from "@/lib/stores/akasha.store";
  import type { Content, LayoutType, SortType } from "../../lib/types";
  import * as Dialog from "$lib/components/ui/dialog/index";
  import { Input } from "$lib/components/ui/input";
  import { Button, buttonVariants } from "@/lib/components/ui/button";
  import { _ } from "svelte-i18n";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index";
  import * as AlertDialog from "@/lib/components/ui/alert-dialog";
  import { createQuery, createMutation } from "@tanstack/svelte-query";
  import { Cloud } from "@/lib/helpers";
  import AkashaPreviewModal from "@/components/cloud/AkashaPreviewModal.svelte";

  let currentId = Cloud.currentId;
  let uploadDragging = $state(false);
  let contentDragging = $state(false);
  let isDoubleClickAllowed = $state(true);

  let layout = $state<LayoutType>("list");

  let selectedItems = $state<Content[]>([]);
  let actionItem = $state<Content | null>(null);
  let lastSelectedIndex: number | null = null;
  let draggedItem: Content | null = null;
  let currentDragOver = $state<Content | null>(null);

  type copy_or_cut_items_Type = {
    action: "copy" | "cut" | null;
    items: Content[];
  };
  let copy_or_cut_items = $state<copy_or_cut_items_Type>({
    action: null,
    items: [],
  });

  const drive = writable<{ id: string } | null>(null);
  const parent = writable<{ id: string; name: string } | null>(null);
  const contents = writable<Content[]>([]);
  const ancestors = writable<
    { id: string; parentId: string | null; name: string; depth: number }[]
  >([]);

  const data = $derived(
    createQuery({
      queryKey: ["contents", $currentId],
      queryFn: async () => {
        const data = await Cloud.item.get($currentId);

        // if (!data || error) {
        //   // @ts-ignore
        //   throw new Error(error.value.error.message);
        // }

        return data;
      },
      refetchIntervalInBackground: true,
      refetchInterval: () => {
        if (typeof document !== "undefined" && document.hidden) {
          return 60000 * 3; // 3분 (백그라운드)
        }
        return 30000; // 30초 (포그라운드)
      },
    }),
  );

  $effect(() => {
    if ($data.data) {
      // drive.set($data.data.drive || null);
      parent.set($data.data.parent || null);
      contents.set($data.data.children || []);
      ancestors.set([...$data.data.ancestors].reverse() || []);
    }
  });

  const refetcher = async () => {
    await $data.refetch();
  };

  $effect(() => {
    if (
      selectedItems.length > 0 &&
      !$contents.some((content) =>
        selectedItems.some((item) => item.id === content.id),
      )
    ) {
      selectedItems = [];
    }
  });

  const scrollToItem = (itemId: string) => {
    const findElement = () => document.querySelector(`[data-uuid="${itemId}"]`);

    let element = findElement();

    if (element) {
      element.scrollIntoView({ behavior: "smooth", block: "center" });
      return;
    }

    const observer = new MutationObserver(() => {
      element = findElement();
      if (element) {
        observer.disconnect();
        element.scrollIntoView({ behavior: "smooth", block: "center" });
      }
    });

    observer.observe(document.body, { childList: true, subtree: true });

    const timeoutId = setTimeout(() => {
      observer.disconnect();
      // console.warn("Timeout: Element not found within 5 seconds");
    }, 5000);
  };

  const onDragEnter = (e: DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) {
      preventEvent(e);
      uploadDragging = true;
    }
  };

  const onDragLeave = (e: DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) {
      preventEvent(e);
      uploadDragging = false;
    }

    currentDragOver = null;
  };

  const onDragOver = (e: DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) {
      preventEvent(e);
      uploadDragging = true;
    }
  };

  let searchBuffer = "";
  let searchTimeout: number | undefined;

  // 버퍼 초기화 및 타이머 설정
  const resetSearchBuffer = () => {
    searchBuffer = "";
    if (searchTimeout) {
      clearTimeout(searchTimeout);
      searchTimeout = undefined;
    }
  };

  const handleKeyDown = (e: KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && e.key === "r") return;

    if (focusSearchInput) return;

    const allSortedContents = get(sortedContents);

    const currentIndex = selectedItems.length
      ? allSortedContents.findIndex((item) => item.id === selectedItems[0]?.id)
      : -1;

    if (e.key === "F2") {
      e.preventDefault();

      if (
        !selectedItems ||
        selectedItems.length === 0 ||
        selectedItems.length > 1
      )
        return;

      DialogStateStore.setOpen("renameDialog", true);
      return;
    }

    if (e.key === "ArrowDown") {
      e.preventDefault();

      if (e.ctrlKey || e.metaKey) {
        // Ctrl + 아래 화살표: isDir 항목으로 이동
        if (currentIndex !== -1 && allSortedContents[currentIndex]?.isDir) {
          currentId.set(allSortedContents[currentIndex].id);
        }
      } else {
        const nextIndex = Math.min(
          currentIndex + 1,
          allSortedContents.length - 1,
        );
        selectedItems = [allSortedContents[nextIndex]];
        lastSelectedIndex = nextIndex;

        // 선택된 항목으로 스크롤
        const element = document.getElementById(
          allSortedContents[nextIndex]?.id,
        );
        if (element) {
          element.scrollIntoView({ behavior: "smooth", block: "center" });
        }
      }
    }

    if (e.key === "ArrowUp") {
      e.preventDefault();

      if (e.ctrlKey || e.metaKey) {
        // Ctrl + 위 화살표: 상위 폴더로 이동
        if ($parent) {
          currentId.set($parent.id);
        } else {
          toast.warning("상위 폴더가 없습니다.");
        }
      } else {
        // 일반 위 화살표: 이전 항목 선택
        const prevIndex = Math.max(currentIndex - 1, 0);
        selectedItems = [allSortedContents[prevIndex]];
        lastSelectedIndex = prevIndex;

        // 선택된 항목으로 스크롤
        const element = document.getElementById(
          allSortedContents[prevIndex]?.id,
        );
        if (element) {
          element.scrollIntoView({ behavior: "smooth", block: "center" });
        }
      }
    }

    if (/^[a-zA-Z0-9]$/.test(e.key) && !(e.ctrlKey || e.metaKey)) {
      e.preventDefault();

      // 알파벳 입력: 해당 이름으로 시작하는 첫 번째 아이템 선택
      const pressedKey = e.key.toLowerCase();

      // 기존 검색 버퍼에 추가
      searchBuffer += pressedKey;

      // 타이머 초기화
      if (searchTimeout) {
        clearTimeout(searchTimeout);
      }
      searchTimeout = window.setTimeout(() => {
        resetSearchBuffer();
      }, 500);

      const firstMatchedItem = allSortedContents.find((item) =>
        item.name.toLowerCase().startsWith(searchBuffer),
      );

      if (firstMatchedItem) {
        selectedItems = [firstMatchedItem];
        lastSelectedIndex = allSortedContents.indexOf(firstMatchedItem);

        // 선택된 항목으로 스크롤
        const element = document.querySelector(
          `[data-uuid="${firstMatchedItem.id}"]`,
        );
        if (element) {
          element.scrollIntoView({ behavior: "smooth", block: "center" });
        }
      }
    }

    if (e.key === "Escape") {
      e.preventDefault();

      selectedItems = [];
      lastSelectedIndex = null;
      copy_or_cut_items = { action: null, items: [] };
    }

    if ((e.ctrlKey || e.metaKey) && e.key === "a") {
      e.preventDefault();

      selectedItems = $contents;
    }

    if ((e.ctrlKey || e.metaKey) && e.key === "c") {
      e.preventDefault();

      if (selectedItems.length >= 1) {
        toast.warning("복사는 지원하지 않습니다");
      }
    }

    if ((e.ctrlKey || e.metaKey) && e.key === "x") {
      e.preventDefault();

      copy_or_cut_items = { action: "cut", items: selectedItems };
      if (selectedItems.length === 1) {
        toast.info(
          `"${selectedItems[0].name}"이(가) 잘라내기 상태로 설정되었습니다`,
        );
      } else if (selectedItems.length > 1) {
        toast.info(
          `"${copy_or_cut_items.items[0].name}"외 ${
            copy_or_cut_items.items.length - 1
          }개가 잘라내기 상태로 설정되었습니다.`,
        );
      }
    }

    if ((e.ctrlKey || e.metaKey) && e.key === "v") {
      e.preventDefault();

      if (!copy_or_cut_items.action || copy_or_cut_items.items.length < 1)
        return;

      if (copy_or_cut_items.action === "cut") {
        // 기존 items 저장
        const itemsToMove = [...copy_or_cut_items.items];

        // moveItems 호출 전에 초기화
        copy_or_cut_items = { action: null, items: [] };

        // moveItems(itemsToMove, currentId)
        //   .then(async () => {
        //     await refetcher();
        //   })
        //   .catch((err: any) => {
        //     console.error("붙여넣기 실패:", err);
        //     toast.error("붙여넣기 중 오류가 발생했습니다.", {
        //       description: err.message,
        //     });
        //   });
      }
    }

    if ((e.ctrlKey || e.metaKey) && e.key === "f") {
      e.preventDefault();
      $DialogStateStore.searchCommand.open =
        !$DialogStateStore.searchCommand.open;
    }
  };

  const handleClickOutside = (_: MouseEvent) => {
    selectedItems = [];
    lastSelectedIndex = null;
  };

  // const handleRightClick = (e: MouseEvent, item?: Content) => {};

  // 드래그 시작 시 호출되는 함수
  const handleDragStart = (e: DragEvent, item: Content) => {
    draggedItem = item;
    e.dataTransfer?.setData("text/plain", item.id);
    e.dataTransfer?.setDragImage(e.currentTarget as HTMLElement, 0, 0);
  };

  // 드롭 대상에서 dragover 시 호출되는 함수
  const handleDragOver = (e: DragEvent, targetItem: Content) => {
    e.preventDefault();
    currentDragOver = targetItem;
    if (targetItem.isDir) {
      e.dataTransfer!.dropEffect = "move";
    } else {
      e.dataTransfer!.dropEffect = "none";
    }
  };

  const sortType = writable<SortType>("NAME:ASC");

  const handleSort = (field: "NAME" | "SIZE" | "DATE") => {
    sortType.update((current) => {
      if (!current.startsWith(field)) {
        return `${field}:DESC`;
      } else if (current === `${field}:DESC`) {
        return `${field}:ASC`;
      } else {
        return `${field}:DESC`;
      }
    });
  };

  let searchInDirQuery = writable("");
  let focusSearchInput = $state(false);

  const rawContents = storeDerived(
    [sortType, contents],
    ([$sortType, $contents]) => {
      if (!$contents) return [];

      return [...$contents].sort((a, b) => {
        // 디렉토리를 우선적으로 정렬
        if (a.isDir && !b.isDir) return -1;
        if (!a.isDir && b.isDir) return 1;

        const [field, order] = $sortType.split(":");
        const multiplier = order === "DESC" ? -1 : 1;

        switch (field) {
          case "NAME":
            // natural sort를 위한 비교 함수
            const naturalCompare = (a: string, b: string) => {
              const chunk = /(\d+)|(\D+)/g;
              const chunks1 = String(a).match(chunk);
              const chunks2 = String(b).match(chunk);

              if (!chunks1 || !chunks2) return a.localeCompare(b);

              const len = Math.min(chunks1.length, chunks2.length);

              for (let i = 0; i < len; i++) {
                const c1 = chunks1[i];
                const c2 = chunks2[i];
                const isNum1 = !isNaN(Number(c1));
                const isNum2 = !isNaN(Number(c2));

                if (isNum1 && isNum2) {
                  const diff = Number(c1) - Number(c2);
                  if (diff !== 0) return multiplier * diff;
                } else {
                  const diff = c1.localeCompare(c2);
                  if (diff !== 0) return multiplier * diff;
                }
              }

              return multiplier * (chunks1.length - chunks2.length);
            };

            return naturalCompare(a.name, b.name);
          case "SIZE":
            if (a.isDir && !b.isDir) return -1;
            if (!a.isDir && b.isDir) return 1;
            return multiplier * ((Number(a.size) || 0) - (Number(b.size) || 0));
          case "DATE":
            return (
              multiplier *
              (new Date(a.updatedAt).getTime() -
                new Date(b.updatedAt).getTime())
            );
          default:
            return 0;
        }
      });
    },
  );

  const sortedContents = storeDerived(
    [rawContents, searchInDirQuery],
    ([$sortedContents, $searchInDirQuery]) => {
      if (!$sortedContents) return [];
      if (!$searchInDirQuery) return $sortedContents;

      const query = $searchInDirQuery.toLowerCase();
      const isChosungSearch = /^[ㄱ-ㅎ]+$/.test(query);

      return $sortedContents
        .map((item) => ({
          item,
          score: getSearchScore(item.name, query),
        }))
        .filter(({ item, score }) => {
          if (score > 0) return true;

          if (isChosungSearch) {
            const itemChosung = getChosung(item.name.toLowerCase());
            return itemChosung.includes(query);
          }

          return false;
        })
        .sort((a, b) => b.score - a.score)
        .map(({ item }) => item);
    },
  );

  export const getItem = (
    uuid: string | undefined | null,
  ): Content | undefined => {
    const allContents = get(contents);
    return allContents.find((content) => content.id === uuid);
  };

  const handleItemClick = async (
    item: Content,
    index: number,
    event: MouseEvent,
  ) => {
    if (event.shiftKey && lastSelectedIndex !== null) {
      // Shift-click: Select range
      const start = Math.min(lastSelectedIndex, index);
      const end = Math.max(lastSelectedIndex, index);
      const newSelections = $sortedContents.slice(start, end + 1);

      // Merge with existing selections if Ctrl/Cmd is held
      if (event.metaKey || event.ctrlKey) {
        selectedItems = Array.from(
          new Set([...selectedItems, ...newSelections]),
        );
      } else {
        selectedItems = newSelections;
      }
    } else if (event.metaKey || event.ctrlKey) {
      // Ctrl/Cmd-click: Toggle selection
      if (selectedItems.includes(item)) {
        selectedItems = selectedItems.filter(
          (selected) => selected.id !== item.id,
        );
      } else {
        selectedItems = [...selectedItems, item];
      }
      lastSelectedIndex = index;
    } else {
      // Regular click: Select single item
      selectedItems = [item];
      lastSelectedIndex = index;
    }
  };

  const handleItemRightClick = async (e: MouseEvent, item: Content) => {
    e.preventDefault();
    if (selectedItems.length <= 1) {
      selectedItems = [item];
    }
  };

  const blockDoubleClick = (time: number = 500) => {
    isDoubleClickAllowed = false;
    setTimeout(() => {
      isDoubleClickAllowed = true;
    }, time);
  };

  const handleItemDoubleClick = async (item: Content) => {
    if (item.isDir) {
      currentId.set(item.id);
    } else {
      if (item.mimeType?.startsWith("text")) {
        // textViewerStore.openTextViewer(item);
      } else {
        // await DownloadItem({ item });
      }
    }
  };

  const handleCopyId = (item: Content) => {
    navigator.clipboard.writeText(item.id).then(() => {
      toast.success($_("drive.toast.copied_to_clipboard"));
    });
  };

  const handleDrop = (e: DragEvent, targetItem: Content) => {
    try {
      e.preventDefault();
      const draggedId = e.dataTransfer?.getData("text/plain");
      if (!draggedId || !draggedItem) return;

      if (!targetItem.isDir)
        return toast.warning("폴더에만 드롭할 수 있습니다.");

      if (draggedItem.id === targetItem.id)
        return toast.warning("자기 자신에게는 드롭할 수 없습니다.");

      // moveItems([draggedItem], targetItem.id);
    } finally {
      currentDragOver = null;
    }
  };
</script>

<svelte:window
  onkeydown={(e) => {
    if (!get(DialogStateStore.anyDialogOpen)) {
      handleKeyDown(e);
    }
  }}
/>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="w-full h-full flex flex-col select-none">
  <!-- Breadcrumb 영역: 고정 높이 -->
  <div class="h-11 flex items-center p-4 border-b w-full">
    <div class="flex-1 min-w-0">
      <Breadcrumb.Root>
        <Breadcrumb.List
          class="flex flex-nowrap overflow-hidden whitespace-nowrap"
        >
          <Breadcrumb.Item>
            {#if $currentId === "root"}
              <Breadcrumb.Page>{$_("drive.ui.drive")}</Breadcrumb.Page>
            {:else}
              <Breadcrumb.Link
                class="cursor-pointer"
                onclick={() => {
                  currentId.set("root");
                }}
              >
                {$_("drive.ui.drive")}
              </Breadcrumb.Link>
            {/if}
          </Breadcrumb.Item>
          {#each $ancestors as anc, idx}
            <Breadcrumb.Separator />

            <ContextMenu.Root>
              <ContextMenu.Trigger class="cursor-pointer">
                <Breadcrumb.Item>
                  {#if anc.id === $currentId}
                    <Breadcrumb.Page
                      class="text-nowrap overflow-hidden text-ellipsis max-w-xs"
                      >{anc.name}</Breadcrumb.Page
                    >
                  {:else}
                    <Breadcrumb.Link
                      class="text-nowrap overflow-hidden text-ellipsis max-w-xs"
                      onclick={() => {
                        currentId.set(anc.id);
                      }}
                    >
                      {anc.name}
                    </Breadcrumb.Link>
                  {/if}
                </Breadcrumb.Item>
              </ContextMenu.Trigger>
              <ContextMenu.Content>
                <ContextMenu.Item class="cursor-pointer"
                  >Clear Prefix</ContextMenu.Item
                >
              </ContextMenu.Content>
            </ContextMenu.Root>
          {/each}
        </Breadcrumb.List>
      </Breadcrumb.Root>
    </div>

    <div class="flex gap-1 ml-4 flex-shrink-0">
      <div class="w-full relative flex items-center">
        <SearchIcon
          class="w-5 h-5 absolute left-2.5 top-2.5 text-gray-500 dark:text-gray-400"
        />
        <Input
          class="pl-8 w-[200px] border-none h-7"
          placeholder={$_("drive.ui.search_in_dir_placeholder")}
          bind:value={$searchInDirQuery}
          onfocus={() => (focusSearchInput = true)}
          onblur={() => (focusSearchInput = false)}
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

      <DropdownMenu.Root>
        <DropdownMenu.Trigger class={buttonVariants({ variant: "ghost" })}>
          {$_("g.make_new")}
        </DropdownMenu.Trigger>
        <DropdownMenu.Content>
          <DropdownMenu.Item
            class="gap-3 cursor-pointer"
            onclick={() => DialogStateStore.setOpen("createDirDialog", true)}
          >
            <FolderIcon size={20} />
            {$_("drive.ui.new_dir")}
          </DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Group>
            <DropdownMenu.Item class="gap-3 cursor-pointer">
              <UploadIcon size={20} />
              {$_("drive.upload_dir")}
            </DropdownMenu.Item>
            <DropdownMenu.Item class="gap-3 cursor-pointer">
              <UploadIcon size={20} />
              {$_("drive.upload_file")}
            </DropdownMenu.Item>
          </DropdownMenu.Group>
        </DropdownMenu.Content>
      </DropdownMenu.Root>
    </div>
  </div>

  <!-- 콘텐츠 영역 -->
  <div
    class="ctx flex flex-col flex-1 overflow-auto"
    ondragenter={onDragEnter}
    ondragleave={onDragLeave}
    ondragover={onDragOver}
  >
    <!-- 정렬 버튼들이 있는 상단 부분 -->
    {#if layout === "list"}
      <div class="flex flex-row text-sm">
        <div
          class="flex flex-row w-full h-8 items-center select-none gap-3 px-3"
        >
          <!-- 이름 정렬 -->
          <button
            class="flex flex-row grow items-center"
            onclick={() => handleSort("NAME")}
          >
            <div
              class={cn(
                "flex flex-row gap-2 items-center",
                $sortType.startsWith("NAME")
                  ? "text-primary"
                  : "text-muted-foreground",
              )}
            >
              <p class="dragselect-start-disallowed line-clamp-1 text-ellipsis">
                {$_("drive.ui.name")}
              </p>
              {#if $sortType === "NAME:DESC"}
                <ArrowDownIcon size="16" />
              {:else if $sortType === "NAME:ASC"}
                <ArrowUpIcon size="16" />
              {/if}
            </div>
          </button>

          <!-- 크기 정렬 -->
          <button
            class="flex flex-row items-center"
            onclick={() => handleSort("SIZE")}
          >
            <div
              class={cn(
                "flex flex-row gap-2 items-center",
                $sortType.startsWith("SIZE")
                  ? "text-primary"
                  : "text-muted-foreground",
              )}
            >
              <p class="dragselect-start-disallowed line-clamp-1 text-ellipsis">
                {$_("drive.ui.size")}
              </p>
              {#if $sortType === "SIZE:DESC"}
                <ArrowDownIcon size="16" />
              {:else if $sortType === "SIZE:ASC"}
                <ArrowUpIcon size="16" />
              {/if}
            </div>
          </button>

          <!-- 날짜 정렬 -->
          <button
            class="flex flex-row items-center"
            onclick={() => handleSort("DATE")}
          >
            <div
              class={cn(
                "flex flex-row gap-2 items-center",
                $sortType.startsWith("DATE")
                  ? "text-primary"
                  : "text-muted-foreground",
              )}
            >
              <p class="dragselect-start-disallowed line-clamp-1 text-ellipsis">
                {$_("drive.ui.date")}
              </p>
              {#if $sortType === "DATE:DESC"}
                <ArrowDownIcon size="16" />
              {:else if $sortType === "DATE:ASC"}
                <ArrowUpIcon size="16" />
              {/if}
            </div>
          </button>
        </div>
      </div>
    {/if}

    <ContextMenu.Root>
      <!-- 실제 파일/폴더 목록 -->
      {#if layout === "list"}
        <ContextMenu.Trigger
          class="flex-grow overflow-y-auto overflow-x-hidden border-none outline-none"
        >
          <div class="flex-1 flex flex-col h-full">
            {#if $sortedContents.length > 0}
              {#each $sortedContents as item, index (item.id)}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <div
                  data-uuid={item.id}
                  class={cn(
                    "flex flex-row items-center px-3 py-2 hover:bg-secondary cursor-pointer gap-4",
                    selectedItems.some((selected) => selected.id === item.id) &&
                      "bg-secondary",
                    currentDragOver?.id === item.id && "bg-secondary",
                  )}
                  draggable="true"
                  ondragstart={(e) => handleDragStart(e, item)}
                  ondragover={(e) => handleDragOver(e, item)}
                  ondrop={(e) => handleDrop(e, item)}
                  onclick={(e) => handleItemClick(item, index, e)}
                  ondblclick={() => {
                    if (
                      !get(DialogStateStore.anyDialogOpen) &&
                      isDoubleClickAllowed
                    ) {
                      handleItemDoubleClick(item);
                      isDoubleClickAllowed = false;
                      setTimeout(() => {
                        isDoubleClickAllowed = true;
                      }, 500);
                    }
                  }}
                  oncontextmenu={(e) => handleItemRightClick(e, item)}
                >
                  <!-- 파일/폴더 아이콘 및 이름 -->
                  <div class="flex flex-row items-center gap-2">
                    <div class="size-12 flex text-muted-foreground p-0.5">
                      {#if $data.isFetching && $currentId === item.id}
                        <div
                          class="w-full h-full flex items-center justify-center"
                        >
                          <LoaderIcon class="animate-spin-1.5" size="20" />
                        </div>
                      {:else if item.isDir && !item.preview}
                        <FolderIcon class="text-yellow-400 w-full h-full" />
                      {:else if item.preview}
                        {#if item.preview}
                          <AkashaPreviewModal
                            className="w-14"
                            img={item.preview.img}
                            alt={item.name}
                            type="list"
                            onOpenChange={(v) =>
                              DialogStateStore.setOpen("previewDialog", v)}
                          />
                        {:else}
                          <div
                            class="w-full h-full flex items-center justify-center"
                          >
                            <LoaderIcon class="animate-spin-1.5" size="20" />
                          </div>
                        {/if}
                      {:else if item.mimeType?.startsWith("text")}
                        <FileTextIcon class="text-blue-400 w-full h-full" />
                      {:else}
                        <FileIcon class="w-full h-full" />
                      {/if}
                    </div>
                  </div>

                  <!-- 이름 -->
                  <div class="flex flex-row items-center gap-2 w-full min-w-0">
                    <div class="grow min-w-0">
                      <span class="line-clamp-1 truncate block w-full"
                        >{item.name}</span
                      >
                    </div>
                  </div>

                  <!-- 크기 -->
                  <div class="flex flex-row items-center gap-2">
                    <div class="min-w-0 text-sm text-muted-foreground">
                      <span class="line-clamp-1 truncate block w-full">
                        {formatSize(Number(item.size))}
                      </span>
                    </div>
                  </div>

                  <!-- 업데이트 날짜 -->
                  <div
                    class="w-38 text-right text-sm text-muted-foreground text-nowrap"
                  >
                    <!-- {format(item.updatedAt, "yyyy년 MM월 dd일 hh시 mm분")} -->
                    {format(item.updatedAt, "PPP", { locale: ko })}
                  </div>
                </div>
              {/each}
            {:else if $data.isFetched && $sortedContents.length < 1}
              <div
                class="flex flex-row items-center justify-center w-full h-full select-none"
              >
                <div class="flex flex-col p-4 justify-center items-center">
                  <div>
                    <FolderIcon size="100" />
                  </div>
                  <p class="text-xl text-center mt-4">
                    {$_("drive.ui.no_contents_section_message.0")}
                  </p>
                  <p class="text-muted-foreground text-center">
                    {$_("drive.ui.no_contents_section_message.1")}
                  </p>
                </div>
              </div>
            {:else}
              <div class="flex h-full w-full justify-center items-center">
                <LoaderIcon class="animate-spin-1.5" size="70" />
              </div>
            {/if}

            <!-- 클릭시 선택 해제 영역 -->
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <div
              class="flex-grow"
              onclick={handleClickOutside}
              oncontextmenu={handleClickOutside}
            ></div>
          </div>
        </ContextMenu.Trigger>
      {:else if layout === "grid"}
        <!-- 그리드 뷰: CSS grid를 사용 -->
        <div class="p-4 overflow-auto">
          {#if $sortedContents.length > 0}
            <div
              class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6 gap-4"
            >
              {#each $sortedContents as item (item.id)}
                <ContextMenu.Trigger>
                  <!-- svelte-ignore a11y_click_events_have_key_events -->
                  <div
                    data-uuid={item.id}
                    class={cn(
                      "border rounded p-2 hover:bg-secondary cursor-pointer",
                      selectedItems.some(
                        (selected) => selected.id === item.id,
                      ) && "bg-secondary",
                    )}
                    draggable="true"
                    ondragstart={(e) => handleDragStart(e, item)}
                    ondragover={(e) => handleDragOver(e, item)}
                    ondrop={(e) => handleDrop(e, item)}
                    onclick={(e) => handleItemClick(item, 0, e)}
                    ondblclick={() => {
                      if (
                        !get(DialogStateStore.anyDialogOpen) &&
                        isDoubleClickAllowed
                      ) {
                        handleItemDoubleClick(item);
                        blockDoubleClick();
                      }
                    }}
                    oncontextmenu={(e) => handleItemRightClick(e, item)}
                  >
                    <!-- 아이콘/미리보기 영역 -->
                    <div
                      class="relative flex justify-center items-center aspect-square"
                    >
                      {#if $data.isFetching && $currentId === item.id}
                        <LoaderIcon class="animate-spin-1.5" size="32" />
                      {:else if item.isDir && !item.preview}
                        <FolderIcon class="text-yellow-400 p-4" size="100" />
                      {:else if item.preview}
                        <img
                          class="relative object-contain w-full h-full"
                          src={item.preview.img.cover}
                          alt={item.name}
                          loading="lazy"
                        />
                      {:else}
                        <FileIcon class="text-blue-400" size="32" />
                      {/if}

                      <div
                        class="absolute flex flex-row justify-center items-center bottom-0 left-1/2 -translate-x-1/2 w-full"
                      >
                        <div
                          class="flex flex-row h-full items-center rounded-full px-[8px] py-[3px] justify-center gap-2 bg-zinc-100 dark:bg-zinc-900"
                        >
                          <p
                            class="dragselect-start-disallowed line-clamp-1 text-ellipsis break-all text-sm text-primary"
                          >
                            {item.name}
                          </p>
                        </div>
                      </div>
                    </div>
                  </div>
                </ContextMenu.Trigger>
              {/each}
            </div>
          {:else if $data.isFetched && $sortedContents.length < 1}
            <div class="flex flex-col items-center justify-center h-full">
              <FolderIcon size="100" />
              <p class="mt-4 text-xl text-center">
                {$_("drive.ui.no_contents_section_message.0")}
              </p>
              <p class="text-muted-foreground text-center">
                {$_("drive.ui.no_contents_section_message.1")}
              </p>
            </div>
          {:else}
            <div class="flex h-full w-full justify-center items-center">
              <LoaderIcon class="animate-spin-1.5" size="70" />
            </div>
          {/if}
        </div>
      {/if}

      <!-- ContextMenu 내용 -->
      <ContextMenu.Content class="flex-grow">
        {#if selectedItems && selectedItems.length !== 0}
          {#if selectedItems.length === 1}
            {#if selectedItems[0].isDir}
              <ContextMenu.Item
                class="cursor-pointer gap-x-2"
                onclick={() => {
                  currentId.set(selectedItems[0].id);
                }}
              >
                <MousePointer2Icon size={18} />
                {$_("drive.ui.context_menu.open")}
              </ContextMenu.Item>
              <ContextMenu.Separator />
            {/if}

            {#if selectedItems[0].mimeType?.startsWith("text") || selectedItems[0].mimeType?.startsWith("image")}
              <ContextMenu.Item
                class="cursor-pointer gap-x-2"
                onclick={() => {
                  if (selectedItems[0].mimeType?.startsWith("text")) {
                    // textViewerStore.openTextViewer(selectedItems[0]);
                  } else if (selectedItems[0].mimeType?.startsWith("image")) {
                  }
                }}
              >
                <EyeIcon size={18} />
                {$_("drive.ui.context_menu.preview")}
              </ContextMenu.Item>
            {/if}

            <ContextMenu.Item
              class="cursor-pointer gap-x-2"
              onclick={() => {
                Cloud.item.download
                  .enqueue(selectedItems[0].id)
                  .catch((e: any) => {
                    toast.error(e.message);
                  });
              }}
            >
              <DownloadIcon size={18} />
              {$_("drive.ui.context_menu.download")}
            </ContextMenu.Item>
            <ContextMenu.Separator />
            <ContextMenu.Item
              class="cursor-pointer gap-x-2"
              onclick={() => {
                if (selectedItems[0]) {
                  DialogStateStore.setOpen("shareDialog", true, {
                    id: selectedItems[0].id,
                  });
                } else {
                  toast.warning("선택된 항목이 없습니다");
                }
              }}
            >
              <Share2Icon size={18} />
              {$_("drive.ui.context_menu.share")}
            </ContextMenu.Item>
            <ContextMenu.Separator />
            <ContextMenu.Item
              class="cursor-pointer gap-x-2"
              onclick={() => DialogStateStore.setOpen("renameDialog", true)}
            >
              <SquarePenIcon size={18} />
              {$_("drive.ui.rename")}
            </ContextMenu.Item>
            <ContextMenu.Separator />
            <ContextMenu.Item
              class="cursor-pointer gap-x-2"
              onclick={() => handleCopyId(selectedItems[0])}
            >
              <CopyIcon size={18} />
              {$_("drive.ui.context_menu.copy_id")}
            </ContextMenu.Item>
            <ContextMenu.Separator />
          {/if}
          {#if selectedItems.every( (item) => item.mimeType?.startsWith("image"), )}
            <ContextMenu.Item class="cursor-pointer gap-x-2">
              <RotateCcwIcon size={18} />
              RG
            </ContextMenu.Item>
          {/if}
          <ContextMenu.Item class="cursor-pointer gap-x-2 text-red-500">
            <Trash2Icon size={18} />
            {$_("drive.ui.trash")}
          </ContextMenu.Item>
        {:else}
          <ContextMenu.Item
            class="cursor-pointer gap-x-2"
            onclick={() => DialogStateStore.setOpen("createDirDialog", true)}
          >
            <FolderIcon size={18} />
            {$_("drive.ui.new_dir")}
          </ContextMenu.Item>
        {/if}
      </ContextMenu.Content>
    </ContextMenu.Root>
  </div>
</div>

{#if uploadDragging}
  <div
    class="absolute pointer-events-none inset-0 bg-primary/10 border-2 border-dashed border-primary/50 rounded-lg flex items-center justify-center"
  >
    <div class="bg-background/90 p-4 rounded-lg shadow-lg">
      <p class="text-lg font-medium">
        {$_("drive.ui.dir_drop_here_section_message.0")}
      </p>
    </div>
  </div>
{/if}

<!-- <PubLinkDialog /> -->

<Dialog.Root
  open={$DialogStateStore.renameDialog.open}
  onOpenChange={(v) => DialogStateStore.setOpen("renameDialog", v)}
>
  <Dialog.Content>
    <Dialog.Header>
      <Dialog.Title>{$_("drive.ui.rename")}</Dialog.Title>
    </Dialog.Header>
    <form class="flex flex-col space-y-4" autocomplete="off">
      <div class="flex flex-row gap-x-4">
        <input
          class={cn(
            "block w-full rounded-lg border-none bg-black/5 dark:bg-white/5 py-2 px-3 text-sm/6 text-black dark:text-white",
            "focus:outline-2 focus:-outline-offset-2 focus:outline-black/25 focus:dark:outline-white/25",
          )}
          name="name"
          placeholder="이름"
          maxlength={200}
          required
          defaultValue={selectedItems.length === 1 &&
          !selectedItems[0].isDir &&
          selectedItems[0].name.includes(".")
            ? selectedItems[0].name.split(".").slice(0, -1).join(".")
            : selectedItems.length === 1
              ? selectedItems[0].name
              : ""}
        />
        {#if !selectedItems[0].isDir}
          <input
            class={cn(
              "block w-1/4 rounded-lg border-none bg-black/5 dark:bg-white/5 py-2 px-3 text-sm/6 text-black dark:text-white",
              "focus:outline-2 focus:-outline-offset-2 focus:outline-black/25 focus:dark:outline-white/25",
            )}
            name="ext"
            placeholder="확장자"
            maxlength={50}
            defaultValue={selectedItems.length === 1 &&
            selectedItems[0].name.includes(".")
              ? "." + selectedItems[0].name.split(".").pop()
              : ""}
          />
        {/if}
      </div>
      <div
        class="flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2"
      >
        <Button
          variant="outline"
          onclick={() => DialogStateStore.setOpen("renameDialog", false)}
        >
          {$_("g.cancel")}
        </Button>
        <Button type="submit" disabled={$LoadingStateStore.renameLoading}>
          {$_("drive.ui.rename")}
        </Button>
      </div>
    </form>
  </Dialog.Content>
</Dialog.Root>

<Dialog.Root
  open={$DialogStateStore.createDirDialog.open}
  onOpenChange={(v) => DialogStateStore.setOpen("createDirDialog", v)}
>
  <Dialog.Content>
    <Dialog.Header>
      <Dialog.Title>{$_("drive.ui.new_dir")}</Dialog.Title>
    </Dialog.Header>
    <form
      class="flex flex-col space-y-4"
      autocomplete="off"
      onsubmit={(e) => {
        // CreateDir(e, refetcher, page.url.pathname.split("/").pop());

        e.preventDefault();

        const form = e.target as HTMLFormElement;
        const formData = new FormData(form);
        const name = formData.get("name") as string;

        // const validate_result = ValidateName(name);
        // if (validate_result) {
        //   return toast.warning(get(_)("#.CreateDir.0"), {
        //     description: validate_result,
        //   });
        // }

        // $CreateDirMutation.mutate({ name, parent: currentId, refetcher });
      }}
    >
      <Input name="name" placeholder="이름" maxlength={255} required />
      <div
        class="flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2"
      >
        <Button
          variant="outline"
          onclick={() => DialogStateStore.setOpen("createDirDialog", false)}
        >
          {$_("g.cancel")}
        </Button>
        <Button type="submit" disabled={$LoadingStateStore.createDirLoading}>
          {$_("drive.ui.new_dir")}
        </Button>
      </div>
    </form>
  </Dialog.Content>
</Dialog.Root>

<AlertDialog.Root
  open={$DialogStateStore.conflictNameDialog.open}
  onOpenChange={(v) => DialogStateStore.setOpen("conflictNameDialog", v)}
>
  <!-- <AlertDialog.Trigger asChild let:builder>
      <Button builders={[builder]} variant="destructive">
        {$_("drive.ui.settings_page.preview_cache.ui.action")}
      </Button>
    </AlertDialog.Trigger> -->
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>중복되는 이름의 디렉토리가 있습니다</AlertDialog.Title>
      <AlertDialog.Description>
        해당 디렉토리로 병합할까요?
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel>{$_("g.cancel")}</AlertDialog.Cancel>
      <AlertDialog.Action
        onclick={() => {
          DialogStateStore.resolveDialog("conflictNameDialog", true);
        }}
      >
        병합
      </AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root
  open={$DialogStateStore.clearPrefixDialog.open}
  onOpenChange={(v) => DialogStateStore.setOpen("clearPrefixDialog", v)}
>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>접두사 정리</AlertDialog.Title>
      <AlertDialog.Description>
        현재 선택된 폴더의 직계 자식 항목들의 이름에서 대소문자 구분 없이
        DISABLED 접두사를 제거합니다.
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel>{$_("g.cancel")}</AlertDialog.Cancel>
      <AlertDialog.Action
        onclick={() => {
          DialogStateStore.resolveDialog("clearPrefixDialog", true);
        }}
      >
        {$_("g.continue")}
      </AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>

<!-- <TextViewer />

<SearchCommand /> -->
