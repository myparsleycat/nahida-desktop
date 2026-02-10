import i18n, { type InitOptions } from "i18next";
import { initReactI18next } from "react-i18next";
import enTranslation from "./locales/en.json";
import jaTranslation from "./locales/ja.json";
import koTranslation from "./locales/ko.json";
import zhTranslation from "./locales/zh.json";

const resources = {
    en: {
        translation: enTranslation,
    },
    ko: {
        translation: koTranslation,
    },
    zh: {
        translation: zhTranslation,
    },
    ja: {
        translation: jaTranslation,
    },
};

i18n.use(initReactI18next).init({
    debug: false,
    fallbackLng: "en",
    interpolation: {
        escapeValue: false,
    },
    resources,
    defaultNS: "translation",
    react: {
        useSuspense: true,
    },
} as InitOptions);

export default i18n;
