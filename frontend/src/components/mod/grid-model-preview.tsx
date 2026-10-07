import { Tools } from "@bindings/tools";
import { normalizeModelViewerTransport } from "@renderer/components/tools/model-viewer/model-viewer-transport";
import { useSettings } from "@renderer/hooks/use-settings";
import { Logger } from "@renderer/lib/logger";
import type { ModInfo } from "@renderer/types/mod";
import { uploadTypedArray } from "@renderer/wails/binary-memory";
import { applyVariableSelection, evaluateViewerState } from "@shared/mod-viewer/eval";
import type {
  EvaluatedViewerState,
  ModViewerTransport,
  ViewerStateValue,
} from "@shared/mod-viewer/types";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

import type {
  GridModelPreviewRenderRequest,
  GridModelPreviewRenderResponse,
} from "./grid-model-preview.worker";

const MODEL_PREVIEW_SIZE = 512;
const MODEL_PREVIEW_CACHE_ENTRIES = 64;
const MODEL_PREVIEW_CACHE_BYTES = 64 * 1024 * 1024;
const MODEL_PREVIEW_RENDER_TIMEOUT_MS = 30_000;
const MODEL_PREVIEW_LOOKUP_CONCURRENCY = 4;

const modelPreviewSettingsConfig = {
  enabled: "mod.gridModelPreview",
  toneMapping: "modelViewer.toneMapping",
  environment: "modelViewer.environment",
  exposure: "modelViewer.exposure",
  toonShadows: "modelViewer.toonShadows",
} as const;

export type GridModelPreviewState =
  | { status: "unavailable" }
  | { status: "loading" }
  | { status: "ready"; url: string };

export type ModelPreviewRenderSettings = {
  toneMapping: "neutral" | "aces" | "none";
  environment: "studio" | "soft" | "none";
  exposure: number;
  toonShadows: boolean;
};

type PreviewListener = (state: GridModelPreviewState) => void;

type PreviewEntry = {
  requestKey: string;
  mod: ModInfo;
  listeners: Set<PreviewListener>;
  state: "checking" | "queued" | "loading" | "rendering";
  variant: string;
  fingerprint: string;
  finalKey: string;
};

export type PreviewRenderTask = {
  entry: PreviewEntry;
  evaluated: EvaluatedViewerState;
  finished: boolean;
  sessionId: string;
  timeout: ReturnType<typeof setTimeout> | null;
  transport: ModViewerTransport;
};

type PreviewCacheEntry = {
  size: number;
  url: string;
};

export class GridModelPreviewCache {
  private readonly entries = new Map<string, PreviewCacheEntry>();
  private totalBytes = 0;

  get(key: string): string | undefined {
    const entry = this.entries.get(key);
    if (!entry) {
      return undefined;
    }

    this.entries.delete(key);
    this.entries.set(key, entry);
    return entry.url;
  }

  set(key: string, blob: Blob): string {
    const existing = this.entries.get(key);
    if (existing) {
      this.entries.delete(key);
      this.totalBytes -= existing.size;
      URL.revokeObjectURL(existing.url);
    }

    const url = URL.createObjectURL(blob);
    this.entries.set(key, { size: blob.size, url });
    this.totalBytes += blob.size;
    this.trim();
    return url;
  }

  clear() {
    for (const entry of this.entries.values()) {
      URL.revokeObjectURL(entry.url);
    }
    this.entries.clear();
    this.totalBytes = 0;
  }

  private trim() {
    while (
      this.entries.size > MODEL_PREVIEW_CACHE_ENTRIES ||
      this.totalBytes > MODEL_PREVIEW_CACHE_BYTES
    ) {
      const oldest = this.entries.entries().next().value;
      if (!oldest) {
        return;
      }

      this.entries.delete(oldest[0]);
      this.totalBytes -= oldest[1].size;
      URL.revokeObjectURL(oldest[1].url);
    }
  }
}

export class GridModelPreviewController {
  private readonly failedKeys = new Set<string>();
  private readonly cache = new GridModelPreviewCache();
  private readonly entries = new Map<string, PreviewEntry>();
  private readonly lookupQueue: PreviewEntry[] = [];
  private readonly queue: PreviewEntry[] = [];
  private activeLookups = 0;
  private activeEntry: PreviewEntry | null = null;
  private activeTask: PreviewRenderTask | null = null;
  private disposed = false;

  constructor(
    private readonly settings: ModelPreviewRenderSettings,
    private readonly showRenderTask: (task: PreviewRenderTask | null) => void,
  ) {}

  subscribe(mod: ModInfo, listener: PreviewListener): () => void {
    if (this.disposed) {
      listener({ status: "unavailable" });
      return () => {};
    }

    const requestKey = createGridModelPreviewRequestKey(mod, this.settings);
    const existing = this.entries.get(requestKey);
    const entry = existing ?? {
      requestKey,
      mod,
      listeners: new Set<PreviewListener>(),
      state: "checking" as const,
      variant: createGridModelPreviewRequestKey({ ...mod, mtime: 0 }, this.settings),
      fingerprint: "",
      finalKey: "",
    };
    entry.listeners.add(listener);
    listener({ status: "loading" });

    if (!existing) {
      this.entries.set(requestKey, entry);
      this.lookupQueue.push(entry);
      this.pumpLookups();
    }

    return () => {
      entry.listeners.delete(listener);
      if (entry.listeners.size > 0 || (entry.state !== "checking" && entry.state !== "queued")) {
        return;
      }

      // A lookup already in flight notices the entry is gone when it returns.
      for (const pending of [this.lookupQueue, this.queue]) {
        const index = pending.indexOf(entry);
        if (index >= 0) {
          pending.splice(index, 1);
        }
      }
      this.entries.delete(entry.requestKey);
    };
  }

  async complete(task: PreviewRenderTask, blob: Blob | null, error?: unknown) {
    if (task.finished) {
      return;
    }
    if (task.timeout !== null) {
      clearTimeout(task.timeout);
      task.timeout = null;
    }
    task.finished = true;

    if (error) {
      Logger.capture(
        "mod-grid:model-preview-render",
        {
          modName: task.entry.mod.name,
          modPath: task.entry.mod.path,
          sessionId: task.sessionId,
        },
        error,
      );
    }

    const { entry } = task;
    let state: GridModelPreviewState = { status: "unavailable" };
    if (!this.disposed && blob) {
      const url = this.cache.set(entry.finalKey, blob);
      state = { status: "ready", url };
      if (entry.fingerprint) {
        await saveModelPreview(task, blob).catch((cacheError: unknown) =>
          Logger.capture(
            "mod-grid:model-preview-cache-save",
            { modPath: entry.mod.path },
            cacheError,
          ),
        );
      }
    } else if (!this.disposed && entry.fingerprint) {
      this.failedKeys.add(entry.finalKey);
    }

    await cleanupModelPreviewSession(task.sessionId);
    if (this.disposed) {
      return;
    }
    this.settle(entry, state);

    if (this.activeTask === task) {
      this.activeTask = null;
      this.activeEntry = null;
      this.showRenderTask(null);
    }
    void this.pump();
  }

  dispose() {
    if (this.disposed) {
      return;
    }
    this.disposed = true;

    for (const entry of this.entries.values()) {
      for (const listener of entry.listeners) {
        listener({ status: "unavailable" });
      }
    }
    this.lookupQueue.splice(0);
    this.queue.splice(0);
    this.entries.clear();
    this.failedKeys.clear();
    this.cache.clear();
    this.showRenderTask(null);

    if (this.activeTask) {
      if (this.activeTask.timeout !== null) {
        clearTimeout(this.activeTask.timeout);
        this.activeTask.timeout = null;
      }
      if (!this.activeTask.finished) {
        this.activeTask.finished = true;
        void cleanupModelPreviewSession(this.activeTask.sessionId);
      }
    }
    this.activeTask = null;
  }

  private settle(entry: PreviewEntry, state: GridModelPreviewState) {
    for (const listener of entry.listeners) {
      listener(state);
    }
    this.entries.delete(entry.requestKey);
  }

  // Cache lookups run apart from the render queue so a saved image never
  // waits behind another mod's model load and render.
  private pumpLookups() {
    while (this.activeLookups < MODEL_PREVIEW_LOOKUP_CONCURRENCY) {
      const entry = this.lookupQueue.shift();
      if (!entry) {
        return;
      }
      this.activeLookups++;
      void this.lookup(entry).finally(() => {
        this.activeLookups--;
        this.pumpLookups();
      });
    }
  }

  private async lookup(entry: PreviewEntry) {
    const saved = await Tools.GetModGridPreviewCache(entry.mod.path, entry.variant).catch(
      (error: unknown) => {
        Logger.capture("mod-grid:model-preview-cache-read", { modPath: entry.mod.path }, error);
        return { fingerprint: "", url: "" };
      },
    );
    if (this.disposed || this.entries.get(entry.requestKey) !== entry) {
      return;
    }

    entry.fingerprint = saved.fingerprint;
    entry.finalKey = JSON.stringify([entry.variant, saved.fingerprint]);
    const cached = saved.url || (saved.fingerprint ? this.cache.get(entry.finalKey) : undefined);
    if (cached || this.failedKeys.has(entry.finalKey)) {
      this.settle(entry, cached ? { status: "ready", url: cached } : { status: "unavailable" });
      return;
    }

    entry.state = "queued";
    this.queue.push(entry);
    void this.pump();
  }

  private async pump() {
    if (this.disposed || this.activeEntry) {
      return;
    }

    const entry = this.queue.shift();
    if (!entry) {
      return;
    }

    this.activeEntry = entry;
    entry.state = "loading";
    let sessionId = "";
    try {
      const loaded = await Tools.LoadModGridPreview(entry.mod.path);
      sessionId = loaded.memorySessionId;
      if (this.disposed) {
        await cleanupModelPreviewSession(sessionId);
        this.activeEntry = null;
        return;
      }

      const transport = normalizeModelViewerTransport(loaded);
      const evaluated = evaluateViewerState(
        transport,
        resolveGridModelPreviewState(transport, entry.mod),
      );
      entry.state = "rendering";
      const task: PreviewRenderTask = {
        entry,
        evaluated,
        finished: false,
        sessionId,
        timeout: null,
        transport,
      };
      this.activeTask = task;
      task.timeout = setTimeout(() => {
        void this.complete(task, null, new Error("Model preview render timed out."));
      }, MODEL_PREVIEW_RENDER_TIMEOUT_MS);
      this.showRenderTask(task);
    } catch (error) {
      if (sessionId) {
        await cleanupModelPreviewSession(sessionId);
      }
      if (!this.disposed) {
        Logger.capture(
          "mod-grid:model-preview-load",
          { modName: entry.mod.name, modPath: entry.mod.path, sessionId },
          error,
        );
        if (entry.fingerprint) {
          this.failedKeys.add(entry.finalKey);
        }
        this.settle(entry, { status: "unavailable" });
      }
      this.entries.delete(entry.requestKey);
      this.activeEntry = null;
      void this.pump();
    }
  }
}

type GridModelPreviewContextValue = {
  enabled: boolean;
  subscribe: (mod: ModInfo, listener: PreviewListener) => () => void;
  viewport: HTMLDivElement | null;
};

const GridModelPreviewContext = createContext<GridModelPreviewContextValue>({
  enabled: false,
  subscribe: (_mod, listener) => {
    listener({ status: "unavailable" });
    return () => {};
  },
  viewport: null,
});

// Renders previews in a worker-owned OffscreenCanvas so loading, shader
// compilation, texture upload, and PNG encoding stay off the main thread.
export class GridModelPreviewRenderer {
  private worker: Worker | null = null;
  private pending: {
    id: number;
    resolve: (blob: Blob) => void;
    reject: (error: unknown) => void;
  } | null = null;
  private nextId = 1;

  constructor(private readonly settings: ModelPreviewRenderSettings) {}

  render(task: PreviewRenderTask): Promise<Blob> {
    this.cancel();
    const worker = (this.worker ??= this.start());
    const id = this.nextId++;
    return new Promise((resolve, reject) => {
      this.pending = { id, resolve, reject };
      worker.postMessage({
        id,
        size: MODEL_PREVIEW_SIZE,
        transport: task.transport,
        evaluated: task.evaluated,
        settings: this.settings,
      } satisfies GridModelPreviewRenderRequest);
    });
  }

  // A render cannot be interrupted inside the worker, so abandoning one
  // discards the worker along with its WebGL context.
  cancel() {
    if (this.pending) {
      this.dispose();
    }
  }

  dispose() {
    this.pending = null;
    this.worker?.terminate();
    this.worker = null;
  }

  private start() {
    const worker = new Worker(new URL("./grid-model-preview.worker.ts", import.meta.url), {
      type: "module",
    });
    worker.onmessage = (event: MessageEvent<GridModelPreviewRenderResponse>) => {
      const response = event.data;
      if (response.blob) {
        if (this.pending?.id === response.id) {
          this.pending.resolve(response.blob);
          this.pending = null;
        }
        return;
      }
      this.fail(response.id, new Error(response.message, { cause: response.diagnostic }));
    };
    worker.onerror = (event) =>
      this.fail(
        this.pending?.id,
        new Error(event.message || "Model preview worker failed.", { cause: event.error }),
      );
    return worker;
  }

  // The next preview starts a fresh worker, which recovers from a lost context.
  private fail(id: number | undefined, error: Error) {
    const pending = this.pending;
    if (!pending || pending.id !== id) {
      return;
    }
    this.dispose();
    pending.reject(error);
  }
}

export function GridModelPreviewProvider({
  children,
  viewport,
}: {
  children: ReactNode;
  viewport: HTMLDivElement | null;
}) {
  const { settings, isLoading } = useSettings(modelPreviewSettingsConfig);
  const enabled = !isLoading && settings.enabled;
  const renderSettings = useMemo<ModelPreviewRenderSettings | null>(
    () =>
      enabled
        ? {
            toneMapping: settings.toneMapping,
            environment: settings.environment,
            exposure: settings.exposure,
            toonShadows: settings.toonShadows,
          }
        : null,
    [enabled, settings.environment, settings.exposure, settings.toneMapping, settings.toonShadows],
  );
  const preview = useMemo(() => {
    if (!renderSettings) {
      return null;
    }

    const renderer = new GridModelPreviewRenderer(renderSettings);
    const controller: GridModelPreviewController = new GridModelPreviewController(
      renderSettings,
      (task) => {
        if (!task) {
          renderer.cancel();
          return;
        }
        renderer.render(task).then(
          (blob) => controller.complete(task, blob),
          (error: unknown) => controller.complete(task, null, error),
        );
      },
    );
    return { controller, renderer };
  }, [renderSettings]);

  useEffect(
    () => () => {
      preview?.controller.dispose();
      preview?.renderer.dispose();
    },
    [preview],
  );

  const subscribe = useCallback(
    (mod: ModInfo, listener: PreviewListener) =>
      preview?.controller.subscribe(mod, listener) ?? (() => {}),
    [preview],
  );
  const value = useMemo(() => ({ enabled, subscribe, viewport }), [enabled, subscribe, viewport]);

  return (
    <GridModelPreviewContext.Provider value={value}>{children}</GridModelPreviewContext.Provider>
  );
}

export function useGridModelPreview(mod: ModInfo, eligible: boolean) {
  const context = useContext(GridModelPreviewContext);
  const [node, setNode] = useState<HTMLDivElement | null>(null);
  const identity = createGridModelPreviewIdentity(mod);
  const [result, setResult] = useState<{
    identity: string;
    state: GridModelPreviewState;
  }>({ identity, state: { status: "unavailable" } });
  const enabled = eligible && context.enabled;

  useEffect(() => {
    if (!enabled || !node) {
      return;
    }

    let unsubscribe: (() => void) | undefined;
    const subscribe = () => {
      unsubscribe ??= context.subscribe(mod, (state) => setResult({ identity, state }));
    };
    const unsubscribeCurrent = () => {
      unsubscribe?.();
      unsubscribe = undefined;
    };

    if (typeof IntersectionObserver === "undefined") {
      subscribe();
      return unsubscribeCurrent;
    }

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) {
          subscribe();
        } else {
          unsubscribeCurrent();
        }
      },
      { root: context.viewport, rootMargin: "800px 0px" },
    );
    observer.observe(node);

    return () => {
      observer.disconnect();
      unsubscribeCurrent();
    };
  }, [context, enabled, identity, mod, node]);

  const state =
    enabled && result.identity === identity ? result.state : ({ status: "unavailable" } as const);
  return [setNode, enabled, state] as const;
}

function createGridModelPreviewIdentity(mod: ModInfo) {
  return JSON.stringify([
    mod.path,
    mod.mtime,
    mod.inis.flatMap((ini) =>
      ini.toggleKeys.map((toggle) => [toggle.iniFileName, toggle.variable, toggle.currentValue]),
    ),
  ]);
}

export function resolveGridModelPreviewState(
  transport: ModViewerTransport,
  mod: ModInfo,
): Record<string, ViewerStateValue> {
  const variables = new Map(
    transport.variables.map((variable) => [variable.id.toLowerCase(), variable] as const),
  );
  let state = { ...transport.defaultState };

  for (const ini of mod.inis) {
    for (const toggle of ini.toggleKeys) {
      if (toggle.currentValue === undefined) {
        continue;
      }

      const variableName = toggle.variable.replace(/^\$+/, "");
      const iniStem = toggle.iniFileName.replace(/^.*[\\/]/, "").replace(/\.[^.]+$/, "");
      const variable =
        variables.get(`${iniStem}::${variableName}`.toLowerCase()) ??
        variables.get(variableName.toLowerCase());
      if (!variable) {
        continue;
      }

      state = applyVariableSelection(state, variable, toggle.currentValue);
    }
  }

  return state;
}

export function createGridModelPreviewRequestKey(
  mod: ModInfo,
  settings: ModelPreviewRenderSettings,
) {
  const toggles = mod.inis.flatMap((ini) =>
    ini.toggleKeys.map((toggle) => [
      toggle.iniFileName.toLowerCase(),
      toggle.variable.toLowerCase(),
      toggle.currentValue ?? null,
    ]),
  );
  return JSON.stringify([mod.path.toLowerCase(), mod.mtime, toggles, settings, "512@1-v1"]);
}

async function saveModelPreview(task: PreviewRenderTask, blob: Blob) {
  const { entry, sessionId } = task;
  const uploadUrl = await Tools.PrepareModGridPreviewCacheUpload(sessionId, blob.size);
  await uploadTypedArray(uploadUrl, new Uint8Array(await blob.arrayBuffer()));
  await Tools.SaveModGridPreviewCache(entry.mod.path, entry.variant, entry.fingerprint, sessionId);
}

async function cleanupModelPreviewSession(sessionId: string) {
  if (!sessionId) {
    return;
  }
  try {
    await Tools.CleanupModelViewer(sessionId);
  } catch (error) {
    Logger.capture("mod-grid:model-preview-cleanup", { sessionId }, error);
  }
}
