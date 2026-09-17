import { Tools } from "@bindings/tools";
import type { ModelViewerHandle } from "@renderer/components/tools/model-viewer/model-viewer-contract";
import { DEFAULT_MODEL_ORIENTATION } from "@renderer/components/tools/model-viewer/model-viewer-dialog-types";
import { normalizeModelViewerTransport } from "@renderer/components/tools/model-viewer/model-viewer-transport";
import { ThreeModelViewer } from "@renderer/components/tools/model-viewer/three-model-viewer";
import { useSettings } from "@renderer/hooks/use-settings";
import { Logger } from "@renderer/lib/logger";
import type { ModInfo } from "@renderer/types/mod";
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
  useRef,
  useState,
} from "react";

const MODEL_PREVIEW_SIZE = 512;
const MODEL_PREVIEW_CACHE_ENTRIES = 64;
const MODEL_PREVIEW_CACHE_BYTES = 64 * 1024 * 1024;

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

type ModelPreviewRenderSettings = {
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
  state: "queued" | "loading" | "rendering";
};

export type PreviewRenderTask = {
  controllerId: number;
  entry: PreviewEntry;
  evaluated: EvaluatedViewerState;
  finalKey: string;
  finished: boolean;
  sessionId: string;
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
  private readonly aliases = new Map<string, string | null>();
  private readonly cache = new GridModelPreviewCache();
  private readonly entries = new Map<string, PreviewEntry>();
  private readonly queue: PreviewEntry[] = [];
  private activeEntry: PreviewEntry | null = null;
  private activeTask: PreviewRenderTask | null = null;
  private disposed = false;

  constructor(
    readonly id: number,
    private readonly settings: ModelPreviewRenderSettings,
    private readonly showRenderTask: (task: PreviewRenderTask | null) => void,
  ) {}

  subscribe(mod: ModInfo, listener: PreviewListener): () => void {
    if (this.disposed) {
      listener({ status: "unavailable" });
      return () => {};
    }

    const requestKey = createGridModelPreviewRequestKey(mod, this.settings);
    const alias = this.aliases.get(requestKey);
    if (alias === null) {
      listener({ status: "unavailable" });
      return () => {};
    }
    if (alias) {
      const url = this.cache.get(alias);
      if (url) {
        listener({ status: "ready", url });
        return () => {};
      }
      this.aliases.delete(requestKey);
    }

    const existing = this.entries.get(requestKey);
    const entry = existing ?? {
      requestKey,
      mod,
      listeners: new Set<PreviewListener>(),
      state: "queued" as const,
    };
    entry.listeners.add(listener);
    listener({ status: "loading" });

    if (!existing) {
      this.entries.set(requestKey, entry);
      this.queue.push(entry);
      void this.pump();
    }

    return () => {
      entry.listeners.delete(listener);
      if (entry.state !== "queued" || entry.listeners.size > 0) {
        return;
      }

      const index = this.queue.indexOf(entry);
      if (index >= 0) {
        this.queue.splice(index, 1);
      }
      this.entries.delete(entry.requestKey);
    };
  }

  async complete(task: PreviewRenderTask, blob: Blob | null, error?: unknown) {
    if (task.finished) {
      return;
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

    let state: GridModelPreviewState = { status: "unavailable" };
    if (!this.disposed && blob) {
      const url = this.cache.set(task.finalKey, blob);
      this.aliases.set(task.entry.requestKey, task.finalKey);
      state = { status: "ready", url };
    } else if (!this.disposed) {
      this.aliases.set(task.entry.requestKey, null);
    }

    await cleanupModelPreviewSession(task.sessionId);
    for (const listener of task.entry.listeners) {
      listener(state);
    }
    this.entries.delete(task.entry.requestKey);

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
    this.queue.splice(0);
    this.entries.clear();
    this.aliases.clear();
    this.cache.clear();
    this.showRenderTask(null);

    if (this.activeTask && !this.activeTask.finished) {
      this.activeTask.finished = true;
      void cleanupModelPreviewSession(this.activeTask.sessionId);
    }
    this.activeTask = null;
  }

  private async pump() {
    if (this.disposed || this.activeEntry) {
      return;
    }

    const entry = this.queue.shift();
    if (!entry) {
      return;
    }
    if (entry.listeners.size === 0) {
      this.entries.delete(entry.requestKey);
      void this.pump();
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
      const finalKey = createGridModelPreviewFinalKey(entry.mod, evaluated.state, this.settings);
      const cached = this.cache.get(finalKey);
      if (cached) {
        this.aliases.set(entry.requestKey, finalKey);
        await cleanupModelPreviewSession(sessionId);
        for (const listener of entry.listeners) {
          listener({ status: "ready", url: cached });
        }
        this.entries.delete(entry.requestKey);
        this.activeEntry = null;
        void this.pump();
        return;
      }

      entry.state = "rendering";
      const task: PreviewRenderTask = {
        controllerId: this.id,
        entry,
        evaluated,
        finalKey,
        finished: false,
        sessionId,
        transport,
      };
      this.activeTask = task;
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
        this.aliases.set(entry.requestKey, null);
        for (const listener of entry.listeners) {
          listener({ status: "unavailable" });
        }
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

let nextModelPreviewControllerId = 1;

export function GridModelPreviewProvider({
  children,
  viewport,
}: {
  children: ReactNode;
  viewport: HTMLDivElement | null;
}) {
  const { settings, isLoading } = useSettings(modelPreviewSettingsConfig);
  const [renderTask, setRenderTask] = useState<PreviewRenderTask | null>(null);
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
  const controller = useMemo(() => {
    if (!renderSettings) {
      return null;
    }

    const id = nextModelPreviewControllerId++;
    return new GridModelPreviewController(id, renderSettings, (task) => {
      setRenderTask((current) => {
        if (task) {
          return task;
        }
        return current?.controllerId === id ? null : current;
      });
    });
  }, [renderSettings]);

  useEffect(() => () => controller?.dispose(), [controller]);

  const subscribe = useCallback(
    (mod: ModInfo, listener: PreviewListener) => controller?.subscribe(mod, listener) ?? (() => {}),
    [controller],
  );
  const value = useMemo(() => ({ enabled, subscribe, viewport }), [enabled, subscribe, viewport]);

  return (
    <GridModelPreviewContext.Provider value={value}>
      {children}
      {controller && renderSettings && renderTask?.controllerId === controller.id ? (
        <GridModelPreviewRenderer
          controller={controller}
          settings={renderSettings}
          task={renderTask}
        />
      ) : null}
    </GridModelPreviewContext.Provider>
  );
}

function GridModelPreviewRenderer({
  controller,
  settings,
  task,
}: {
  controller: GridModelPreviewController;
  settings: ModelPreviewRenderSettings;
  task: PreviewRenderTask | null;
}) {
  const viewerRef = useRef<ModelViewerHandle | null>(null);
  const captureTaskRef = useRef<PreviewRenderTask | null>(null);

  const capture = useCallback(() => {
    if (!task || captureTaskRef.current === task) {
      return;
    }
    captureTaskRef.current = task;

    void (async () => {
      const viewer = viewerRef.current;
      if (!viewer) {
        throw new Error("Model preview renderer is unavailable.");
      }
      await viewer.setDoubleSided(true);
      await viewer.updateFraming();
      const blob = await viewer.captureSquarePngBlob();
      if (!blob) {
        throw new Error("Model preview capture returned no image.");
      }
      await controller.complete(task, blob);
    })().catch((error: unknown) => controller.complete(task, null, error));
  }, [controller, task]);

  return (
    <div
      aria-hidden="true"
      className="pointer-events-none fixed top-0 left-0 -z-50 overflow-hidden opacity-0"
      inert
      style={{ height: MODEL_PREVIEW_SIZE, width: MODEL_PREVIEW_SIZE }}
    >
      {task ? (
        <ThreeModelViewer
          ref={viewerRef}
          className="h-full w-full"
          orientation={DEFAULT_MODEL_ORIENTATION}
          pixelRatio={1}
          payloadTransport={task.transport}
          payloadEval={task.evaluated}
          threeToneMapping={settings.toneMapping}
          threeEnvironment={settings.environment}
          threeExposure={settings.exposure}
          toonShadows={settings.toonShadows}
          onLoad={capture}
          onError={(error) => void controller.complete(task, null, error)}
        />
      ) : null}
    </div>
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
  return JSON.stringify([mod.path.toLowerCase(), mod.mtime, toggles, settings, "512@1"]);
}

export function createGridModelPreviewFinalKey(
  mod: ModInfo,
  state: Record<string, ViewerStateValue>,
  settings: ModelPreviewRenderSettings,
) {
  const sortedState = Object.fromEntries(
    Object.entries(state).sort(([left], [right]) => left.localeCompare(right)),
  );
  return JSON.stringify([mod.path.toLowerCase(), mod.mtime, sortedState, settings, "512@1"]);
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
