import { mount } from 'svelte'
import './app.css';
import "$lib/i18n";
import { locale, waitLocale } from "svelte-i18n";
import { setupWindowControls } from './window-controls'

import App from './App.svelte'

const browserLang = window.navigator.language.split("-")[0];
const userLang = browserLang;
locale.set(userLang);
waitLocale();

const app = mount(App, {
  target: document.getElementById('app')!
})

let windowControlsSetup = false;
document.addEventListener('DOMContentLoaded', () => {
  if (!windowControlsSetup) {
    setupWindowControls();
    windowControlsSetup = true;
  }
})

export default app
