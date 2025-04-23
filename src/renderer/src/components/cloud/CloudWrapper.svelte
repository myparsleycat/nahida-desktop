<!-- src/renderer/src/components/cloud/CloudWrapper.svelte -->

<script lang="ts">
  import { Auth, Main } from "$lib/helpers";
  import Cloud from "./Cloud.svelte";
  import { LoaderIcon } from "lucide-svelte";
  import Login from "@/components/Login.svelte";
  import { onDestroy } from "svelte";
  import Layout from "./Layout.svelte";

  let page = Main.page;
  let { checkingSession, loggedIn } = Auth;

  $effect(() => {
    Auth.CheckSessionState()
      .then((res) => {
        loggedIn.set(res);
      })
      .finally(() => {
        checkingSession.set(false);
      });
  });

  $effect(() => {
    const unsubscribeAuthState = window.api.auth.onAuthStateChanged((state) => {
      loggedIn.set(state);
    });

    return () => {
      unsubscribeAuthState();
      Main.cleanupChannel("auth-state-changed");
    };
  });
</script>

{#if $checkingSession}
  <div class="flex justify-center items-center w-full h-full">
    <LoaderIcon class="animate-spin-1.5" size={80} />
  </div>
{:else if !$loggedIn}
  <Login></Login>
{:else}
  <Layout>
    <Cloud></Cloud>
  </Layout>
{/if}
