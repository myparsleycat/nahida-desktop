<script lang="ts">
  import { Button, buttonVariants } from "$lib/components/ui/button";
  import * as AlertDialog from "$lib/components/ui/alert-dialog/index";
  import { NDH } from "$lib/helpers";
  import { formatSize } from "$lib/utils";
  import { toast } from "svelte-sonner";
  import { _ } from "svelte-i18n";
  import { Loader2Icon } from "@lucide/svelte";
  import { Switch } from "$lib/components/ui/switch";
  import { Label } from "$lib/components/ui/label";
  import Separator from "$lib/components/ui/separator/separator.svelte";

  let getStates = $state(true);
  let sizes = $state(0);
  let _state = $state(true);
  let open = $state(false);

  const getState = async () => {
    NDH.util.imageCache
      .getStates()
      .then((res) => {
        _state = res.state;
        sizes = res.sizes;
      })
      .finally(() => {
        getStates = false;
      });
  };

  const handleDeleteImageButtonClick = async () => {
    const clearPromise = NDH.util.imageCache.clear();
    toast.promise(clearPromise, {
      loading: `${$_("setting.cloud.preview_cache.c.toast.loading")}`,
      success: () => {
        getState();
        open = false;
        return `${$_("setting.cloud.preview_cache.c.toast.success")}`;
      },
      error: (err: any) =>
        `${$_("setting.cloud.preview_cache.c.toast.error", { values: { error: err.message } })}`,
    });
  };

  $effect(() => {
    getState();
  });
</script>

<div class="space-y-6">
  <div class="flex flex-row justify-between items-center gap-14 min-h-10">
    <div class="flex flex-col">
      <p class="line-clamp-1 text-ellipsis break-all">
        {$_("setting.cloud.preview_cache.s.a")}
      </p>
      <p class="text-sm text-muted-foreground">
        {$_("setting.cloud.preview_cache.s.b")}
      </p>
    </div>
    <div class="flex flex-row items-center gap-4">
      <Switch
        checked={_state}
        onCheckedChange={async (v) => {
          await NDH.util.imageCache.change(v).then(() => {
            getState();
          });
        }}
      />
    </div>
  </div>

  <Separator />

  <div class="flex flex-row justify-between items-center gap-14 min-h-10">
    <div class="flex flex-col">
      <p class="line-clamp-1 text-ellipsis break-all">
        {$_("setting.cloud.preview_cache.c.a")}
      </p>
      <p class="text-sm text-muted-foreground">
        {$_("setting.cloud.preview_cache.c.b")}
      </p>
    </div>
    <div class="flex flex-row items-center gap-4">
      {#if getStates}
        <Loader2Icon class="animate-spin-1.5" />
      {:else}
        <p class="text-muted-foreground">{formatSize(sizes)}</p>
      {/if}
      <AlertDialog.Root bind:open>
        <AlertDialog.Trigger class={buttonVariants({ variant: "destructive" })}>
          {$_("setting.cloud.preview_cache.c.dialog.action")}
        </AlertDialog.Trigger>
        <AlertDialog.Content>
          <AlertDialog.Header>
            <AlertDialog.Title>
              {$_("setting.cloud.preview_cache.c.dialog.a")}
            </AlertDialog.Title>
            <AlertDialog.Description>
              {$_("setting.cloud.preview_cache.c.dialog.b")}
            </AlertDialog.Description>
          </AlertDialog.Header>
          <AlertDialog.Footer>
            <AlertDialog.Cancel>{$_("global.cancel")}</AlertDialog.Cancel>
            <AlertDialog.Action
              class={buttonVariants({ variant: "destructive" })}
              onclick={handleDeleteImageButtonClick}
            >
              {$_("setting.cloud.preview_cache.c.dialog.action")}
            </AlertDialog.Action>
          </AlertDialog.Footer>
        </AlertDialog.Content>
      </AlertDialog.Root>
    </div>
  </div>
</div>
