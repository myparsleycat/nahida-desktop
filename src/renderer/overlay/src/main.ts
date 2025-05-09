import { mount } from 'svelte';
import "$lib/i18n";
import { locale, waitLocale } from "svelte-i18n";
import '../../global.css';
import App from './App.svelte'

const browserLang = window.navigator.language.split("-")[0];
const userLang = browserLang;
locale.set(userLang);
waitLocale();

const app = mount(App, {
  target: document.getElementById('overlay')!
})

export default app
