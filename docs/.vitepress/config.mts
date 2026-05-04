import { defineConfig } from "vitepress";

// https://vitepress.dev/reference/site-config
export default defineConfig({
    title: "Nahida Desktop Docs",
    description: "Nahida Desktop",
    themeConfig: {
        // https://vitepress.dev/reference/default-theme-config
        nav: [
            { text: "Home", link: "/" },
            { text: "Guide", link: "/guide" },
        ],

        sidebar: [
            {
                text: "Examples",
                items: [
                    { text: "Markdown Examples", link: "/markdown-examples" },
                    { text: "Runtime API Examples", link: "/api-examples" },
                ],
            },
        ],

        socialLinks: [{ icon: "github", link: "https://github.com/myparlseycat/nahida-desktop" }],
    },

    locales: {
        root: {
            label: "English",
            lang: "en",
            title: "Nahida Desktop",
            description: "A Mod Manager",
        },
        ko: {
            label: "한국어",
            lang: "ko",
            link: "/ko/",
            title: "Nahida Desktop",
            description: "모드 매니저",
            themeConfig: {
                nav: [
                    { text: "홈", link: "/ko/" },
                    { text: "가이드", link: "/ko/guide/what-is-nahida-desktop" },
                ],
                sidebar: [
                    {
                        text: "소개",
                        items: [
                            {
                                text: "나히다 데스크탑에 대해",
                                link: "/ko/guide/what-is-nahida-desktop",
                            },
                            { text: "시작하기", link: "/ko/guide/getting-started" },
                        ],
                    },
                    {
                        text: "상세 기능",
                        link: "/ko/features/index",
                        items: [
                            {
                                text: "게임바나나",
                                link: "/ko/features/gamebanana",
                            },
                        ],
                    },
                ],
            },
        },
    },
});
