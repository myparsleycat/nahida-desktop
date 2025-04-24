<script lang="ts">
  import { fade, scale } from "svelte/transition";
  import { sineOut } from "svelte/easing";
  import { ExpandIcon, LoaderIcon } from "lucide-svelte";
  import { cn } from "@/lib/utils";
  import { Cloud } from "@/lib/helpers";

  let {
    className,
    src,
    alt,
    onOpenChange,
  }: {
    className?: string;
    src: string;
    alt: string;
    onOpenChange: (state: boolean) => void;
  } = $props<{
    className?: string;
    src: string;
    alt: string;
    onOpenChange: (state: boolean) => void;
  }>();
  let showModal = $state(false);

  function updateModalState(state: boolean) {
    showModal = state;
    if (onOpenChange) {
      onOpenChange(showModal);
    }
  }
</script>

<svelte:window
  onkeydown={(e) => e.key === "Escape" && updateModalState(false)}
/>

<div class={cn("relative select-none", className)}>
  <button
    class="p-1 hover:bg-black/50 rounded-lg duration-150"
    onclick={(e) => {
      e.stopPropagation();
      updateModalState(true);
    }}
  >
    <ExpandIcon color="white" size={22} />
  </button>

  {#if showModal}
    <button
      onclick={(e) => {
        e.stopPropagation();
        updateModalState(false);
      }}
      ondblclick={(e) => e.stopPropagation()}
      class="fixed inset-0 bg-black/75 flex items-center justify-center z-50"
      transition:fade={{ duration: 100 }}
    >
      <img
        {src}
        {alt}
        class="max-h-[90vh] max-w-[90vw] object-contain"
        draggable="false"
        transition:scale={{
          duration: 200,
          easing: sineOut,
          start: 0.93,
        }}
      />
    </button>
  {/if}
</div>
