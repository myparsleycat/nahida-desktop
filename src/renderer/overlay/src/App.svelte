<script lang="ts">
  import { _ } from "svelte-i18n";
  import { toast, Toaster } from "svelte-sonner";
  import { ModeWatcher } from "mode-watcher";
  // import Layout from "./components/Layout.svelte";
  import { createDraggable } from "animejs";
  import Mods from "@/components/mods/Mods.svelte";
  import { QueryClientProvider } from "@tanstack/svelte-query";
  import { queryClient } from "@/queryClient";

  $effect(() => {
    createDraggable(".square");
  });
</script>

<ModeWatcher defaultMode="system" />
<Toaster richColors position="bottom-left" />
<QueryClientProvider client={queryClient}>
  <div class="h-screen w-screen">
    <div
      class="square draggable bg-black/50 rounded-lg p-2 absolute bottom-4 left-4 w-1/3 h-1/3"
    >
      <Mods>
        
      </Mods>
    </div>
  </div>
</QueryClientProvider>
