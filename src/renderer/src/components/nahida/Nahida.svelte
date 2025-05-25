<script lang="ts">
  import { NahidaHelper } from "@/lib/helpers/nahida.helper";
  import { createInfiniteQuery } from "@tanstack/svelte-query";
  import { inview } from "svelte-inview";
  import PreviewModalButton from "../mods/PreviewModalEntry.svelte";
  import { cn } from "@/lib/utils";
  import { Badge } from "@/lib/components/ui/badge";
  import { onDestroy } from "svelte";
  import { queryClient } from "@/queryClient";
  import { Skeleton } from "$lib/components/ui/skeleton";

  let timestamp = $state(Date.now());

  const query = createInfiniteQuery({
    queryKey: ["mods"],
    queryFn: async ({ pageParam }) =>
      await NahidaHelper.get.mods({ page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) => {
      const currentPage = lastPage.data.cp;
      const totalPage = lastPage.data.tp;
      return currentPage < totalPage ? currentPage + 1 : undefined;
    },
  });

  onDestroy(() => {
    queryClient.removeQueries({ queryKey: ["mods"] });
  });
</script>

<div class="w-full overflow-y-auto px-1.5">
  {#if $query.isSuccess}
    <div
      class="grid grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5 gap-4 h-full overflow-y-auto pr-1.5 py-2.5 px-1"
    >
      {#each $query.data.pages as { data: { r } }}
        {#each r as mod}
          <div
            class="rounded-lg shadow hover:shadow-lg duration-200 transition-all h-min overflow-hidden group"
          >
            <div
              class="relative flex justify-center items-center aspect-square duration-200 transition-all overflow-hidden"
            >
              <div class="absolute inset-0 w-full h-full">
                <img
                  class="w-full h-full object-cover blur scale-110"
                  src={`nahida://external-image?url=${encodeURIComponent(`${mod.preview_url}`)}&t=${timestamp}`}
                  alt={mod.title}
                  loading="lazy"
                />
              </div>
              <img
                class="relative object-contain w-full h-full"
                src={`nahida://external-image?url=${encodeURIComponent(`${mod.preview_url}`)}&t=${timestamp}`}
                alt={mod.title}
                loading="lazy"
              />
              <div
                class="absolute right-1 top-1 z-20 opacity-0 group-hover:opacity-100 transition-opacity duration-200"
              >
                <PreviewModalButton
                  src={`nahida://external-image?url=${encodeURIComponent(`${mod.preview_url}`)}&t=${timestamp}`}
                  alt={`${mod.title} Modal`}
                />
              </div>

              <div
                class="absolute inset-0 flex flex-col justify-between p-4 duration-300 bg-black/35 opacity-0 group-hover:opacity-100"
                style="transition-property: opacity; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1)"
              >
                <div>
                  <h2 class="text-xl font-bold text-white mb-2 pr-10">
                    {mod.title}
                  </h2>
                  <p class="break-words text-white">
                    {mod.description || ""}
                  </p>
                </div>
                <div>
                  <div
                    class="flex justify-end mt-2 flex-wrap gap-2 select-none"
                  >
                    {#if mod.tags && mod.tags.length > 0}
                      {#each mod.tags as tag}
                        <Badge
                          variant={/^(r18|nsfw|19)$/i.test(tag)
                            ? "destructive"
                            : "white"}
                        >
                          {tag}
                        </Badge>
                      {/each}
                    {/if}
                  </div>
                  <div class="mt-2">
                    <span class="text-sm text-white">
                      {new Date(mod.uploaded_at * 1000).toLocaleString(
                        "ko-KR",
                        {
                          year: "numeric",
                          month: "long",
                          day: "numeric",
                          hour: "numeric",
                          minute: "numeric",
                          hour12: true,
                        },
                      )}
                    </span>
                  </div>
                </div>

                <!-- <div
                  class="absolute top-3 right-3 rounded-full p-1"
                  href={`https://arca.live/b/${mod.game === "wuwa" ? "thingzyoa" : "genshinskinmode"}?target=all&keyword=${encodeURIComponent(mod.title)}`}
                  target="_blank"
                >
                  <SearchIcon class="h-6 w-6 text-white" />
                </div> -->
              </div>
            </div>
          </div>
        {/each}
      {/each}

      <div
        class="h-4"
        use:inview={{ unobserveOnEnter: false }}
        oninview_enter={(_e) => {
          $query.fetchNextPage();
        }}
      ></div>
    </div>
  {:else if $query.isLoading}
    <div
      class="grid grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5 gap-4 h-full overflow-y-auto pr-1.5"
    >
      {#each Array(12) as _, idx}
        <Skeleton class="size-full rounded-lg" />
      {/each}
    </div>
  {/if}
</div>
