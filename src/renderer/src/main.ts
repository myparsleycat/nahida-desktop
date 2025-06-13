import { mount } from 'svelte'
import '../app.css';
import "$lib/i18n";
import { locale, waitLocale } from "svelte-i18n";
import { setupWindowControls } from './window-controls'
import { SettingHelper } from '$lib/helpers/setting.helper';

import App from './App.svelte'

async function startApp() {
  try {
    const userLang = await SettingHelper.general.lang.get();
    locale.set(userLang);
  } catch (e: any) {
    console.error('Failed to get user language preference:', e);
    const browserLang = window.navigator.language.split("-")[0];
    locale.set(browserLang);
  }

  await waitLocale();
  setupWindowControls();

  const app = mount(App, {
    target: document.getElementById('app')!
  });

  return app;
}

const app = await startApp();

export default app;