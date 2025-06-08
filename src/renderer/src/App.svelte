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

  let listeners: (() => void)[] = [];

  const getSelectedCharPath = (): string => {
    console.log("Renderer: getSelectedCharPath called");
    return "test path";
  };

  onMount(() => {
    const folderPathListener = window.api.mods.msg.currentFolderPathChanged(
      (resp) => {
        ModsHelper.currentFolderPath.set(resp);
      },
    );
    listeners.push(folderPathListener);

    const charPathListener = window.api.mods.msg.currentCharPathChanged(
      (resp) => {
        ModsHelper.currentCharPath.set(resp);
      },
    );
    listeners.push(charPathListener);

    const toastListener = window.api.toast.toastShow(
      (params: {
        message: string;
        type?: "default" | "success" | "error" | "warning" | "info";
        data?: ExternalToast;
      }) => {
        const { message, type = "default", data } = params;

        switch (type) {
          case "success":
            toast.success(message, data);
            break;
          case "error":
            toast.error(message, data);
            break;
          case "warning":
            toast.warning(message, data);
            break;
          case "info":
            toast.info(message, data);
            break;
          default:
            toast(message, data);
        }
      },
    );
    listeners.push(toastListener);

    const charPathRequestListener = window.api.renderer.requestCharPath(
      (data: any) => {
        const { requestId } = data;
        const result = getSelectedCharPath();

        window.api.renderer.charPathResponse(requestId, {
          success: true,
          data: result,
        });
      },
    );
    listeners.push(charPathRequestListener);
  });

  const clearListeners = () => {
    listeners.forEach((removeListener) => {
      removeListener();
    });
    listeners = [];
  };

  onDestroy(() => {
    clearListeners();
  });
</script>

<ModeWatcher defaultMode="system" />
<Toaster richColors />

<QueryClientProvider client={queryClient}>
  <main class="h-[calc(100vh-1.5rem)]">
    <Layout></Layout>
  </main>
</QueryClientProvider>
