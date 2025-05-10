<script lang="ts">
  import { _ } from "svelte-i18n";
  import { cn } from "$lib/utils";
  import {
    CloudIcon,
    HardDriveIcon,
    SettingsIcon,
    Share2Icon,
    Trash2Icon,
  } from "lucide-svelte";
  import * as Tooltip from "$lib/components/ui/tooltip";
  import * as ContextMenu from "$lib/components/ui/context-menu";
  import { QueryClientProvider } from "@tanstack/svelte-query";
  import { queryClient } from "../queryClient";
  import { NDH, Main } from "@/lib/helpers";
  import SettingComponent from "./setting/Setting.svelte";
  import autoAnimate from "@formkit/auto-animate";
  import CloudWrapper from "./cloud/CloudWrapper.svelte";
  import ModsWrapper from "./mods/ModsWrapper.svelte";
  import Separator from "@/lib/components/ui/separator/separator.svelte";

  let page = Main.page;
</script>

<div class="flex h-full w-full data-[panel-group-direction=vertical]:flex-col">
  <div class="flex flex-col">
    <div class="w-full flex flex-col h-full select-none">
      <div
        class="flex flex-col overflow-y-auto overflow-x-hidden dragselect-start-allowed px-2"
      >
        <div class="space-y-2">
          <div class="flex">
            <Tooltip.Root openDelay={50} closeDelay={50}>
              <Tooltip.Trigger>
                <!-- <ProcessSheet /> -->
              </Tooltip.Trigger>
              <Tooltip.Content side="right">
                <p>{$_("drive.ui.transfers")}</p>
              </Tooltip.Content>
            </Tooltip.Root>
          </div>

          <Separator />

          <div class="flex">
            <Tooltip.Root openDelay={50} closeDelay={50}>
              <Tooltip.Trigger>
                <button
                  class={cn(
                    "flex flex-row gap-2.5 w-full p-2 rounded-md transition-all items-center hover:bg-secondary text-primary cursor-pointer active",
                    $page === "mods" && "bg-secondary",
                  )}
                  onclick={() => {
                    if ($page !== "mods") {
                      page.set("mods");
                    }
                    NDH.nav.move("mods");
                  }}
                >
                  <div class="flex flex-row gap-2 items-center">
                    <div>
                      <HardDriveIcon />
                    </div>
                  </div>
                </button>
              </Tooltip.Trigger>
              <Tooltip.Content side="right">
                <p>{$_("drive.ui.my_mods")}</p>
              </Tooltip.Content>
            </Tooltip.Root>
          </div>

          <div class="flex">
            <Tooltip.Root openDelay={50} closeDelay={50}>
              <Tooltip.Trigger>
                <button
                  class={cn(
                    "flex flex-row gap-2.5 w-full p-2 rounded-md transition-all items-center hover:bg-secondary text-primary cursor-pointer active",
                    $page === "cloud" && "bg-secondary",
                  )}
                  onclick={() => {
                    if ($page !== "cloud") {
                      page.set("cloud");
                    }
                    NDH.nav.move("root");
                  }}
                >
                  <div class="flex flex-row gap-2 items-center">
                    <div>
                      <CloudIcon />
                    </div>
                  </div>
                </button>
              </Tooltip.Trigger>
              <Tooltip.Content side="right">
                <p>{$_("drive.ui.cloud_drive")}</p>
              </Tooltip.Content>
            </Tooltip.Root>
          </div>

          <div class="flex">
            <Tooltip.Root openDelay={50} closeDelay={50}>
              <Tooltip.Trigger>
                <button
                  class={cn(
                    "flex flex-row gap-2.5 w-full p-2 rounded-md transition-all items-center hover:bg-secondary text-primary cursor-pointer bg-transparent",
                    $page === "setting" && "bg-secondary",
                  )}
                  onclick={() => Main.page.set("setting")}
                >
                  <SettingsIcon />
                </button>
              </Tooltip.Trigger>
              <Tooltip.Content side="right">
                <p>{$_("drive.ui.settings")}</p>
              </Tooltip.Content>
            </Tooltip.Root>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div
    class="flex-1 relative overflow-hidden border-l border-t rounded-tl-2xl dark:bg-[#111115] shadow-inner"
  >
    <!-- relative 추가 -->
    <div
      class="flex grow relative h-full"
      use:autoAnimate={{ duration: 100, easing: "ease-in-out" }}
    >
      <!-- h-full 추가 -->
      <QueryClientProvider client={queryClient}>
        {#if $page === "mods"}
          <ModsWrapper></ModsWrapper>
        {:else if $page === "cloud"}
          <CloudWrapper></CloudWrapper>
        {:else if $page === "setting"}
          <SettingComponent></SettingComponent>
        {/if}
      </QueryClientProvider>
    </div>
  </div>
</div>

<!-- <AlertDialog.Root
  open={$DialogStateStore.emptyTrashDialog.open}
  onOpenChange={(v) => DialogStateStore.setOpen("emptyTrashDialog", v)}
>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{$_("drive.ui.empty_trash")}</AlertDialog.Title>
      <AlertDialog.Description>
        {$_("drive.ui.empty_trash_dialog.0")}
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel>{$_("g.cancel")}</AlertDialog.Cancel>
      <AlertDialog.Action onclick={EmptyTrash}>
        {$_("drive.ui.empty_trash_dialog.1")}
      </AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root> -->
