import { type ChildProcess, spawn } from "node:child_process";

export class ToolExecutor {
    private currentProcess: ChildProcess | null = null;

    constructor(private onLog: (msg: string) => void) {}

    private stripAnsi(str: string): string {
        // biome-ignore lint/suspicious/noControlCharactersInRegex: ansi stripping
        const ansiRegex = /[\x1B\x9B][[()#;?]*(?:[0-9]{1,4}(?:;[0-9]{0,4})*)?[0-9A-ORZcf-nqry=><]/g;
        return str.replace(ansiRegex, "");
    }

    public async execute(
        filePath: string,
        type: "python" | "exec",
        cwd: string,
        signal?: AbortSignal,
    ): Promise<void> {
        return new Promise<void>((resolve, reject) => {
            if (signal?.aborted) {
                return reject(new Error("Aborted"));
            }

            const env = { ...process.env, PYTHONIOENCODING: "utf-8" };
            let child: ChildProcess;

            if (type === "python") {
                child = spawn("python", [filePath], { windowsHide: true, cwd, env });
            } else {
                if (process.platform === "win32") {
                    child = spawn("cmd.exe", ["/c", "start", "/wait", "", filePath], {
                        windowsHide: true,
                        cwd,
                        env,
                    });
                } else {
                    child = spawn(filePath, [], { windowsHide: false, cwd, env });
                }
            }

            this.currentProcess = child;

            if (signal) {
                signal.addEventListener("abort", () => {
                    try {
                        if (process.platform === "win32" && child.pid) {
                            spawn("taskkill", ["/pid", child.pid.toString(), "/f", "/t"]);
                        } else {
                            child.kill();
                        }
                    } catch {
                        // ignore
                    }
                    reject(new Error("Aborted"));
                });
            }

            let stdoutBuffer = "";
            let stderrBuffer = "";

            const processOutput = (data: Buffer, bufferContext: "stdout" | "stderr") => {
                let currentBuffer = bufferContext === "stdout" ? stdoutBuffer : stderrBuffer;

                const chunk = data.toString();
                currentBuffer += chunk;

                currentBuffer = this.stripAnsi(currentBuffer);

                const lastEscIndex = currentBuffer.lastIndexOf("\x1B");

                const lowerStr = currentBuffer.toLowerCase();

                if (
                    lowerStr.includes("press any key") ||
                    lowerStr.includes('press "enter" to quit') ||
                    lowerStr.includes("done")
                ) {
                    this.sendInput("\n");
                }

                if (lastEscIndex !== -1) {
                    const safePart = currentBuffer.substring(0, lastEscIndex);
                    const incompletePart = currentBuffer.substring(lastEscIndex);

                    if (incompletePart.length > 50) {
                        const message = currentBuffer.trim();
                        if (message) this.onLog(message);
                        currentBuffer = "";
                    } else {
                        const message = safePart.trim();
                        if (message) this.onLog(message);
                        currentBuffer = incompletePart;
                    }
                } else {
                    const message = currentBuffer.trim();
                    if (message) this.onLog(message);
                    currentBuffer = "";
                }

                if (bufferContext === "stdout") {
                    stdoutBuffer = currentBuffer;
                } else {
                    stderrBuffer = currentBuffer;
                }
            };

            child.stdout?.on("data", (data) => processOutput(data, "stdout"));
            child.stderr?.on("data", (data) => processOutput(data, "stderr"));

            child.on("close", (code) => {
                if (stdoutBuffer.trim()) this.onLog(stdoutBuffer.trim());
                if (stderrBuffer.trim()) this.onLog(stderrBuffer.trim());

                this.currentProcess = null;

                if (code === 0) {
                    resolve();
                } else {
                    reject(new Error(`Process exited with code ${code}`));
                }
            });

            child.on("error", (err) => {
                this.currentProcess = null;
                reject(err);
            });
        });
    }

    public sendInput(input: string) {
        const stdin = this.currentProcess?.stdin;
        if (stdin && !stdin.destroyed) {
            stdin.write(input);
            return true;
        }
        return false;
    }

    public isRunning(): boolean {
        return this.currentProcess !== null;
    }
}
