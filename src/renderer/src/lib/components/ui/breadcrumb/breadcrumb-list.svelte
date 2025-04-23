<script lang="ts">
  import type { HTMLOlAttributes } from "svelte/elements";
  import { cn } from "$lib/utils.js";

  type $$Props = HTMLOlAttributes & {
    el?: HTMLOListElement;
    use?: (node: HTMLElement) => { destroy?: () => void };
  };

  export let el: $$Props["el"] = undefined;
  export let use: $$Props["use"] = undefined; // export use prop
  let className: $$Props["class"] = undefined;
  export { className as class };
</script>

{#if use}
  <ol
    bind:this={el}
    class={cn(
      "text-muted-foreground flex flex-wrap items-center gap-1.5 break-words text-sm sm:gap-2.5",
      className
    )}
    {...$$restProps}
    use:use
  >
    <slot />
  </ol>
{:else}
  <ol
    bind:this={el}
    class={cn(
      "text-muted-foreground flex flex-wrap items-center gap-1.5 break-words text-sm sm:gap-2.5",
      className
    )}
    {...$$restProps}
  >
    <slot />
  </ol>
{/if}
