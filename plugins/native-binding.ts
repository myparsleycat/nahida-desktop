import fs from "node:fs";
import path from "node:path";
import { Plugin } from "vite";

function getNativeIndexFiles(root: string): string[] {
    const nativeDir = path.resolve(root, "native");
    if (!fs.existsSync(nativeDir)) return [];

    return fs
        .readdirSync(nativeDir, { withFileTypes: true })
        .filter((dirent) => dirent.isDirectory())
        .map((dirent) => path.join(nativeDir, dirent.name, "index.js"))
        .filter((filePath) => fs.existsSync(filePath));
}

function patchNativeBinding(filePath: string): void {
    if (!fs.existsSync(filePath)) return;

    try {
        let content = fs.readFileSync(filePath, "utf-8");

        if (content.includes("const binding = nativeBinding?.default ?? nativeBinding")) {
            return;
        }

        let changed = false;

        const mainExportRegex = /module\.exports\s*=\s*nativeBinding(?:\.default)?(?=[;\n\r]|$)/;

        if (mainExportRegex.test(content)) {
            const patchBlock = `const binding = nativeBinding?.default ?? nativeBinding
if (!binding) {
  throw new Error('Loaded native binding has no exports')
}
module.exports = binding`;

            content = content.replace(mainExportRegex, patchBlock);

            content = content.replace(
                /module\.exports\.(\w+)\s*=\s*nativeBinding(?:\.default)?\.(\w+)/g,
                "module.exports.$1 = binding.$2",
            );

            content = content.replace(
                /module\.exports\.(\w+)\s*=\s*\(nativeBinding\.default\s*\?\?\s*nativeBinding\)(?:\.default)?\.(\w+)/g,
                "module.exports.$1 = binding.$2",
            );

            changed = true;
        }

        if (changed) {
            fs.writeFileSync(filePath, content, "utf-8");
            console.log(
                `[NativeBindingPlugin] Successfully patched: ${path.relative(process.cwd(), filePath)}`,
            );
        }
    } catch (error) {
        console.error(`[NativeBindingPlugin] Failed to patch ${filePath}:`, error);
    }
}

export function nativeBindingPlugin(): Plugin {
    return {
        name: "vite-plugin-native-binding",
        buildStart() {
            const root = process.cwd();
            const files = getNativeIndexFiles(root);
            for (const file of files) {
                patchNativeBinding(file);
            }
        },
        configureServer(server) {
            const root = process.cwd();
            const nativeDir = path.resolve(root, "native");

            server.watcher.add(nativeDir);

            const handleFile = (file: string): void => {
                const relative = path.relative(nativeDir, file);
                const parts = relative.split(path.sep);
                if (parts.length === 2 && parts[1] === "index.js") {
                    setTimeout(() => patchNativeBinding(file), 100);
                }
            };

            server.watcher.on("change", handleFile);
            server.watcher.on("add", handleFile);
        },
    };
}
