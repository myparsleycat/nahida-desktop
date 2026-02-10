import { type ChildProcess, spawn } from "node:child_process";
import { EventEmitter } from "node:events";
import * as readline from "node:readline";

export interface GoProcessOptions {
    path: string;
    args: string[];
    cwd?: string;
    env?: NodeJS.ProcessEnv;
}

export type IpcMessage<T = any> = {
    type: string;
    payload: T;
};

export class GoProcess extends EventEmitter {
    private process: ChildProcess | null = null;
    private options: GoProcessOptions;

    constructor(options: GoProcessOptions) {
        super();
        this.options = options;
    }

    public start(): Promise<any> {
        return new Promise((resolve, reject) => {
            try {
                this.process = spawn(this.options.path, this.options.args, {
                    cwd: this.options.cwd,
                    env: this.options.env,
                    stdio: ["pipe", "pipe", "pipe"],
                });

                if (!this.process.stdout || !this.process.stderr) {
                    throw new Error("Failed to spawn process with pipes");
                }

                const rl = readline.createInterface({
                    input: this.process.stdout,
                    terminal: false,
                });

                rl.on("line", (line) => {
                    this.handleLine(line, resolve, reject);
                });

                this.process.stderr.on("data", (data) => {
                    const msg = data.toString();
                    this.emit("stderr", msg);
                });

                this.process.on("error", (err) => {
                    reject(err);
                });

                this.process.on("close", (code) => {
                    if (code !== 0) {
                    }
                    this.emit("exit", code);
                });
            } catch (error) {
                reject(error);
            }
        });
    }

    private handleLine(line: string, resolve: (val: any) => void, reject: (err: Error) => void) {
        try {
            if (!line || !line.trim()) return;

            const msg = JSON.parse(line) as IpcMessage;

            switch (msg.type) {
                case "progress":
                    this.emit("progress", msg.payload);
                    break;
                case "success":
                    this.emit("success", msg.payload);
                    resolve(msg.payload);
                    break;
                case "error": {
                    const errPayload = msg.payload as { code: string; message: string };
                    const error = new Error(errPayload.message);
                    (error as any).code = errPayload.code;
                    this.emit("error", error);
                    reject(error);
                    break;
                }
                case "log":
                    this.emit("log", msg.payload);
                    if (
                        typeof msg.payload === "string" &&
                        msg.payload.toLowerCase().includes("process ready")
                    ) {
                        resolve(msg.payload);
                    }
                    break;
                default:
                    this.emit(msg.type, msg.payload);
                    break;
            }
        } catch (e) {
            this.emit("raw-log", line);
        }
    }

    public kill() {
        if (this.process) {
            this.process.kill();
        }
    }

    public write(data: string) {
        if (this.process && this.process.stdin) {
            this.process.stdin.write(data);
        }
    }
}
