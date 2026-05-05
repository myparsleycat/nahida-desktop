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
                items: [
                    { text: "Set Up XXMI", link: "/others/set-up-xxmi" },
                ],
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
                            {
                                text: "모드 매니저",
                                link: "/ko/features/mod-manager",
                            },
                            {
                                text: "모드 툴",
                                items: [
                                    {
                                        text: "d3d11.dll 빌더",
                                        link: "/ko/features/mod-tools/dll-builder",
                                    },
                                    {
                                        text: "토글 영구 저장",
                                        link: "/ko/features/mod-tools/persist-toggles",
                                    },
                                    {
                                        text: "정적 GLB 변환기",
                                        link: "/ko/features/mod-tools/static-glb-converter",
                                    },
                                ],
                            },
                        ],
                    },
                    {
                        text: "기타",
                        items: [
                            {
                                text: "XXMI 연결하기",
                                link: "/ko/others/set-up-xxmi",
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
                        items: [
                            {
                                text: "Nahida Desktop とは",
                                link: "/ja/guide/what-is-nahida-desktop",
                            },
                            { text: "はじめに", link: "/ja/guide/getting-started" },
                        ],
                    },
                    {
                        text: "機能",
                        items: [
                            { text: "概要", link: "/ja/features/index" },
                            { text: "GameBanana", link: "/ja/features/gamebanana" },
                            { text: "MODマネージャー", link: "/ja/features/mod-manager" },
                            {
                                text: "MOD ツール",
                                items: [
                                    {
                                        text: "d3d11.dll ビルダー",
                                        link: "/ja/features/mod-tools/dll-builder",
                                    },
                                    {
                                        text: "トグル状態の永続保存",
                                        link: "/ja/features/mod-tools/persist-toggles",
                                    },
                                    {
                                        text: "静的 GLB 変換 / モデルビューア",
                                        link: "/ja/features/mod-tools/static-glb-converter",
                                    },
                                ],
                            },
                        ],
                    },
                    {
                        text: "その他",
                        items: [
                            {
                                text: "XXMI の設定",
                                link: "/ja/others/set-up-xxmi",
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
                        items: [
                            {
                                text: "什么是 Nahida Desktop",
                                link: "/zh-CN/guide/what-is-nahida-desktop",
                            },
                            { text: "快速开始", link: "/zh-CN/guide/getting-started" },
                        ],
                    },
                    {
                        text: "功能",
                        items: [
                            { text: "概览", link: "/zh-CN/features/index" },
                            { text: "GameBanana", link: "/zh-CN/features/gamebanana" },
                            { text: "模组管理器", link: "/zh-CN/features/mod-manager" },
                            {
                                text: "模组工具",
                                items: [
                                    {
                                        text: "d3d11.dll 构建器",
                                        link: "/zh-CN/features/mod-tools/dll-builder",
                                    },
                                    {
                                        text: "切换状态持久化",
                                        link: "/zh-CN/features/mod-tools/persist-toggles",
                                    },
                                    {
                                        text: "静态 GLB 转换器 / 模型查看器",
                                        link: "/zh-CN/features/mod-tools/static-glb-converter",
                                    },
                                ],
                            },
                        ],
                    },
                    {
                        text: "其他",
                        items: [
                            {
                                text: "连接 XXMI",
                                link: "/zh-CN/others/set-up-xxmi",
                            },
                        ],
                    },
                ],
            },
        },
    },
});
