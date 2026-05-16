import { type ChildProcess, spawn } from "node:child_process";
import type { FixToolLogEvent } from "@shared/types";

export class ScriptExecutor {
    private currentProcess: ChildProcess | null = null;
    private stdoutBuffer = "";
    private stderrBuffer = "";
    private stdoutHasPartialLog = false;
    private stderrHasPartialLog = false;
    private readonly stdoutDecoder = new TextDecoder("utf-8");
    private readonly stderrDecoder = new TextDecoder("utf-8");

    constructor(private onLog: (msg: string, event?: Omit<FixToolLogEvent, "message">) => void) {}

    private stripAnsi(str: string): string {
        // oxlint-disable-next-line no-control-regex
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

    private emitLog(
        message: string,
        bufferContext: "stdout" | "stderr",
        replaceLast: boolean = false,
    ) {
        const trimmed = message.trim();
        if (!trimmed) {
            return;
        }

        this.onLog(trimmed, replaceLast ? { replaceLast: true } : undefined);
        if (bufferContext === "stdout") {
            this.stdoutHasPartialLog = replaceLast;
        } else {
            this.stderrHasPartialLog = replaceLast;
        }
        this.checkAndAutoReply(trimmed);
    }

    private handleStreamData(data: Uint8Array, bufferContext: "stdout" | "stderr") {
        const decoder = bufferContext === "stdout" ? this.stdoutDecoder : this.stderrDecoder;
        let currentBuffer = bufferContext === "stdout" ? this.stdoutBuffer : this.stderrBuffer;
        let hasPartialLog =
            bufferContext === "stdout" ? this.stdoutHasPartialLog : this.stderrHasPartialLog;

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
                const line = lines[i];
                if (line.trim()) {
                    this.emitLog(line, bufferContext, hasPartialLog);
                }
                hasPartialLog = false;
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
                        this.emitLog(incompleteClean, bufferContext, hasPartialLog);
                        this.sendInput("\n");
                        remaining = "";
                        hasPartialLog = false;
                    } else {
                        this.emitLog(incompleteClean, bufferContext, hasPartialLog);
                        hasPartialLog = true;
                    }
                }
            }
        }

        if (bufferContext === "stdout") {
            this.stdoutBuffer = remaining;
            this.stdoutHasPartialLog = hasPartialLog;
        } else {
            this.stderrBuffer = remaining;
            this.stderrHasPartialLog = hasPartialLog;
        }
    }

    private quoteWindowsArg(value: string) {
        return `"${value.replace(/"/g, '""')}"`;
    }

    private buildLegacyWindowsCommand(filePath: string, type: "python" | "exec", args: string[]) {
        const quotedArgs = args.map((arg) => this.quoteWindowsArg(arg)).join(" ");

        if (type === "python") {
            const parts = [`python -u ${this.quoteWindowsArg(filePath)}`];
            if (quotedArgs) {
                parts.push(quotedArgs);
            }
            return `chcp 65001 > nul && ${parts.join(" ")}`;
        }

        const parts = [this.quoteWindowsArg(filePath)];
        if (quotedArgs) {
            parts.push(quotedArgs);
        }
        return `chcp 65001 > nul && ${parts.join(" ")}`;
    }

    public async execute(
        filePath: string,
        type: "python" | "exec",
        cwd: string,
        args: string[] = [],
        windowsExecutionMode: "legacy-shell" | "direct" = "legacy-shell",
        signal?: AbortSignal,
    ): Promise<void> {
        this.stdoutBuffer = "";
        this.stderrBuffer = "";
        this.stdoutHasPartialLog = false;
        this.stderrHasPartialLog = false;

        return new Promise<void>((resolve, reject) => {
            let settled = false;

            if (signal?.aborted) {
                settled = true;
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
                if (windowsExecutionMode === "direct") {
                    if (type === "python") {
                        const command = `python -u ${[filePath, ...args]
                            .map((arg) => this.quoteWindowsArg(arg))
                            .join(" ")}`;

                        child = spawn(
                            "cmd.exe",
                            ["/d", "/s", "/c", `chcp 65001 > nul && ${command}`],
                            {
                                windowsHide: true,
                                cwd,
                                env,
                                shell: false,
                            },
                        );
                    } else {
                        child = spawn(filePath, args, {
                            windowsHide: true,
                            cwd,
                            env,
                            shell: false,
                        });
                    }
                } else {
                    const command = this.buildLegacyWindowsCommand(filePath, type, args);
                    child = spawn(command, [], {
                        windowsHide: true,
                        cwd,
                        env,
                        shell: true,
                    });
                }
            } catch (err) {
                settled = true;
                return reject(err);
            }

            this.currentProcess = child;

            const abortHandler = () => {
                if (settled || !this.currentProcess) return;
                settled = true;

                try {
                    if (child.pid) {
                        spawn("taskkill", ["/pid", child.pid.toString(), "/f", "/t"]);
                    }
                } catch {}

                this.currentProcess = null;
                reject(new Error("Aborted"));
            };

            if (signal) {
                signal.addEventListener("abort", abortHandler);
            }

            child.stdout?.on("data", (data) => this.handleStreamData(data, "stdout"));
            child.stderr?.on("data", (data) => this.handleStreamData(data, "stderr"));

            const cleanup = () => {
                if (signal) {
                    signal.removeEventListener("abort", abortHandler);
                }
                this.currentProcess = null;
            };

            child.on("close", (code) => {
                if (settled) return;
                settled = true;

                const stdoutFinal = this.stdoutDecoder.decode();
                const stderrFinal = this.stderrDecoder.decode();

                const finalProcess = (
                    buf: string,
                    rest: string,
                    bufferContext: "stdout" | "stderr",
                    replaceLast: boolean,
                ) => {
                    const combined = this.stripAnsi(buf + rest).trim();
                    if (combined) {
                        for (const line of combined.split(/\r?\n/)) {
                            if (line.trim()) {
                                this.emitLog(line, bufferContext, replaceLast);
                                replaceLast = false;
                            }
                        }
                    }
                };

                finalProcess(this.stdoutBuffer, stdoutFinal, "stdout", this.stdoutHasPartialLog);
                finalProcess(this.stderrBuffer, stderrFinal, "stderr", this.stderrHasPartialLog);

                cleanup();

                if (code === 0) {
                    resolve();
                } else {
                    reject(new Error(`Process exited with code ${code}`));
                }
            });

            child.on("error", (err) => {
                if (settled) return;
                settled = true;

                cleanup();
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
