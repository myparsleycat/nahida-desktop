<script lang="ts">
  import { Switch } from "@/lib/components/ui/switch";
  import { toast } from "svelte-sonner";

  let checked = $state(true);
</script>

<div
  class="flex flex-col gap-3 border rounded-xl p-4 shadow hover:shadow-lg duration-200 dark:bg-zinc-950"
>
  <div class="space-y-6">
    <div class="flex flex-row justify-between items-center gap-14 min-h-10">
      <div class="flex flex-col">
        <p class="line-clamp-1 text-ellipsis break-all">자동 픽스</p>
        <p class="text-sm text-muted-foreground">
          등록된 폴더 안에 모드가 추가되면 자동으로 픽스를 진행해요
        </p>
      </div>
      <div class="flex flex-row items-center gap-4">
        <Switch disabled />
      </div>
    </div>

    <!-- <Separator /> -->
  </div>
</div>
