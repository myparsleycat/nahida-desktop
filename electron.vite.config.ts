import { defineConfig, externalizeDepsPlugin } from 'electron-vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import path from 'path'
// import process from 'process'

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()]
  },
  preload: {
    plugins: [externalizeDepsPlugin()]
  },
  renderer: {
    plugins: [
      svelte()
    ],
    resolve: {
      alias: {
        $lib: path.resolve("./src/renderer/src/lib"),
        '@': path.resolve('./src/renderer/src'),
      }
    },
    root: path.resolve(process.cwd(), 'src/renderer'),
    base: './',
  }
})
