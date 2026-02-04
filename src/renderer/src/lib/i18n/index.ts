import i18n, { InitOptions } from "i18next";
import { initReactI18next } from "react-i18next";
import enTranslation from "./locales/en.json";
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
};

i18n.use(initReactI18next).init({
    debug: true,
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
