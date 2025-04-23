<script lang="ts">
  import { Button } from "@/lib/components/ui/button";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import { Cloud } from "@/lib/helpers";
  import { formatSize } from "@/lib/utils";
  import { toast } from "svelte-sonner";
  import { _ } from "svelte-i18n";
  import { Loader2Icon } from "lucide-svelte";
  import { Switch } from "$lib/components/ui/switch";
  import { Label } from "@/lib/components/ui/label";
  import Separator from "@/lib/components/ui/separator/separator.svelte";

  let getStates = $state(true);
  let sizes = $state(0);
  let _state = $state(true);

  const getState = async () => {
    Cloud.util.imageCache
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
    const clearPromise = Cloud.util.imageCache.clear();
    toast.promise(clearPromise, {
      loading: `${$_("drive.ui.settings_page.preview_cache.toast.loading")}`,
      success: () => {
        getState();
        return `${$_("drive.ui.settings_page.preview_cache.toast.success")}`;
      },
      error: (err: any) =>
        `${$_("drive.ui.settings_page.preview_cache.toast.error", { values: { error: err.message } })}`,
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
        {$_("drive.ui.settings_page.preview_cache_state.ui.title")}
      </p>
      <p class="text-sm text-muted-foreground">
        {$_("drive.ui.settings_page.preview_cache_state.ui.desc")}
      </p>
    </div>
    <div class="flex flex-row items-center gap-4">
      <Switch
        checked={_state}
        onCheckedChange={async (v) => {
          await Cloud.util.imageCache.change(v).then(() => {
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
        {$_("drive.ui.settings_page.preview_cache.ui.title")}
      </p>
      <p class="text-sm text-muted-foreground">
        {$_("drive.ui.settings_page.preview_cache.ui.desc")}
      </p>
    </div>
    <div class="flex flex-row items-center gap-4">
      {#if getStates}
        <Loader2Icon class="animate-spin-1.5" />
      {:else}
        <p class="text-muted-foreground">{formatSize(sizes)}</p>
      {/if}
      <AlertDialog.Root>
        <AlertDialog.Trigger asChild let:builder>
          <Button builders={[builder]} variant="destructive">
            {$_("drive.ui.settings_page.preview_cache.ui.action")}
          </Button>
        </AlertDialog.Trigger>
        <AlertDialog.Content>
          <AlertDialog.Header>
            <AlertDialog.Title>
              {$_("drive.ui.settings_page.preview_cache.ui.dialog.title")}
            </AlertDialog.Title>
            <AlertDialog.Description>
              {$_("drive.ui.settings_page.preview_cache.ui.dialog.desc")}
            </AlertDialog.Description>
          </AlertDialog.Header>
          <AlertDialog.Footer>
            <AlertDialog.Cancel>{$_("g.cancel")}</AlertDialog.Cancel>
            <AlertDialog.Action asChild let:builder>
              <Button
                builders={[builder]}
                variant="destructive"
                onclick={handleDeleteImageButtonClick}
              >
                {$_("drive.ui.settings_page.preview_cache.ui.action")}
              </Button>
            </AlertDialog.Action>
          </AlertDialog.Footer>
        </AlertDialog.Content>
      </AlertDialog.Root>
    </div>
  </div>
</div>
