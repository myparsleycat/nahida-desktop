<script lang="ts">
  import { _ } from "svelte-i18n";
  import { Toaster } from "svelte-sonner";
  import { ModeWatcher } from "mode-watcher";
  import Layout from "./components/Layout.svelte";
  import { onDestroy, onMount } from "svelte";
  import { toast, type ExternalToast } from "svelte-sonner";
  import { ModsHelper } from "./lib/helpers";
  import { QueryClientProvider } from "@tanstack/svelte-query";
  import { queryClient } from "./queryClient";
  import { CharPathSelector } from "./lib/stores/global.store";
  import { ListenersManager } from "./listeners";

  onMount(async () => {
    await ListenersManager.init();
  });

  onDestroy(() => {
    ListenersManager.destroy();
  });
</script>

<ModeWatcher defaultMode="system" />
<Toaster richColors />

<QueryClientProvider client={queryClient}>
  <main class="h-[calc(100vh-1.5rem)]">
    <Layout></Layout>
  </main>
</QueryClientProvider>
