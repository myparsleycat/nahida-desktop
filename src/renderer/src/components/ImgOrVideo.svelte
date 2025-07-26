<script lang="ts">
  import { nanoid } from "nanoid";
  import { sineOut } from "svelte/easing";
  import { scale } from "svelte/transition";

  let {
    src = "",
    alt = "",
    class: className,
    type,
    path = "",
    timestamp = "",
    draggable,
    transition,
  }: {
    src?: string;
    alt: string;
    class?: string;
    type?: "img" | "video";
    path?: string;
    timestamp?: string | number;
    draggable?: "true" | "false";
    transition?: "modal";
  } = $props();

  const actualType = $derived.by(() => {
    if (type) {
      return type;
    }

    if (src) {
      return src.includes("video-local") ? "video" : "img";
    }

    return "img";
  });

  const computedSrc = $derived.by(() => {
    if (src) {
      return src;
    }

    if (path && timestamp) {
      const encodedPath = encodeURIComponent(path);
      const protocol =
        actualType === "video"
          ? "nahida://video-local"
          : "nahida://image-local";
      return `${protocol}?path=${encodedPath}&t=${timestamp}`;
    }

    return "";
  });

  const transitionProps =
    transition === "modal"
      ? {
          duration: 200,
          easing: sineOut,
          start: 0.93,
        }
      : undefined;
</script>

{#if actualType === "img"}
  {#if transition === "modal"}
    <img
      src={computedSrc}
      {alt}
      class={className}
      loading="lazy"
      decoding="async"
      {draggable}
      transition:scale={transitionProps}
    />
  {:else}
    <img
      src={computedSrc}
      {alt}
      class={className}
      loading="lazy"
      decoding="async"
      {draggable}
    />
  {/if}
{:else if actualType === "video"}
  {#if transition === "modal"}
    <!-- svelte-ignore element_invalid_self_closing_tag -->
    {#key `${src}-${nanoid()}`}
      <video
        src={computedSrc}
        autoplay
        defaultmuted
        muted
        loop
        class={className}
        preload="metadata"
        crossorigin="anonymous"
        {draggable}
        transition:scale={transitionProps}
        onerror={(e) => {
          // @ts-ignore
          e.target.load();
        }}
      />
    {/key}
  {:else}
    <!-- svelte-ignore element_invalid_self_closing_tag -->
    {#key `${src}-${nanoid()}`}
      <video
        src={computedSrc}
        autoplay
        defaultmuted
        muted
        loop
        class={className}
        preload="metadata"
        crossorigin="anonymous"
        {draggable}
        onerror={(e) => {
          // @ts-ignore
          e.target.load();
        }}
      />
    {/key}
  {/if}
{:else}
  <span>Invalid Type</span>
{/if}
