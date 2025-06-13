<script lang="ts">
  import { Switch } from "$lib/components/ui/switch/index";
  import { onMount } from "svelte";
  import { _ } from "svelte-i18n";

  let autofix_after_nahida = $state(true);
  let autofix_after_nahida_disabled = $state(false);

  onMount(async () => {
    const init = await window.api.setting.autofix.nahida.get();
    autofix_after_nahida = init;
  });

  async function handleToggle() {
    await window.api.setting.autofix.nahida.set(autofix_after_nahida);
  }
</script>

<div
  class="flex flex-col gap-3 border rounded-xl p-4 shadow hover:dark:shadow-none hover:shadow-lg duration-200 dark:bg-zinc-950"
>
  <div class="space-y-6">
    <div class="flex flex-row justify-between items-center gap-14 min-h-10">
      <div class="flex flex-col">
        <p class="line-clamp-1 text-ellipsis break-all">
          {$_("setting.mods.autofix.a")}
        </p>
        <p class="text-sm text-muted-foreground">
          {$_("setting.mods.autofix.b")}
        </p>
      </div>
      <div class="flex flex-row items-center gap-4">
        <Switch
          bind:checked={autofix_after_nahida}
          disabled={autofix_after_nahida_disabled}
          onCheckedChange={handleToggle}
        />
      </div>
    </div>
  </div>
</div>
