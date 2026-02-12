import { resolve, join } from "node:path";
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import type { Plugin } from "vite";

interface IpcGeneratorOptions {
    handlerDir: string;
    typesFile: string;
}

function generateIpc(options: IpcGeneratorOptions) {
    const { handlerDir, typesFile } = options;
    const channels = new Map<string, string>(); // channel -> type definition

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
    }

    const sortedChannelNames = Array.from(channels.keys()).sort();

    // update types.ts
    const typesContent = readFileSync(typesFile, "utf-8");
    const typesEol = typesContent.includes("\r\n") ? "\r\n" : "\n";
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
}

export const ipcGeneratorPlugin = (): Plugin => {
    const options = {
        handlerDir: resolve("src/main/ipc/handlers"),
        typesFile: resolve("src/shared/types.gen.ts"),
    };

    return {
        name: "ipc-generator",
        buildStart() {
            generateIpc(options);
        },
        configureServer(server) {
            server.watcher.on("change", (file) => {
                if (file.includes(join("src", "main", "ipc", "handlers"))) {
                    generateIpc(options);
                }
            });
        },
    };
};
