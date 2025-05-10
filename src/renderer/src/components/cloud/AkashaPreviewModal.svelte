<script lang="ts">
  import { fade, scale } from "svelte/transition";
  import { sineOut } from "svelte/easing";
  import { LoaderIcon } from "lucide-svelte";
  import type { LayoutType } from "$lib/types";
  import { cn } from "@/lib/utils";
  import { NDH } from "@/lib/helpers";

  let {
    className,
    img,
    alt,
    type,
    onOpenChange,
  }: {
    className?: string;
    img: { default: string; cover: string | null; thumbnail: string | null };
    alt: string;
    type: LayoutType;
    onOpenChange: (state: boolean) => void;
  } = $props<{
    className?: string;
    img: { default: string; cover: string | null; thumbnail: string | null };
    alt: string;
    type: LayoutType;
    onOpenChange: (state: boolean) => void;
  }>();
  let showModal = $state(false);
  let hihi = $state("");

  function updateModalState(state: boolean) {
    showModal = state;
    if (onOpenChange) {
      onOpenChange(showModal);
    }
  }

  function imgErrorHandle(e: Event) {
    if (e.currentTarget) {
      // @ts-ignore
      e.currentTarget.src = "https://nahida.live/d9dcf263a2a539d395740f74ae747390_5623118858442815874_waifu2x_art_noise0_scale.png";
    }
  }

  $effect(() => {
    if (showModal) {
      NDH.util.imageCache.get(img.default).then((res) => {
        hihi = res;
      });
    }
  });
</script>

<svelte:window
  onkeydown={(e) => e.key === "Escape" && updateModalState(false)}
/>

<div class={cn("relative select-none", className)}>
  <button
    class="focus:outline-none"
    onclick={(e) => {
      e.stopPropagation();
      updateModalState(true);
    }}
    ondblclick={(e) => e.stopPropagation()}
  >
    <img
      src={type === "list" ? img.thumbnail : img.cover}
      {alt}
      class="object-cover rounded-md aspect-square"
      draggable="false"
      loading="lazy"
      onerror={imgErrorHandle}
    />
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
      {#if hihi}
        <img
          src={hihi}
          {alt}
          class="md:max-h-[70vh] max-w-[90vw] md:max-w-[70vw] object-contain"
          draggable="false"
          transition:scale={{
            duration: 200,
            easing: sineOut,
            start: 0.93,
          }}
        />
      {:else}
        <div>
          <LoaderIcon class="animate-spin-1.5" size={80} />
        </div>
      {/if}
    </button>
  {/if}
</div>
