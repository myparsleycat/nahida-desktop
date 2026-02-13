import { Plugin } from 'vite'
import fs from 'node:fs'
import path from 'node:path'

function getNativeIndexFiles(root: string): string[] {
  const nativeDir = path.resolve(root, 'native')
  if (!fs.existsSync(nativeDir)) return []

  return fs.readdirSync(nativeDir, { withFileTypes: true })
    .filter((dirent) => dirent.isDirectory())
    .map((dirent) => path.join(nativeDir, dirent.name, 'index.js'))
    .filter((filePath) => fs.existsSync(filePath))
}

function patchNativeBinding(filePath: string): void {
  if (!fs.existsSync(filePath)) return

  try {
    let content = fs.readFileSync(filePath, 'utf-8')
    let changed = false

    const mainExportRegex = /(module\.exports\s*=\s*nativeBinding)(?!\.default)(?=[;\n\r]|$)/g
    if (mainExportRegex.test(content)) {
      content = content.replace(mainExportRegex, '$1.default')
      changed = true
    }

    const propExportRegex = /(= \s*nativeBinding)\.(?!default)/g
    if (propExportRegex.test(content)) {
      content = content.replace(propExportRegex, '$1.default.')
      changed = true
    }

    if (changed) {
      fs.writeFileSync(filePath, content, 'utf-8')
      console.log(`[NativeBindingPlugin] Patched ${path.relative(process.cwd(), filePath)}`)
    }
  } catch (error) {
    console.error(`[NativeBindingPlugin] Failed to patch ${filePath}:`, error)
  }
}

export function nativeBindingPlugin(): Plugin {
  return {
    name: 'vite-plugin-native-binding',
    buildStart() {
      const root = process.cwd()
      const files = getNativeIndexFiles(root)
      for (const file of files) {
        patchNativeBinding(file)
      }
    },
    configureServer(server) {
      const root = process.cwd()
      const nativeDir = path.resolve(root, 'native')

      server.watcher.add(nativeDir)

      const handleFile = (file: string): void => {
        const relative = path.relative(nativeDir, file)
        
        const parts = relative.split(path.sep)
        if (parts.length === 2 && parts[1] === 'index.js') {
           setTimeout(() => patchNativeBinding(file), 100)
        }
      }

      server.watcher.on('change', handleFile)
      server.watcher.on('add', handleFile)
    }
  }
}
