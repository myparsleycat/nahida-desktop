import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { resolve, join } from "node:path";
import type { Plugin } from "vite";

interface IpcGeneratorOptions {
    handlerDir: string;
    typesFile: string;
    sharedTypesFile: string;
    runtimeFile: string;
}

function getEol(content: string) {
    return content.includes("\r\n") ? "\r\n" : "\n";
}

function extractTypeSection(content: string, typeName: string) {
    const marker = `export type ${typeName} = {`;
    const start = content.indexOf(marker);
    if (start === -1) {
        return null;
    }

    const bodyStart = start + marker.length;
    const end = content.indexOf("\n};", bodyStart);
    if (end === -1) {
        return null;
    }

    return content.slice(bodyStart, end);
}

function extractTypeKeys(content: string, typeName: string) {
    const section = extractTypeSection(content, typeName);
    if (!section) {
        return [];
    }

    const keys = new Set<string>();
    const lines = section.split(/\r?\n/);
    let depth = 0;

    for (const line of lines) {
        if (depth === 0) {
            const match = line.match(/^\s*(?:"([^"]+)"|'([^']+)'|([A-Za-z0-9_$]+))\s*:/);
            const key = match?.[1] ?? match?.[2] ?? match?.[3];
            if (key) {
                keys.add(key);
            }
        }

        for (const char of line) {
            if (char === "{") {
                depth += 1;
            } else if (char === "}") {
                depth = Math.max(0, depth - 1);
            }
        }
    }

    return Array.from(keys).sort();
}

function toConstLiteral(values: string[], eol: string) {
    return `[${eol}${values.map((value) => `    ${JSON.stringify(value)}`).join(`,${eol}`)}${eol}] as const;`;
}

function generateIpc(options: IpcGeneratorOptions) {
    const { handlerDir, typesFile, sharedTypesFile, runtimeFile } = options;
    const channels = new Map<string, string>(); // channel -> type definition
    const sendChannels = new Set<string>();

    let currentTypesContent = "";
    try {
        currentTypesContent = readFileSync(typesFile, "utf-8");
    } catch {
        return;
    }

    const typesSectionMatch = currentTypesContent.match(
        /\/\/ IPC_HANDLERS_START(.*?)\/\/ IPC_HANDLERS_END/s,
    );
    const typeMap = new Map<string, string>();
    if (typesSectionMatch) {
        const section = typesSectionMatch[1];
        const parts = section.split(";");

        for (const p of parts) {
            const firstColon = p.indexOf(":");
            if (firstColon !== -1) {
                const key = p.substring(0, firstColon).trim().replace(/['"]/g, "");
                const type = p.substring(firstColon + 1).trim();
                if (key && type) typeMap.set(key, type);
            }
        }
    }

    const files = readdirSync(handlerDir);
    for (const file of files) {
        if (!file.endsWith(".ts")) continue;
        const content = readFileSync(join(handlerDir, file), "utf-8");

        const importMap = new Map<string, string>();
        const importRegex = /import\s+(?:type\s+)?\{([^}]+)\}\s+from\s+["']([^"']+)["']/g;
        let importMatch;
        while ((importMatch = importRegex.exec(content)) !== null) {
            const imports = importMatch[1].split(",");
            const source = importMatch[2];
            for (const imp of imports) {
                const parts = imp.trim().split(/\s+as\s+/);
                const importedName = parts[0].trim();
                const localName = parts[1]?.trim() || importedName;
                if (localName) {
                    importMap.set(localName, `import("${source}").${importedName}`);
                }
            }
        }

        const rhRegex =
            /(?:rh|ipcMain\.handle)\s*\(\s*["']([^"']+)["']\s*,\s*(.*?)(?:\)\s*;|\)\s*\n)/gs;
        let match;
        while ((match = rhRegex.exec(content)) !== null) {
            const channel = match[1];
            const impl = match[2];

            if (typeMap.has(channel)) {
                channels.set(channel, typeMap.get(channel)!);
            } else {
                // infer type from implementation
                // direct function reference: rh("channel", myFunc)
                const simpleIdentifierMatch = impl.match(/^[a-zA-Z0-9_$]+$/);
                if (simpleIdentifierMatch && importMap.has(simpleIdentifierMatch[0])) {
                    const importRef = importMap.get(simpleIdentifierMatch[0]);
                    channels.set(
                        channel,
                        `(...args: Parameters<typeof ${importRef}>) => ReturnType<typeof ${importRef}>`,
                    );
                    continue;
                }

                // function call in arrow/return: => d.foo.bar(...) or return d.foo.bar(...)
                const returnMatch = impl.match(
                    /(?:return|=>)\s*(?:await\s+)?(?:desktop|d)\.([a-zA-Z0-9_$.]+)\(/,
                );

                if (returnMatch) {
                    const methodPath = returnMatch[1];
                    channels.set(
                        channel,
                        `(...args: Parameters<typeof desktop.${methodPath}>) => ReturnType<typeof desktop.${methodPath}>`,
                    );
                } else {
                    //simple imported function call: => myFunc(...)
                    const bareReturnMatch = impl.match(
                        /(?:return|=>)\s*(?:await\s+)?([a-zA-Z0-9_$]+)\(/,
                    );
                    if (bareReturnMatch && importMap.has(bareReturnMatch[1])) {
                        const importRef = importMap.get(bareReturnMatch[1]);
                        channels.set(
                            channel,
                            `(...args: Parameters<typeof ${importRef}>) => ReturnType<typeof ${importRef}>`,
                        );
                    } else {
                        channels.set(channel, `(...args: any[]) => any`);
                    }
                }
            }
        }

        const onRegex = /ipcMain\.on\s*\(\s*["']([^"']+)["']/g;
        let onMatch;
        while ((onMatch = onRegex.exec(content)) !== null) {
            sendChannels.add(onMatch[1]);
        }
    }

    const sortedChannelNames = Array.from(channels.keys()).sort();
    const sortedSendChannelNames = Array.from(sendChannels).sort();
    const sharedTypesContent = readFileSync(sharedTypesFile, "utf-8");
    const eventChannelNames = extractTypeKeys(sharedTypesContent, "IpcEvents");

    // update types.ts
    const typesContent = readFileSync(typesFile, "utf-8");
    const typesEol = getEol(typesContent);
    const typesReplacement = sortedChannelNames
        .map((c) => {
            const type = channels.get(c);
            const quote = c.includes(":") ? '"' : "";
            return `    ${quote}${c}${quote}: ${type};`;
        })
        .join(typesEol);
    const newTypesContent = typesContent.replace(
        /(\/\/ IPC_HANDLERS_START).*?(\/\/ IPC_HANDLERS_END)/s,
        `$1${typesEol}${typesReplacement}${typesEol}    $2`,
    );

    if (newTypesContent !== typesContent) {
        writeFileSync(typesFile, newTypesContent);
        console.log(`[IPC Gen] Updated ${typesFile}`);
    }

    const runtimeEol = getEol(currentTypesContent);
    const runtimeContent = [
        "// This file is automatically generated by ipcGeneratorPlugin; do not edit it.",
        "",
        `export const IPC_HANDLER_CHANNELS = ${toConstLiteral(sortedChannelNames, runtimeEol)}`,
        "",
        `export const IPC_SEND_CHANNELS = ${toConstLiteral(sortedSendChannelNames, runtimeEol)}`,
        "",
        `export const IPC_EVENT_CHANNELS = ${toConstLiteral(eventChannelNames, runtimeEol)}`,
        "",
        "export type IpcHandlerChannel = (typeof IPC_HANDLER_CHANNELS)[number];",
        "export type IpcSendChannel = (typeof IPC_SEND_CHANNELS)[number];",
        "export type IpcEventChannel = (typeof IPC_EVENT_CHANNELS)[number];",
        "",
    ].join(runtimeEol);

    let currentRuntimeContent = "";
    try {
        currentRuntimeContent = readFileSync(runtimeFile, "utf-8");
    } catch {
        // ignore missing file
    }

    if (runtimeContent !== currentRuntimeContent) {
        writeFileSync(runtimeFile, runtimeContent);
        console.log(`[IPC Gen] Updated ${runtimeFile}`);
    }
}

export const ipcGeneratorPlugin = (): Plugin => {
    const options = {
        handlerDir: resolve("src/main/ipc/handlers"),
        typesFile: resolve("src/shared/types.gen.ts"),
        sharedTypesFile: resolve("src/shared/types.ts"),
        runtimeFile: resolve("src/shared/ipc-keys.gen.ts"),
    };

    return {
        name: "ipc-generator",
        buildStart() {
            generateIpc(options);
        },
        configureServer(server) {
            server.watcher.on("change", (file) => {
                if (
                    file.includes(join("src", "main", "ipc", "handlers")) ||
                    file === options.typesFile ||
                    file === options.sharedTypesFile
                ) {
                    generateIpc(options);
                }
            });
        },
    };
};
