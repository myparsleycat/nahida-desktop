<script lang="ts">
  import { cn } from "@/lib/utils";
  import Cloud from "./cloud/Cloud.svelte";
  import {
    CloudIcon,
    HardDriveIcon,
    ImageIcon,
    Loader2Icon,
    LogIn,
    LogOutIcon,
    SettingsIcon,
  } from "lucide-svelte";
  import { Auth, Main } from "@/lib/helpers";
  import General from "./general/General.svelte";
  import { toast } from "svelte-sonner";
  import autoAnimate from "@formkit/auto-animate";
  import Mods from "./mods/Mods.svelte";

  let { checkingSession, loggedIn } = Auth;

  type type = "general" | "mods" | "cloud";
  let page = $state<type>("general");

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

<div class="flex flex-row w-full h-full select-none">
  <div
    class="border-r h-full w-64 flex-col justify-center p-3 duration-200 space-y-1"
  >
    <button
      class={cn(
        "flex gap-2 p-2 rounded-lg w-full hover:bg-muted duration-200",
        page === "general" && "bg-muted",
      )}
      onclick={() => (page = "general")}
    >
      <SettingsIcon size={22} />
      일반
    </button>

    <button
      class={cn(
        "flex gap-2 p-2 rounded-lg w-full hover:bg-muted duration-200",
        page === "mods" && "bg-muted",
      )}
      onclick={() => (page = "mods")}
    >
      <HardDriveIcon size={22} />
      모드
    </button>

    <button
      class={cn(
        "flex gap-2 p-2 rounded-lg w-full hover:bg-muted duration-200",
        page === "cloud" && "bg-muted",
      )}
      onclick={() => (page = "cloud")}
    >
      <CloudIcon size={22} />
      클라우드
    </button>

    <button
      class={cn("flex gap-2 p-2 rounded-lg w-full hover:bg-muted duration-200")}
      onclick={async () => {
        try {
          if ($loggedIn) {
            await Auth.Logout();
          } else {
            await Auth.StartOAuth2Login();
          }
        } catch (e: any) {
          toast.error(e.message);
        }
      }}
    >
      <p class="flex gap-2 whitespace-nowrap" use:autoAnimate>
        {#if $checkingSession}
          <Loader2Icon size={22} class="animate-spin-1.5" />
        {:else if $loggedIn}
          <LogOutIcon size={22} />
          로그아웃
        {:else}
          <LogIn size={22} />
          로그인
        {/if}
      </p>
    </button>
  </div>

  <div class="flex flex-col p-6 h-full w-full items-center">
    <div class="max-w-3xl w-full">
      {#if page === "general"}
        <General />
      {:else if page === "mods"}
        <Mods />
      {:else if page === "cloud"}
        <Cloud />
      {/if}
    </div>
  </div>
</div>
