// src/lib/i18n/index.ts
import { init, register } from 'svelte-i18n'

const defaultLocale = 'en'

register('en', () => import('@shared/locales/en.json'))
register('ko', () => import('@shared/locales/ko.json'))
register('zh', () => import('@shared/locales/zh.json'))

init({
  fallbackLocale: defaultLocale,
  initialLocale: window.navigator.language.split('-')[0] || defaultLocale,
})