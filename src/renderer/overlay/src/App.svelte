<script lang="ts">
  import { _ } from "svelte-i18n";
  import { toast, Toaster } from "svelte-sonner";
  import { ModeWatcher } from "mode-watcher";
  // import Layout from "./components/Layout.svelte";
  import { createDraggable } from "animejs";
  import Mods from "@/components/mods/Mods.svelte";
  import { QueryClientProvider } from "@tanstack/svelte-query";
  import { queryClient } from "@/queryClient";
  import { onDestroy, onMount } from "svelte";
  import { ListenersManager } from "@/listeners";

  onMount(async () => {
    createDraggable(".square");
    await ListenersManager.init();

    const folderPathListener = window.api.overlay.visibilityChange((state) => {
      console.log(state)
    });
  });

  onDestroy(() => {
    ListenersManager.destroy();
  });
</script>

<ModeWatcher defaultMode="system" />
<Toaster richColors position="bottom-left" />
<QueryClientProvider client={queryClient}>
  <div class="h-screen w-full">
    <div
      class="square draggable bg-black/75 rounded-lg p-2 absolute bottom-4 left-4 w-1/3 h-1/3"
    >
      <Mods></Mods>
    </div>
  </div>
</QueryClientProvider>
