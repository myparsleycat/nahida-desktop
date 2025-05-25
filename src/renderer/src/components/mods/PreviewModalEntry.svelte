<script lang="ts">
  import { fade, scale } from "svelte/transition";
  import { sineOut } from "svelte/easing";
  import { ExpandIcon } from "lucide-svelte";
  import { cn } from "@/lib/utils";
  import { writable } from "svelte/store";
  import { PreviewModalClass } from "@/lib/stores/global.store";

  const modalStore = writable<{
    isOpen: boolean;
    src: string;
    alt: string;
  }>({
    isOpen: false,
    src: "",
    alt: "",
  });

  let {
    className,
    src,
    alt,
  }: {
    className?: string;
    src: string;
    alt: string;
  } = $props();

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && $modalStore.isOpen) {
      PreviewModalClass.close();
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class={cn("relative select-none", className)}>
  <button
    class="p-1 hover:bg-black/50 rounded-lg duration-150"
    onclick={(e) => {
      e.stopPropagation();
      PreviewModalClass.open(src, alt);
    }}
  >
    <ExpandIcon color="white" size={22} />
  </button>
</div>
