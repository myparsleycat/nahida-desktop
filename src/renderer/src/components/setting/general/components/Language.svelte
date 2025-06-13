<script lang="ts">
  import { _ } from "svelte-i18n";
  import * as Select from "$lib/components/ui/select/index";
  import { setMode, userPrefersMode } from "mode-watcher";
  import { SettingHelper } from "@/lib/helpers/setting.helper";
  import { onMount } from "svelte";
  import type { languages } from "@shared/types/setting.types";

  let lang = $state("");

  const langueges = [
    { label: "English", value: "en" },
    { label: "한국어", value: "ko" },
    { label: "中文", value: "zh" },
  ];

  const get_lang = async () => {
    lang = await SettingHelper.general.lang.get();
  };

  onMount(async () => {
    await get_lang();
  });

  const triggerContent = $derived(
    langueges.find((l) => l.value === lang)?.label,
  );
</script>

<div
  class="flex flex-col gap-3 border rounded-xl p-4 shadow hover:dark:shadow-none hover:shadow-lg duration-200 dark:bg-zinc-950"
>
  <div class="flex flex-row justify-between items-center gap-14 min-h-10">
    <div class="flex flex-col">
      <p class="line-clamp-1 text-ellipsis break-all">
        {$_("setting.general.languege.a")}
      </p>
      <p class="text-sm text-muted-foreground">
        {$_("setting.general.languege.b")}
      </p>
    </div>
    <div class="flex flex-row items-center gap-4">
      <Select.Root
        required
        type="single"
        {...{
          // @ts-ignore
        }}
        onValueChange={(v: languages) =>
          SettingHelper.general.lang.set(v).then(async () => await get_lang())}
        value={lang}
      >
        <Select.Trigger class="w-[180px]">
          {triggerContent}
        </Select.Trigger>
        <Select.Content>
          {#each langueges as lang}
            <Select.Item value={lang.value}>{lang.label}</Select.Item>
          {/each}
        </Select.Content>
      </Select.Root>
    </div>
  </div>
</div>
