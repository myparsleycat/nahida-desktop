import { type ChildProcess, spawn } from "node:child_process";

export class ScriptExecutor {
    private currentProcess: ChildProcess | null = null;
    private stdoutBuffer = "";
    private stderrBuffer = "";
    private readonly stdoutDecoder = new TextDecoder("utf-8");
    private readonly stderrDecoder = new TextDecoder("utf-8");

    constructor(private onLog: (msg: string) => void) {}

    private stripAnsi(str: string): string {
        // biome-ignore lint/suspicious/noControlCharactersInRegex: ansi stripping
        const ansiRegex = /[\x1B\x9B][[()#;?]*(?:[0-9]{1,4}(?:;[0-9]{0,4})*)?[0-9A-ORZcf-nqry=><]/g;
        return str.replace(ansiRegex, "");
    }

    private checkAndAutoReply(text: string) {
        const lowerText = text.toLowerCase();
        const keywords = ["press any key", 'press "enter" to quit', "done"];

        const shouldReply = keywords.some((keyword) => lowerText.includes(keyword));

        if (shouldReply) {
            this.sendInput("\n");
        }
    }

    private handleStreamData(data: Uint8Array, bufferContext: "stdout" | "stderr") {
        const decoder = bufferContext === "stdout" ? this.stdoutDecoder : this.stderrDecoder;
        let currentBuffer = bufferContext === "stdout" ? this.stdoutBuffer : this.stderrBuffer;

        const chunk = decoder.decode(data, { stream: true });
        currentBuffer += chunk;

        const lastEscIndex = currentBuffer.lastIndexOf("\x1B");
        let toProcess = "";
        let remaining = "";

        if (lastEscIndex !== -1 && currentBuffer.length - lastEscIndex < 10) {
            toProcess = currentBuffer.substring(0, lastEscIndex);
            remaining = currentBuffer.substring(lastEscIndex);
        } else {
            toProcess = currentBuffer;
            remaining = "";
        }

        if (toProcess) {
            const cleanText = this.stripAnsi(toProcess);
            const lines = cleanText.split(/\r?\n/);
            const endsWithNewline = cleanText.endsWith("\n") || cleanText.endsWith("\r");

            const processCount = lines.length - (endsWithNewline ? 0 : 1);

            for (let i = 0; i < processCount; i++) {
                const line = lines[i].trim();
                if (line) {
                    this.onLog(line);
                    this.checkAndAutoReply(line);
                }
            }

            if (!endsWithNewline) {
                const lastLine = lines[lines.length - 1];
                remaining = lastLine + remaining;

                const incompleteClean = this.stripAnsi(remaining).trim();
                if (incompleteClean) {
                    const lowerLast = incompleteClean.toLowerCase();
                    if (
                        lowerLast.includes("press any key") ||
                        lowerLast.includes('press "enter" to quit') ||
                        lowerLast.includes("done")
                    ) {
                        this.onLog(incompleteClean);
                        this.sendInput("\n");
                        remaining = "";
                    }
                }
            }
        }

        if (bufferContext === "stdout") {
            this.stdoutBuffer = remaining;
        } else {
            this.stderrBuffer = remaining;
        }
    }

    public async execute(
        filePath: string,
        type: "python" | "exec",
        cwd: string,
        signal?: AbortSignal,
    ): Promise<void> {
        this.stdoutBuffer = "";
        this.stderrBuffer = "";

        return new Promise<void>((resolve, reject) => {
            if (signal?.aborted) {
                return reject(new Error("Aborted"));
            }

            const env: NodeJS.ProcessEnv = {
                ...process.env,
                PYTHONIOENCODING: "utf-8",
                PYTHONUTF8: "1",
                PYTHONLEGACYWINDOWSSTDIO: "1",
            };

            let child: ChildProcess;

            try {
                if (process.platform === "win32") {
                    // on windows, use chcp 65001 to force utf-8 console output
                    const cmd =
                        type === "python"
                            ? `chcp 65001 > nul && python -u "${filePath}"`
                            : `chcp 65001 > nul && "${filePath}"`;

                    child = spawn(cmd, [], {
                        windowsHide: true,
                        cwd,
                        env,
                        shell: true,
                    });
                } else {
                    if (type === "python") {
                        child = spawn("python", ["-u", filePath], { windowsHide: false, cwd, env });
                    } else {
                        child = spawn(filePath, [], { windowsHide: false, cwd, env });
                    }
                }
            } catch (err) {
                return reject(err);
            }

            this.currentProcess = child;

            if (signal) {
                signal.addEventListener("abort", () => {
                    if (!this.currentProcess) return;

                    try {
                        if (process.platform === "win32" && child.pid) {
                            spawn("taskkill", ["/pid", child.pid.toString(), "/f", "/t"]);
                        } else {
                            child.kill();
                        }
                    } catch {}
                    reject(new Error("Aborted"));
                });
            }

            child.stdout?.on("data", (data) => this.handleStreamData(data, "stdout"));
            child.stderr?.on("data", (data) => this.handleStreamData(data, "stderr"));

            child.on("close", (code) => {
                const stdoutFinal = this.stdoutDecoder.decode();
                const stderrFinal = this.stderrDecoder.decode();

                const finalProcess = (buf: string, rest: string) => {
                    const combined = this.stripAnsi(buf + rest).trim();
                    if (combined) {
                        for (const line of combined.split(/\r?\n/)) {
                            if (line.trim()) this.onLog(line.trim());
                        }
                    }
                };

                finalProcess(this.stdoutBuffer, stdoutFinal);
                finalProcess(this.stderrBuffer, stderrFinal);

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
