import { josa } from "es-hangul";
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

void i18n.use(initReactI18next).init({
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

i18n.services.formatter?.add("josa", (value, _lng, options) => {
    const pair = options?.pair;
    if (!pair || typeof value !== "string") return value as string;
    return josa(value, pair as Parameters<typeof josa>[1]);
});

export default i18n;
