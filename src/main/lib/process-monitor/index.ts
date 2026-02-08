import koffi from "koffi";
import path from "node:path";
import { EventEmitter } from "node:events";
import { is } from "@electron-toolkit/utils";

const dllPath = is.dev
    ? path.join(
          process.cwd(),
          "build",
          "process-monitor",
          "build",
          "Release",
          "process_monitor.dll",
      )
    : path.join(process.resourcesPath, "lib", "process_monitor.dll");

try {
    koffi.proto("void EventCallback(bool isCreation, const char* processName, uint32_t pid)");
} catch (e) {
    // ignore duplicate type error
}

const lib = koffi.load(dllPath);

const StartMonitoring = lib.func(
    "bool StartMonitoring(bool watchCreation, bool watchDeletion, EventCallback* callback)",
);
const StopMonitoring = lib.func("void StopMonitoring()");
const IsMonitoring = lib.func("bool IsMonitoring()");

class ProcessMonitor extends EventEmitter {
    private callback: any = null;

    async subscribe(options: { creation?: boolean; deletion?: boolean }) {
        if (this.callback) {
            throw new Error("Already subscribed. Call unsubscribe first.");
        }

        const watchCreation = options.creation ?? false;
        const watchDeletion = options.deletion ?? false;

        if (!watchCreation && !watchDeletion) {
            throw new Error("At least one of creation or deletion must be true");
        }

        this.callback = koffi.register((isCreation: boolean, processName: string, pid: number) => {
            const eventType = isCreation ? "creation" : "deletion";
            this.emit(eventType, [processName, pid]);
        }, koffi.pointer("EventCallback"));

        const success = StartMonitoring(watchCreation, watchDeletion, this.callback);

        if (!success) {
            koffi.unregister(this.callback);
            this.callback = null;
            throw new Error("Failed to start process monitoring");
        }

        return this;
    }

    unsubscribe() {
        if (!this.callback) {
            return;
        }

        StopMonitoring();
        koffi.unregister(this.callback);
        this.callback = null;
        this.removeAllListeners();
    }

    isMonitoring(): boolean {
        return IsMonitoring();
    }
}

export async function subscribe(options: { creation?: boolean; deletion?: boolean }) {
    const monitor = new ProcessMonitor();
    await monitor.subscribe(options);
    return monitor;
}

export { ProcessMonitor };
