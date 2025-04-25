<script lang="ts">
  import { _ } from "svelte-i18n";
  import { Toaster } from "svelte-sonner";
  import { ModeWatcher } from "mode-watcher";
  import Layout from "./components/Layout.svelte";
</script>

<ModeWatcher defaultMode="system" />
<Toaster richColors />

<main class="h-[calc(100vh-1.5rem)]">
  <Layout></Layout>
</main>
