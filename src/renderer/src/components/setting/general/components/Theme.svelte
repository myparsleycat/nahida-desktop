<script lang="ts">
  import { _ } from "svelte-i18n";
  import * as Select from "$lib/components/ui/select/index";
  import { setMode, userPrefersMode } from "mode-watcher";
</script>

<div
  class="flex flex-col gap-3 border rounded-xl p-4 shadow hover:dark:shadow-none hover:shadow-lg duration-200 dark:bg-zinc-950"
>
  <div class="flex flex-row justify-between items-center gap-14 min-h-10">
    <div class="flex flex-col">
      <p class="line-clamp-1 text-ellipsis break-all">
        {$_("setting.general.theme.a")}
      </p>
      <p class="text-sm text-muted-foreground">
        {$_("setting.general.theme.b")}
      </p>
    </div>
    <div class="flex flex-row items-center gap-4">
      <Select.Root
        required
        type="single"
        {...{
          // @ts-ignore
        }}
        onValueChange={(v) => setMode(v)}
        value={userPrefersMode.current}
      >
        <Select.Trigger class="w-[180px]">
          {$_(`setting.general.theme.themes.${userPrefersMode.current}`)}
        </Select.Trigger>
        <Select.Content>
          <Select.Item value="light"
            >{$_("setting.general.theme.themes.light")}</Select.Item
          >
          <Select.Item value="dark"
            >{$_("setting.general.theme.themes.dark")}</Select.Item
          >
          <Select.Item value="system"
            >{$_("setting.general.theme.themes.system")}</Select.Item
          >
        </Select.Content>
      </Select.Root>
    </div>
  </div>
</div>
