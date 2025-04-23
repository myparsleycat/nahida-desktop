<script lang="ts">
  import { _ } from "svelte-i18n";
  import * as Select from "$lib/components/ui/select";
  import { setMode, mode, userPrefersMode } from "mode-watcher";

  type themeT = "light" | "dark" | "system";
  let theme = $state(userPrefersMode.current);
  let label =
    theme === "dark"
      ? $_("g.dark")
      : theme === "light"
        ? $_("g.light")
        : $_("g.system");
</script>

<div class="flex flex-col gap-3 border rounded-xl p-4 shadow hover:shadow-lg duration-200 dark:bg-zinc-950">
  <div class="flex flex-row justify-between items-center gap-14 min-h-10">
    <div class="flex flex-col">
      <p class="line-clamp-1 text-ellipsis break-all">
        {$_("drive.ui.settings_page.general.theme")}
      </p>
      <p class="text-sm text-muted-foreground">
        {$_("drive.ui.settings_page.general.desc")}
      </p>
    </div>
    <div class="flex flex-row items-center gap-4">
      <Select.Root
        required
        selected={{ label, value: theme }}
        onSelectedChange={(v) => setMode(v?.value as themeT)}
      >
        <Select.Trigger class="w-[180px]">
          <Select.Value placeholder="Theme" />
        </Select.Trigger>
        <Select.Content>
          <Select.Item value="light">{$_("g.light")}</Select.Item>
          <Select.Item value="dark">{$_("g.dark")}</Select.Item>
          <Select.Item value="system">{$_("g.system")}</Select.Item>
        </Select.Content>
      </Select.Root>
    </div>
  </div>
</div>
