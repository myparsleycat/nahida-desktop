import { defineConfig, externalizeDepsPlugin } from 'electron-vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import path from 'path'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
    resolve: {
      alias: {
        '@shared': path.resolve('./src/shared'),
        '@core': path.resolve('./src/core'),
        '@main': path.resolve('./src/main')
      }
    }
  },
  preload: {
    build: {
      rollupOptions: {
        external: ['electron'],
        output: {
          format: 'es'
        }
      }
    },
    plugins: [externalizeDepsPlugin()],
    resolve: {
      alias: {
        '@shared': path.resolve('./src/shared'),
        '@core': path.resolve('./src/core'),
        '@main': path.resolve('./src/main')
      }
    }
  },
  renderer: {
    plugins: [
      svelte(),
      tailwindcss()
    ],
    resolve: {
      alias: {
        $lib: path.resolve("./src/renderer/src/lib"),
        '@': path.resolve('./src/renderer/src'),
        '@shared': path.resolve('./src/shared'),
      }
    },
    root: path.resolve(process.cwd(), 'src/renderer'),
    base: './',
  }
})