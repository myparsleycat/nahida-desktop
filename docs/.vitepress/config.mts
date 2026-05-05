import { defineConfig } from "vitepress";

const docsBasePath = process.env.DOCS_BASE_PATH || "/";

// https://vitepress.dev/reference/site-config
export default defineConfig({
    base: docsBasePath,
    title: "Nahida Desktop Docs",
    description: "Nahida Desktop",
    themeConfig: {
        nav: [
            { text: "Home", link: "/" },
            { text: "Guide", link: "/guide/what-is-nahida-desktop" },
            { text: "Features", link: "/features/index" },
        ],
        sidebar: [
            {
                text: "Introduction",
                items: [
                    { text: "What is Nahida Desktop?", link: "/guide/what-is-nahida-desktop" },
                    { text: "Getting Started", link: "/guide/getting-started" },
                ],
            },
            {
                text: "Features",
                items: [
                    { text: "Overview", link: "/features/index" },
                    { text: "GameBanana", link: "/features/gamebanana" },
                    { text: "Mod Manager", link: "/features/mod-manager" },
                    {
                        text: "Mod Tools",
                        items: [
                            {
                                text: "d3d11.dll Builder",
                                link: "/features/mod-tools/dll-builder",
                            },
                            {
                                text: "Persist Toggles",
                                link: "/features/mod-tools/persist-toggles",
                            },
                            {
                                text: "Static GLB Converter / Model Viewer",
                                link: "/features/mod-tools/static-glb-converter",
                            },
                        ],
                    },
                ],
            },
            {
                text: "Other",
                items: [{ text: "Set Up XXMI", link: "/others/set-up-xxmi" }],
            },
        ],
        socialLinks: [{ icon: "github", link: "https://github.com/myparsleycat/nahida-desktop" }],
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
                        base: "/ko/guide",
                        items: [
                            {
                                text: "나히다 데스크탑에 대해",
                                link: "/what-is-nahida-desktop",
                            },
                            { text: "시작하기", link: "/getting-started" },
                        ],
                    },
                    {
                        text: "상세 기능",
                        link: "/ko/features/index",
                        base: "/ko/features",
                        items: [
                            {
                                text: "게임바나나",
                                link: "/gamebanana",
                            },
                            {
                                text: "모드 매니저",
                                link: "/mod-manager",
                            },
                            {
                                text: "모드 툴",
                                base: "/ko/features/mod-tools",
                                collapsed: true,
                                items: [
                                    {
                                        text: "d3d11.dll 빌더",
                                        link: "/dll-builder",
                                    },
                                    {
                                        text: "토글 영구 저장",
                                        link: "/persist-toggles",
                                    },
                                    {
                                        text: "정적 GLB 변환기",
                                        link: "/static-glb-converter",
                                    },
                                ],
                            },
                        ],
                    },
                    {
                        text: "기타",
                        base: "/ko/others",
                        items: [
                            {
                                text: "XXMI 연결하기",
                                link: "/set-up-xxmi",
                            },
                        ],
                    },
                ],
            },
        },
        ja: {
            label: "日本語",
            lang: "ja",
            link: "/ja/",
            title: "Nahida Desktop",
            description: "MODマネージャー",
            themeConfig: {
                nav: [
                    { text: "ホーム", link: "/ja/" },
                    { text: "ガイド", link: "/ja/guide/what-is-nahida-desktop" },
                    { text: "機能", link: "/ja/features/index" },
                ],
                sidebar: [
                    {
                        text: "紹介",
                        base: "/ja/guide",
                        items: [
                            {
                                text: "Nahida Desktop とは",
                                link: "/what-is-nahida-desktop",
                            },
                            { text: "はじめに", link: "/getting-started" },
                        ],
                    },
                    {
                        text: "機能",
                        link: "/ja/features/index",
                        base: "/ja/features",
                        items: [
                            { text: "GameBanana", link: "/gamebanana" },
                            { text: "MODマネージャー", link: "/mod-manager" },
                            {
                                text: "MOD ツール",
                                base: "/ja/features/mod-tools",
                                collapsed: true,
                                items: [
                                    {
                                        text: "d3d11.dll ビルダー",
                                        link: "/dll-builder",
                                    },
                                    {
                                        text: "トグル状態の永続保存",
                                        link: "/persist-toggles",
                                    },
                                    {
                                        text: "静的 GLB 変換 / モデルビューア",
                                        link: "/static-glb-converter",
                                    },
                                ],
                            },
                        ],
                    },
                    {
                        text: "その他",
                        base: "/ja/others",
                        items: [
                            {
                                text: "XXMI の設定",
                                link: "/set-up-xxmi",
                            },
                        ],
                    },
                ],
            },
        },
        "zh-CN": {
            label: "简体中文",
            lang: "zh-CN",
            link: "/zh-CN/",
            title: "Nahida Desktop",
            description: "模组管理器",
            themeConfig: {
                nav: [
                    { text: "首页", link: "/zh-CN/" },
                    { text: "指南", link: "/zh-CN/guide/what-is-nahida-desktop" },
                    { text: "功能", link: "/zh-CN/features/index" },
                ],
                sidebar: [
                    {
                        text: "介绍",
                        base: "/zh-CN/guide",
                        items: [
                            {
                                text: "什么是 Nahida Desktop",
                                link: "/what-is-nahida-desktop",
                            },
                            { text: "快速开始", link: "/getting-started" },
                        ],
                    },
                    {
                        text: "功能",
                        link: "/zh-CN/features/index",
                        base: "/zh-CN/features",
                        items: [
                            { text: "GameBanana", link: "/gamebanana" },
                            { text: "模组管理器", link: "/mod-manager" },
                            {
                                text: "模组工具",
                                base: "/zh-CN/features/mod-tools",
                                collapsed: true,
                                items: [
                                    {
                                        text: "d3d11.dll 构建器",
                                        link: "/dll-builder",
                                    },
                                    {
                                        text: "切换状态持久化",
                                        link: "/persist-toggles",
                                    },
                                    {
                                        text: "静态 GLB 转换器 / 模型查看器",
                                        link: "/static-glb-converter",
                                    },
                                ],
                            },
                        ],
                    },
                    {
                        text: "其他",
                        base: "/zh-CN/others",
                        items: [
                            {
                                text: "连接 XXMI",
                                link: "/set-up-xxmi",
                            },
                        ],
                    },
                ],
            },
        },
    },
});
