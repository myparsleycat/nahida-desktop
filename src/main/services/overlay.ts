import { EventEmitter } from "node:events";
import type { NahidaDesktop } from "@main/index";
import { type NativeOverlayEvent, OverlayController, type WindowRect } from "@native/overlay";
import { globalShortcut } from "electron";

export type { NativeOverlayEvent, WindowRect };

export class OverlayService extends EventEmitter {
    private readonly desktop: NahidaDesktop;
    private controller: OverlayController;
    private trackId: string | number | null = null;

    public get currentTrackId() {
        return this.trackId;
    }

    public isVisible: boolean = true;

    constructor(desktop: NahidaDesktop) {
        super();
        this.desktop = desktop;
        this.controller = new OverlayController();
        this.updateSettings();
    }

    private isGameFocused = false;
    private isOverlayFocused = false;
    private readonly interactiveKeys = ["w", "a", "s", "d"];
    private toggleKey = "Alt+A";
    private enabled = true;

    public async updateSettings() {
        const oldToggleKey = this.toggleKey;
        this.enabled = await this.desktop.setting.overlay.getEnabled();
        this.toggleKey = (await this.desktop.setting.overlay.getToggleKey()) as string;

        if (oldToggleKey !== this.toggleKey) {
            globalShortcut.unregister(oldToggleKey);
        }

        if (!this.enabled && this.isVisible) {
            this.isVisible = false;
            this.emit("toggle-visible", this.isVisible);
        }

        this.updateShortcuts();
    }

    public start(targetTitle: string) {
        if (this.trackId === targetTitle) return;

        this.stop();
        this.trackId = targetTitle;
        this.isVisible = true;
        this.isGameFocused = false;
        this.isOverlayFocused = false;

        this.desktop.logger.info(
            `Starting overlay tracker for title: ${targetTitle}`,
            "OverlayService",
        );

        this.controller.start(targetTitle, (err: Error | null, event: NativeOverlayEvent) => {
            this.handleOverlayEvent(err, event);
        });
    }

    public startByPid(pid: number) {
        if (this.trackId === pid) return;

        this.stop();
        this.trackId = pid;
        this.isVisible = true;
        this.isGameFocused = false;
        this.isOverlayFocused = false;

        this.desktop.logger.info(`Starting overlay tracker for PID: ${pid}`, "OverlayService");

        this.controller.startByPid(pid, (err: Error | null, event: NativeOverlayEvent) => {
            this.handleOverlayEvent(err, event);
        });
    }

    private handleOverlayEvent(err: Error | null, event: NativeOverlayEvent) {
        if (err) {
            this.desktop.logger.error(`Overlay Tracker Error: ${err}`, "OverlayService");
            return;
        }

        if (event.event === "focus") {
            this.isGameFocused = true;
            this.updateShortcuts();
        } else if (event.event === "blur") {
            this.isGameFocused = false;
            this.updateShortcuts();
        }

        this.emit(event.event, event.rect);
        this.emit("event", event);
    }

    public stop() {
        if (!this.trackId) return;

        this.desktop.logger.info(`Stopping overlay tracker for: ${this.trackId}`, "OverlayService");
        this.controller.stop();
        this.trackId = null;
        this.isGameFocused = false;
        this.isOverlayFocused = false;
        this.updateShortcuts();
    }

    public toggle() {
        this.isVisible = !this.isVisible;

        if (!this.isVisible && this.isOverlayFocused) {
            this.isGameFocused = true;
        }

        this.emit("toggle-visible", this.isVisible);
        this.updateShortcuts();
    }

    public setOverlayFocused(focused: boolean) {
        this.isOverlayFocused = focused;
        this.updateShortcuts();
    }

    private updateShortcuts() {
        const anyFocused = this.isGameFocused || this.isOverlayFocused;

        if (anyFocused && this.enabled) {
            if (!globalShortcut.isRegistered(this.toggleKey)) {
                try {
                    const ret = globalShortcut.register(this.toggleKey, () => {
                        this.toggle();
                    });
                    if (!ret) {
                        this.desktop.logger.warn(
                            `Failed to register global shortcut ${this.toggleKey}`,
                            "OverlayService",
                        );
                    }
                } catch (e) {
                    this.desktop.logger.error(
                        `Error registering shortcut ${this.toggleKey}: ${e}`,
                        "OverlayService",
                    );
                }
            }
        } else {
            if (globalShortcut.isRegistered(this.toggleKey)) {
                globalShortcut.unregister(this.toggleKey);
            }
        }

        if (this.enabled && this.isGameFocused && this.isVisible && !this.isOverlayFocused) {
            for (const key of this.interactiveKeys) {
                if (!globalShortcut.isRegistered(key)) {
                    globalShortcut.register(key, () => {
                        this.emit("input", key);
                    });
                }
            }
        } else {
            for (const key of this.interactiveKeys) {
                if (globalShortcut.isRegistered(key)) {
                    globalShortcut.unregister(key);
                }
            }
        }
    }
}

export default OverlayService;
