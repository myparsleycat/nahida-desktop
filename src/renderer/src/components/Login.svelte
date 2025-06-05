<script lang="ts">
  import { Button } from "$lib/components/ui/button";
  import { Auth } from "$lib/helpers";
  import { LoaderCircleIcon } from "@lucide/svelte";

  let processing = $state(false);
</script>

<div class="flex justify-center items-center w-full h-full">
  {#if !processing}
    <Button
      onclick={() => {
        processing = true;
        Auth.StartOAuth2Login();
      }}>로그인</Button
    >
  {:else}
    <LoaderCircleIcon size={80} class="animate-spin-1.5" />
  {/if}
</div>
