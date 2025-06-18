<script lang="ts">
  import { _ } from "svelte-i18n";
  import { cn } from "$lib/utils";
  import {
    CloudIcon,
    HardDriveIcon,
    LeafyGreenIcon,
    SettingsIcon,
  } from "@lucide/svelte";
  import * as Tooltip from "$lib/components/ui/tooltip";
  import { NDH, Main, ModsHelper } from "$lib/helpers";
  import SettingComponent from "./setting/Setting.svelte";
  import autoAnimate from "@formkit/auto-animate";
  import CloudWrapper from "./cloud/CloudWrapper.svelte";
  import ModsWrapper from "./mods/ModsWrapper.svelte";
  import { Separator } from "$lib/components/ui/separator";
  // import ProcessSheet from "./ProcessSheet.svelte";
  import NahidaWrapper from "./nahida/NahidaWrapper.svelte";
  import { PreviewModalClass } from "$lib/stores/global.store";
  import { fade, scale } from "svelte/transition";
  import { sineOut } from "svelte/easing";
  import CharPathSelector from "./CharPathSelector.svelte";

  let page = Main.page;
  let previewModalStore = PreviewModalClass.store;
</script>

<div
  class="flex h-full w-full data-[panel-group-direction=vertical]:flex-col select-none"
>
  <div class="flex flex-col">
    <div class="w-full flex flex-col h-full select-none">
      <div
        class="flex flex-col overflow-y-auto overflow-x-hidden dragselect-start-allowed px-2"
      >
        <div class="space-y-2">
          <!-- <div class="flex">
            <Tooltip.Provider>
              <Tooltip.Root delayDuration={50}>
                <Tooltip.Trigger>
                  <ProcessSheet />
                </Tooltip.Trigger>
                <Tooltip.Content side="right">
                  <p>{$_("drive.ui.transfers")}</p>
                </Tooltip.Content>
              </Tooltip.Root>
            </Tooltip.Provider>
          </div> -->

          <Separator />

          <div class="flex">
            <Tooltip.Provider>
              <Tooltip.Root delayDuration={50}>
                <Tooltip.Trigger
                  class={cn(
                    "flex flex-row gap-2.5 w-full p-2 rounded-md transition-all items-center hover:bg-secondary text-primary cursor-pointer active",
                    $page === "mods" && "bg-secondary",
                  )}
                  onclick={() => {
                    if ($page !== "mods") {
                      page.set("mods");
                    }
                    NDH.nav.move("mods");
                    ModsHelper.clearPath();
                  }}
                >
                  <HardDriveIcon />
                </Tooltip.Trigger>
                <Tooltip.Content side="right">
                  <p>{$_("mods.a")}</p>
                </Tooltip.Content>
              </Tooltip.Root>
            </Tooltip.Provider>
          </div>

          <!-- <div class="flex">
            <Tooltip.Provider>
              <Tooltip.Root delayDuration={50}>
                <Tooltip.Trigger>
                  <button
                    class={cn(
                      "flex flex-row gap-2.5 w-full p-2 rounded-md transition-all items-center hover:bg-secondary text-primary cursor-pointer active",
                      $page === "nahida" && "bg-secondary",
                    )}
                    onclick={() => {
                      if ($page !== "nahida") {
                        page.set("nahida");
                      }
                    }}
                  >
                    <div class="flex flex-row gap-2 items-center">
                      <div>
                        <LeafyGreenIcon />
                      </div>
                    </div>
                  </button>
                </Tooltip.Trigger>
                <Tooltip.Content side="right">
                  <p>{$_("live.a")}</p>
                </Tooltip.Content>
              </Tooltip.Root>
            </Tooltip.Provider>
          </div> -->

          <!-- <div class="flex">
            <Tooltip.Provider>
              <Tooltip.Root delayDuration={50}>
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
                  <p>{$_("drive.a")}</p>
                </Tooltip.Content>
              </Tooltip.Root>
            </Tooltip.Provider>
          </div> -->

          <div class="flex">
            <Tooltip.Provider>
              <Tooltip.Root delayDuration={50}>
                <Tooltip.Trigger
                  class={cn(
                    "flex flex-row gap-2.5 w-full p-2 rounded-md transition-all items-center hover:bg-secondary text-primary cursor-pointer bg-transparent",
                    $page === "setting" && "bg-secondary",
                  )}
                  onclick={() => Main.page.set("setting")}
                >
                  <SettingsIcon />
                </Tooltip.Trigger>
                <Tooltip.Content side="right">
                  <p>{$_("setting.a")}</p>
                </Tooltip.Content>
              </Tooltip.Root>
            </Tooltip.Provider>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div
    class="flex-1 relative overflow-hidden border-l border-t rounded-tl-2xl dark:bg-[#111115] shadow-inner"
  >
    <div
      class="flex grow relative h-full"
      use:autoAnimate={{ duration: 100, easing: "ease-in-out" }}
    >
      {#if $page === "mods"}
        <ModsWrapper></ModsWrapper>
      {:else if $page === "nahida"}
        <NahidaWrapper></NahidaWrapper>
      {:else if $page === "cloud"}
        <CloudWrapper></CloudWrapper>
      {:else if $page === "setting"}
        <SettingComponent></SettingComponent>
      {/if}
    </div>
  </div>
</div>

<CharPathSelector />

{#if $previewModalStore.isOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 bg-black/75 flex items-center justify-center z-[5000]"
    onclick={(e) => {
      e.stopPropagation();
      PreviewModalClass.close();
    }}
    transition:fade={{ duration: 100 }}
  >
    <img
      src={$previewModalStore.src}
      alt={$previewModalStore.alt}
      class="max-h-[90vh] max-w-[90vw] object-contain"
      draggable="false"
      decoding="async"
      transition:scale={{
        duration: 200,
        easing: sineOut,
        start: 0.93,
      }}
    />
  </div>
{/if}

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
