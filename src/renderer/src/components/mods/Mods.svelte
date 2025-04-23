<script lang="ts">
  import { Button, buttonVariants } from "$lib/components/ui/button";
  import * as Resizable from "$lib/components/ui/resizable";
  import * as Dialog from "$lib/components/ui/dialog";
  import { Mods } from "@/lib/helpers";
  import { cn } from "@/lib/utils";
  import { Input } from "@/lib/components/ui/input";
  import { Label } from "@/lib/components/ui/label";
  import {
    DotIcon,
    EditIcon,
    FolderPenIcon,
    FolderPlusIcon,
    FolderSyncIcon,
    Trash2Icon,
    WrenchIcon,
  } from "lucide-svelte";
  import { toast } from "svelte-sonner";
  import Folder from "./Folder.svelte";
  import * as ContextMenu from "$lib/components/ui/context-menu";
  import autoAnimate from "@formkit/auto-animate";

  let size = Mods.resizableSize;
  let currentFolderPath = Mods.currentFolderPath;
  let folders = Mods.folders;
  let folderChildren = Mods.folderChildren;
  let currentCharPath = Mods.currentCharPath;

  let open = $state(false);
  let temp_name = $state<string | null>(null);
  let temp_path = $state<string | null>(null);

  const clear_temp = () => {
    temp_name = null;
    temp_path = null;
  };

  const getResizableSize = async () => {
    size.set(await Mods.ui.resizable.get());
  };

  const getFolderChildren = async () => {
    Mods.getDirectChildren($currentFolderPath).then((res) => {
      folderChildren.set(res);
    });
  };

  const getFolders = async () => {
    folders.set(await Mods.folder.getAll());
  };

  $effect(() => {
    getResizableSize();
    getFolders();
  });

  $effect(() => {
    if ($currentFolderPath) {
      getFolderChildren();
    } else {
      folderChildren.set([]);
    }
  });
</script>

<div class="flex flex-row w-full h-full">
  <Resizable.PaneGroup direction="horizontal">
    <Resizable.Pane
      defaultSize={$size}
      minSize={20}
      maxSize={35}
      onResize={async (size) => {
        Mods.ui.resizable.set(size);
      }}
    >
      <div
        class="flex-col justify-center p-2 duration-200 space-y-1 select-none h-full overflow-y-auto overflow-x-hidden"
      >
        {#each $folders as folder}
          <ContextMenu.Root>
            <ContextMenu.Trigger>
              <button
                draggable="true"
                class={cn(
                  "flex gap-2 p-2 rounded-lg w-full hover:bg-muted duration-200",
                  $currentFolderPath === folder.path && "bg-muted",
                )}
                onclick={() => {
                  if ($currentFolderPath) {
                    $currentFolderPath = "";
                  } else {
                    currentFolderPath.set(folder.path);
                  }
                }}
              >
                {folder.name}
              </button>
            </ContextMenu.Trigger>
            <ContextMenu.Content>
              <ContextMenu.Item class="flex items-center gap-2 cursor-pointer">
                <FolderPenIcon size={18} />
                이름 변경
              </ContextMenu.Item>
              <ContextMenu.Separator />
              <ContextMenu.Item class="flex items-center gap-2 cursor-pointer">
                <WrenchIcon size={18} />
                픽스
              </ContextMenu.Item>
              <ContextMenu.Item class="flex items-center gap-2 cursor-pointer">
                <FolderSyncIcon size={18} />
                자동 백업
              </ContextMenu.Item>
              <ContextMenu.Separator />
              <ContextMenu.Item class="flex items-center gap-2 cursor-pointer">
                <Trash2Icon size={18} class="text-destructive" />
                목록에서 삭제
              </ContextMenu.Item>
            </ContextMenu.Content>
          </ContextMenu.Root>

          <div
            class="flex flex-col gap-1 ml-4"
            use:autoAnimate={{ easing: "ease-in-out" }}
          >
            {#if folder.path.includes($currentFolderPath)}
              {#each $folderChildren as char}
                <button
                  class="flex w-full items-center min-h-8"
                  onclick={() => {
                    currentCharPath.set(char.path);
                  }}
                  use:autoAnimate={{ duration: 150 }}
                >
                  {#if char.path === $currentCharPath}
                    <div class="flex-shrink-0">
                      <DotIcon size={30} color="green" />
                    </div>
                  {/if}
                  <p class="truncate overflow-hidden whitespace-nowrap">
                    {char.name}
                  </p>
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
            항목 생성
          </Dialog.Trigger>
          <Dialog.Content class="sm:max-w-[425px]">
            <Dialog.Header>
              <Dialog.Title>새 항목 생성</Dialog.Title>
              <Dialog.Description>
                그룹 또는 폴더 이름을 작성하고 버튼을 누르세요.
              </Dialog.Description>
            </Dialog.Header>
            <div class="grid gap-4 py-4">
              <div class="flex w-full max-w-sm flex-col gap-1.5">
                <Label for="name">이름</Label>
                <Input id="name" placeholder="이름" bind:value={temp_name} />
              </div>
              <div class="flex w-full max-w-sm flex-col gap-1.5">
                <Label for="path">경로</Label>
                <div class="flex justify-center items-center gap-2">
                  <Input id="path" readonly bind:value={temp_path} />
                  <Button
                    variant="outline"
                    size="default"
                    onclick={async () => {
                      temp_path = await window.api.fs.select({
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
                    toast.warning("이름을 지정해주세요");
                    return;
                  } else if (!temp_path) {
                    toast.warning("경로를 선택헤주세요");
                    return;
                  }

                  Mods.folder
                    .create(temp_name, temp_path)
                    .then(() => {
                      getFolders().then(() => {
                        toast.success("생성되었습니다");
                        clear_temp();
                        open = false;
                      });
                    })
                    .catch((e: any) => {
                      toast.error("항목 생성중 오류 발생", {
                        description: e.message,
                      });
                    });
                }}>생성</Button
              >
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog.Root>
      </div>
    </Resizable.Pane>

    <Resizable.Handle withHandle />

    <Resizable.Pane>
      <Folder></Folder>
    </Resizable.Pane>
  </Resizable.PaneGroup>
</div>
