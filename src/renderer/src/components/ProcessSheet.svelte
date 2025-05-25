<script lang="ts">
  import * as Sheet from "$lib/components/ui/sheet";
  import Progress from "@/lib/components/ui/progress/progress.svelte";
  import {
    ArrowUpDownIcon,
    CircleXIcon,
    FolderIcon,
    FileIcon,
    LoaderIcon,
    PauseIcon,
    PlayIcon,
    Play,
    DownloadIcon,
  } from "lucide-svelte";
  import {
    isTransferSheetOpen,
    openChangeTransferSheet,
  } from "@/lib/stores/akasha.store";
  import { _ } from "svelte-i18n";
  import { toast } from "svelte-sonner";
  import { formatSize } from "@/lib/utils";

  // Subscribe to process store
  $: upload = $AkashaStore.upload;
  $: download = $AkashaStore.download;
  $: gamebanana = $AkashaStore.gamebanana;

  $: uploads = upload.queue.length + (upload.current ? 1 : 0);
  $: downloads = download.queue.length + (download.current ? 1 : 0);
  $: gamebananas = gamebanana.current.length;
  $: total = uploads + downloads;

  // Format the progress message based on process status
  const getProgressMessage = (process): string => {
    switch (process.status) {
      case "creating-directory":
        return `디렉토리 생성중 (${process.processedItems}/${process.totalItems})`;
      case "hash-calculation":
        return `해시 계산중 (${process.processedItems}/${process.totalItems})`;
      case "uploading":
        return `업로드중 (${process.processedItems}/${process.totalItems})`;
      case "paused":
        return "일시정지됨";
      case "completed":
        return "완료됨";
      case "failed":
        return `실패: ${process.error || "알 수 없는 오류"}`;
      default:
        return "대기중";
    }
  };
</script>

<Sheet.Root open={$isTransferSheetOpen} onOpenChange={openChangeTransferSheet}>
  <Sheet.Trigger>
    <div
      class="p-2 flex flex-row rounded-lg transition-colors items-center justify-center cursor-pointer font-semibold hover:text-primary text-muted-foreground"
    >
      <ArrowUpDownIcon />
      {#if total > 0}
        <span class="ml-1">{total}</span>
      {/if}
    </div>
  </Sheet.Trigger>

  <Sheet.Content class="h-full">
    <Sheet.Header>
      <Sheet.Title>{$_("drive.ui.transfers")} ({total})</Sheet.Title>
      <Sheet.Description class="flex flex-grow">
        <div class="flex flex-col w-full gap-4 max-h-[70vh] overflow-y-auto">
          {#if upload.current}
            <div
              class="flex flex-col w-full gap-2 pb-4 border-b last:border-b-0"
            >
              <div
                class="flex flex-row items-center w-full gap-4 justify-between"
              >
                <div class="flex flex-row items-center gap-4">
                  <div
                    class="bg-secondary rounded-md flex items-center justify-center aspect-square w-10 p-1"
                  >
                    {#if upload.current.directories && upload.current.directories.length > 0}
                      <FolderIcon class="w-6 h-6 shrink-0 object-cover" />
                    {:else}
                      <FileIcon class="w-6 h-6 shrink-0 object-cover" />
                    {/if}
                  </div>
                  <div class="flex flex-col">
                    <p class="line-clamp-1 text-ellipsis break-all">
                      {upload.current.name}
                    </p>
                    <p
                      class="line-clamp-1 text-ellipsis break-all text-xs text-muted-foreground"
                    >
                      {formatSize(upload.current.size)}
                    </p>
                  </div>
                </div>

                <div class="flex flex-row gap-1 shrink-0">
                  <div
                    class="inline-flex rounded-full border px-1.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 border-transparent bg-secondary text-secondary-foreground hover:bg-secondary/80 items-center gap-2"
                  >
                    <LoaderIcon class="animate-spin" size={12} />
                    {getProgressMessage(upload.current)}
                  </div>

                  <!-- {#if currentUpload.status === "uploading"}
                    <button
                      class="inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 hover:bg-accent hover:text-accent-foreground h-10 w-10"
                      onclick={() => handlePause(currentUpload.pid)}
                    >
                      <PauseIcon />
                    </button>
                  {:else if currentUpload.status === "paused"}
                    <button
                      class="inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 hover:bg-accent hover:text-accent-foreground h-10 w-10"
                      onclick={() => handlePause(currentUpload.pid)}
                    >
                      <PlayIcon />
                    </button>
                  {/if}

                  <button
                    class="inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 hover:bg-accent hover:text-accent-foreground h-10 w-10"
                    onclick={() => handleCancel(currentUpload.pid)}
                  >
                    <CircleXIcon />
                  </button> -->
                </div>
              </div>

              {#if upload.current.status === "uploading" && upload.current.totalBytes !== undefined}
                <div class="flex flex-col gap-2">
                  <!-- <Progress
                    value={(upload.current.uploadedBytes /
                      upload.current.totalBytes) *
                      100}
                    class="h-1"
                  /> -->
                  <div
                    class="flex justify-between text-xs text-muted-foreground"
                  >
                    <span>
                      <!-- {formatFileSize(process.bytesUploaded)} / {formatFileSize(
                    process.totalBytes
                  )} -->
                    </span>
                    <span>{formatSize(upload.current.uploadBytesPerSec)}/s</span
                    >
                  </div>
                </div>
              {/if}

              {#if upload.current.error}
                <p class="text-sm text-destructive">{upload.current.error}</p>
              {/if}
            </div>
          {/if}

          {#if download.current}
            <div
              class="flex flex-col w-full gap-2 pb-4 border-b last:border-b-0"
            >
              <div
                class="flex flex-row items-center w-full gap-4 justify-between"
              >
                <div class="flex flex-row items-center gap-4">
                  <div
                    class="bg-secondary rounded-md flex items-center justify-center aspect-square w-10"
                  >
                    <button
                      class="inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 hover:bg-accent hover:text-accent-foreground h-10 w-10"
                      onclick={() => {
                        if (download.current) {
                          // AkashaStore.CancelDownload(download.current.pid);
                        } else {
                          toast.warning(
                            "현재 중지하려는 다운로드가 current 상태에 있지 않습니다",
                          );
                        }
                      }}
                    >
                      <CircleXIcon />
                    </button>
                  </div>
                  <div class="flex flex-col">
                    <p class="line-clamp-1 text-ellipsis break-all">
                      {download.current.name}
                    </p>
                    <p
                      class="line-clamp-1 text-ellipsis break-all text-xs text-muted-foreground"
                    >
                      {formatSize(download.current.totalSize)}
                    </p>
                  </div>
                </div>

                <div class="flex flex-row gap-1 shrink-0">
                  <div
                    class="inline-flex rounded-full border px-1.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 border-transparent bg-secondary text-secondary-foreground hover:bg-secondary/80 items-center gap-2"
                  >
                    <LoaderIcon class="animate-spin" size={12} />
                    {formatSize(download.current.downloadedSize)}/{formatSize(
                      download.current.totalSize,
                    )}
                  </div>

                  <!-- {#if currentUpload.status === "uploading"}
                    <button
                      class="inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 hover:bg-accent hover:text-accent-foreground h-10 w-10"
                      onclick={() => handlePause(currentUpload.pid)}
                    >
                      <PauseIcon />
                    </button>

                    <button
                      class="inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 hover:bg-accent hover:text-accent-foreground h-10 w-10"
                      onclick={() => handlePause(currentUpload.pid)}
                    >
                      <PlayIcon />
                    </button>
                     -->
                </div>
              </div>

              <div class="flex flex-col gap-2">
                <Progress value={download.current.progress} class="h-1" />
                <div class="flex justify-between text-xs text-muted-foreground">
                  <span>
                    <!-- {formatFileSize(process.bytesUploaded)} / {formatFileSize(
                    process.totalBytes
                  )} -->
                  </span>
                  {#if download.current.downloadBytesPerSec}
                    <span
                      >{formatSize(
                        download.current.downloadBytesPerSec,
                      )}/s</span
                    >
                  {/if}
                </div>
              </div>
            </div>
          {/if}

          {#each upload.queue as queue (queue.pid)}
            <div
              class="flex flex-col w-full gap-2 pb-4 border-b last:border-b-0"
            >
              <div
                class="flex flex-row items-center w-full gap-4 justify-between"
              >
                <div class="flex flex-row items-center gap-4">
                  <div
                    class="bg-secondary rounded-md flex items-center justify-center aspect-square w-10"
                  >
                    {#if queue.directories && queue.directories.length > 0}
                      <FolderIcon />
                    {:else}
                      <FileIcon />
                    {/if}
                  </div>
                  <div class="flex flex-col">
                    <p class="line-clamp-1 text-ellipsis break-all">
                      {queue.name}
                    </p>
                    <p
                      class="line-clamp-1 text-ellipsis break-all text-xs text-muted-foreground"
                    >
                      {formatSize(queue.size)}
                    </p>
                  </div>
                </div>

                <div class="flex flex-row gap-1 shrink-0">
                  <!-- {#if currentUpload.status === "uploading"}
                    <button
                      class="inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 hover:bg-accent hover:text-accent-foreground h-10 w-10"
                      onclick={() => handlePause(currentUpload.pid)}
                    >
                      <PauseIcon />
                    </button>
                  {:else if currentUpload.status === "paused"}
                    <button
                      class="inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 hover:bg-accent hover:text-accent-foreground h-10 w-10"
                      onclick={() => handlePause(currentUpload.pid)}
                    >
                      <PlayIcon />
                    </button>
                  {/if} -->

                  <button
                    class="inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 hover:bg-accent hover:text-accent-foreground h-10 w-10"
                    onclick={() => {
                      // AkashaStore.removeUploadByPid(queue.pid);
                    }}
                    tabindex={-1}
                  >
                    <CircleXIcon />
                  </button>
                </div>
              </div>
            </div>
          {/each}

          {#each download.queue as queue (queue.pid)}
            <div
              class="flex flex-col w-full gap-2 pb-4 border-b last:border-b-0"
            >
              <div
                class="flex flex-row items-center w-full gap-4 justify-between"
              >
                <div class="flex flex-row items-center gap-4">
                  <div
                    class="bg-secondary rounded-md flex items-center justify-center aspect-square w-10"
                  >
                    <DownloadIcon />
                  </div>
                  <div class="flex flex-col">
                    <p class="line-clamp-1 text-ellipsis break-all">
                      {queue.name}
                    </p>
                  </div>
                </div>
              </div>
            </div>
          {/each}

          {#if gamebananas > 0}
            <div>
              {#if gamebanana.current}{/if}
            </div>
          {/if}

          {#if total === 0}
            <div
              class="w-full flex flex-col items-center justify-center text-muted-foreground gap-2"
            >
              <ArrowUpDownIcon />
              <p class="text-base">
                {$_("drive.ui.process_sheet.no_transfer_yet")}
              </p>
            </div>
          {/if}
        </div>
      </Sheet.Description>
    </Sheet.Header>
  </Sheet.Content>
</Sheet.Root>
