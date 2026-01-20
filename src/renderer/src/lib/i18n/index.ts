import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";
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

i18n.use(LanguageDetector)
    .use(initReactI18next)
    .init({
        resources,
        fallbackLng: "en",
        detection: {
            order: ["navigator"],
            caches: [],
        },
        interpolation: {
            escapeValue: false,
        },
    });

export default i18n;
