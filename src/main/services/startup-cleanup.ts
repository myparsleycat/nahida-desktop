import type { NahidaDesktop } from "@/main";

type StartupCleanupTask = {
    name: string;
    run: () => Promise<void>;
};

export class StartupCleanupService {
    private readonly tasks = new Map<string, StartupCleanupTask>();

    constructor(private readonly desktop: NahidaDesktop) {}

    public register(task: StartupCleanupTask) {
        this.tasks.set(task.name, task);
    }

    public async runAll() {
        for (const task of this.tasks.values()) {
            try {
                await task.run();
            } catch (error) {
                this.desktop.logger.error(error, `StartupCleanup:${task.name}`);
            }
        }
    }
}
