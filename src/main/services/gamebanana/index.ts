import { setting } from "@main/internal/db/schema";
import { focus, getDefaultWebPreferences } from "@main/windows/utils";
import { eq } from "drizzle-orm";
import { BrowserWindow } from "electron";
import ky, { type Input, type Options } from "ky";
import { z, type ZodType } from "zod";
import type { NahidaDesktop } from "@/main";
import {
    GameBananaLoginRequiredSchema,
    GameProfileSchema,
    GameSubfeedSchema,
    GameTopSubsSchema,
    MemberNavigatorPersonalSchema,
    ModCategoriesSchema,
    ModCategoryProfileSchema,
    ModConfigSchema,
    ModIndexSchema,
    ModPostsSchema,
    ModProfileSchema,
} from "./model";

export const gameBananaGames = {
    gi: 8552,
    sr: 18366,
    zz: 19567,
    ww: 20357,
    ef: 21842,
} as const;

export type GameBananaGameKey = keyof typeof gameBananaGames;
export type GameBananaCategorySort = "a_to_z" | "count";
export type GameBananaFeedSort = "default";
export type GameBananaModPostsSort = "popular" | "newest";
export type GameBananaSubmissionModel = "Mod" | "Tool" | (string & {});

export class GameBananaService {
    public readonly games = gameBananaGames;
    private readonly apiBaseUrl = "https://gamebanana.com/apiv11";
    private readonly loginUrl = "https://gamebanana.com/members/account/login";
    private readonly logoutUrl = "https://gamebanana.com/members/account/logout";
    private readonly authUrls = [
        `${this.apiBaseUrl}/Member/Authenticate`,
        `${this.apiBaseUrl}/Member/EmailAuthenticate`,
    ] as const;
    private readonly navigatorPersonalUrl = `${this.apiBaseUrl}/Member/Navigator/Personal`;
    private readonly cookieSettingKey = "gamebanana_auth_cookies";
    private loginWindow: BrowserWindow | null = null;
    private authPromise: Promise<string> | null = null;

    private readonly baseUrls = {
        game: {
            profilePage: `${this.apiBaseUrl}/Game/{}/ProfilePage`,
            topSubs: `${this.apiBaseUrl}/Game/{}/TopSubs`,
            subfeed: `${this.apiBaseUrl}/Game/{}/Subfeed?_sSort={}&_nPage={}`,
        },
        modCategory: {
            profilePage: `${this.apiBaseUrl}/ModCategory/{}/ProfilePage`,
            index: `${this.apiBaseUrl}/Mod/Index?_nPerpage={}&_aFilters%5BGeneric_Category%5D={}&_nPage={}`,
            categories: `${this.apiBaseUrl}/Mod/Categories?_idCategoryRow={}&_sSort={}&_bShowEmpty={}`,
        },
        mod: {
            profilePage: `${this.apiBaseUrl}/Mod/{}/ProfilePage`,
            config: `${this.apiBaseUrl}/Mod/{}/Config`,
            posts: `${this.apiBaseUrl}/Mod/{}/Posts?_nPage={}&_nPerpage={}&_sSort={}`,
        },
    } as const;

    constructor(private readonly desktop: NahidaDesktop) { }

    private getParentWindow() {
        const mainWindow = this.desktop.window.main.window;

        if (!mainWindow || mainWindow.isDestroyed()) {
            return null;
        }

        return mainWindow;
    }

    private async saveCookie(cookie: string) {
        const encryptedCookie = this.desktop.lib.crypto.encryptString(cookie);

        await this.desktop.lib.db
            .insert(setting)
            .values({ key: this.cookieSettingKey, value: encryptedCookie })
            .onConflictDoUpdate({
                target: setting.key,
                set: { value: encryptedCookie },
            });
    }

    private async getCookie() {
        const cookie = await this.desktop.lib.db.query.setting.findFirst({
            where: eq(setting.key, this.cookieSettingKey),
        });

        if (!cookie?.value) {
            return null;
        }

        try {
            return this.desktop.lib.crypto.decryptString(cookie.value);
        } catch {
            await this.removeCookie();
            return null;
        }
    }

    private async removeCookie() {
        await this.desktop.lib.db
            .insert(setting)
            .values({ key: this.cookieSettingKey, value: null })
            .onConflictDoUpdate({
                target: setting.key,
                set: { value: null },
            });
    }

    private parseCookies(setCookieHeaders: string[] | undefined) {
        if (!setCookieHeaders?.length) {
            return null;
        }

        const cookies = new Map<string, string>();

        for (const header of setCookieHeaders) {
            const parts = header.split(";").map((part) => part.trim());
            const cookie = parts[0];
            if (!cookie) {
                continue;
            }

            const separatorIndex = cookie.indexOf("=");
            if (separatorIndex <= 0) {
                continue;
            }

            const name = cookie.slice(0, separatorIndex).trim();
            const value = cookie.slice(separatorIndex + 1).trim();
            if (!name) {
                continue;
            }

            cookies.set(name, `${name}=${value}`);
        }

        return cookies.size > 0 ? Array.from(cookies.values()).join("; ") : null;
    }

    private async validateCookie(cookie: string) {
        try {
            const response = await this.request(this.navigatorPersonalUrl, {
                method: "GET",
                _skipAuth: true,
                _retryAuth: false,
                headers: {
                    Cookie: cookie,
                },
            });

            const data = await response.json();

            if (GameBananaLoginRequiredSchema.safeParse(data).success) {
                return false;
            }

            return MemberNavigatorPersonalSchema.safeParse(data).success;
        } catch {
            return false;
        }
    }

    private async ensureAuthenticated(forceRelogin = false, _depth = 0) {
        if (_depth > 1) {
            throw new Error("GAMEBANANA_AUTH_FAILED");
        }

        if (forceRelogin) {
            await this.removeCookie();
        } else {
            const storedCookie = await this.getCookie();
            if (storedCookie) {
                const isValid = await this.validateCookie(storedCookie);
                if (isValid) {
                    return storedCookie;
                }

                await this.removeCookie();
            }
        }

        if (this.authPromise) {
            await this.authPromise;
            const cookieAfterWait = await this.getCookie();
            if (cookieAfterWait) {
                return cookieAfterWait;
            }
            throw new Error("GAMEBANANA_AUTH_FAILED");
        }

        const authPromise = this.openLoginWindow();
        this.authPromise = authPromise;
        authPromise.finally(() => {
            if (this.authPromise === authPromise) {
                this.authPromise = null;
            }
        });

        await this.authPromise;

        const refreshedCookie = await this.getCookie();
        if (!refreshedCookie) {
            throw new Error("GAMEBANANA_AUTH_FAILED");
        }

        return refreshedCookie;
    }

    public async ensureSession() {
        await this.ensureAuthenticated();
    }

    public async logout() {
        const cookie = await this.getCookie();

        try {
            if (cookie) {
                try {
                    await this.request(this.logoutUrl, {
                        method: "GET",
                        _skipAuth: true,
                        _retryAuth: false,
                        redirect: "manual",
                        headers: {
                            Cookie: cookie,
                        },
                    });
                } catch (error) {
                    this.desktop.logger.warn(
                        `Unexpected GameBanana logout response: ${error instanceof Error ? error.message : String(error)}`,
                        "GameBananaService",
                    );
                }
            }
        } finally {
            await this.removeCookie();
        }
    }

    private async openLoginWindow() {
        return new Promise<string>((resolve, reject) => {
            void (async () => {
                const parentWindow = this.getParentWindow();

                if (this.loginWindow && !this.loginWindow.isDestroyed()) {
                    focus(this.loginWindow);
                    reject(new Error("GAMEBANANA_AUTH_FAILED"));
                    return;
                }

                let settled = false;
                const resolveOnce = (value?: string) => {
                    if (settled) return;
                    settled = true;
                    cleanup();
                    resolve(value ?? "");
                };
                const rejectOnce = (error: Error) => {
                    if (settled) return;
                    settled = true;
                    cleanup();
                    reject(error);
                };

                const loginWindow = new BrowserWindow({
                    title: "GameBanana 로그인",
                    width: 540,
                    height: 760,
                    resizable: false,
                    show: false,
                    maximizable: false,
                    minimizable: false,
                    autoHideMenuBar: true,
                    modal: !!parentWindow,
                    ...(parentWindow ? { parent: parentWindow } : {}),
                    webPreferences: {
                        ...getDefaultWebPreferences(),
                        partition: `gamebanana-login-${Date.now()}`,
                    },
                });

                this.loginWindow = loginWindow;
                const webRequest = loginWindow.webContents.session.webRequest;

                const cleanup = () => {
                    webRequest.onHeadersReceived(null as never);
                    if (this.loginWindow === loginWindow) {
                        this.loginWindow = null;
                    }
                };

                webRequest.onHeadersReceived(
                    { urls: this.authUrls.map((url) => `${url}*`) },
                    async (details, callback) => {
                        const headers = Object.entries(details.responseHeaders ?? {}).reduce<
                            Record<string, string[]>
                        >((acc, [key, value]) => {
                            acc[key.toLowerCase()] = value;
                            return acc;
                        }, {});

                        const cookie = this.parseCookies(headers["set-cookie"]);
                        if (cookie) {
                            try {
                                await this.saveCookie(cookie);
                                callback({
                                    cancel: false,
                                    responseHeaders: details.responseHeaders,
                                });
                                resolveOnce(cookie);
                                if (!loginWindow.isDestroyed()) {
                                    loginWindow.close();
                                }
                                return;
                            } catch {
                                callback({
                                    cancel: false,
                                    responseHeaders: details.responseHeaders,
                                });
                                rejectOnce(new Error("GAMEBANANA_AUTH_FAILED"));
                                if (!loginWindow.isDestroyed()) {
                                    loginWindow.close();
                                }
                                return;
                            }
                        }

                        callback({ cancel: false, responseHeaders: details.responseHeaders });
                    },
                );

                loginWindow.on("ready-to-show", () => {
                    loginWindow.show();
                    focus(loginWindow);
                });

                loginWindow.on("closed", () => {
                    if (!settled) {
                        rejectOnce(new Error("GAMEBANANA_LOGIN_CANCELLED"));
                    } else {
                        cleanup();
                    }
                });

                loginWindow.webContents.setWindowOpenHandler(({ url }) => {
                    loginWindow.loadURL(url);
                    return { action: "deny" };
                });

                try {
                    await loginWindow.loadURL(this.loginUrl);
                } catch {
                    rejectOnce(new Error("GAMEBANANA_AUTH_FAILED"));
                    if (!loginWindow.isDestroyed()) {
                        loginWindow.close();
                    }
                }
            })().catch((error) => {
                reject(error instanceof Error ? error : new Error("GAMEBANANA_AUTH_FAILED"));
            });
        });
    }

    private formatUrl(template: string, ...values: Array<string | number | boolean>) {
        let index = 0;

        return template.replace(/\{\}/g, () => {
            const value = values[index++];

            if (value === undefined) {
                throw new Error("Not enough values provided for URL template");
            }

            return encodeURIComponent(String(value));
        });
    }

    private async request(
        input: Input,
        options?: Options & { _retryAuth?: boolean; _skipAuth?: boolean },
    ) {
        const { _retryAuth = true, _skipAuth = false, ...kyOptions } = options ?? {};
        const cookie = _skipAuth ? null : await this.ensureAuthenticated();

        const response = await ky(input, {
            ...kyOptions,
            throwHttpErrors: false,
            headers: {
                ...kyOptions.headers,
                ...(cookie ? { Cookie: cookie } : {}),
                "User-Agent":
                    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
            },
        });

        if ((response.status === 401 || response.status === 403) && _retryAuth) {
            await this.ensureAuthenticated(true, 1);
            return this.request(input, {
                ...kyOptions,
                _retryAuth: false,
            });
        }

        if (_retryAuth) {
            const data = await response
                .clone()
                .json()
                .catch(() => null);
            if (GameBananaLoginRequiredSchema.safeParse(data).success) {
                await this.ensureAuthenticated(true, 1);
                return this.request(input, {
                    ...kyOptions,
                    _retryAuth: false,
                });
            }
        }

        if (!response.ok) {
            if (response.status === 401 || response.status === 403) {
                await this.removeCookie();
                throw new Error("GAMEBANANA_AUTH_FAILED");
            }

            throw new Error(
                `GAMEBANANA_HTTP_ERROR:${response.status}:${response.statusText || "UNKNOWN"}`,
            );
        }

        return response;
    }

    private async requestJson<T>(schema: ZodType<T>, input: Input, options?: Options): Promise<T> {
        const data = await (await this.request(input, options)).json();
        return schema.parse(data);
    }

    private getSubmissionReferer(modelName: GameBananaSubmissionModel, itemId: number) {
        const segment = `${modelName}`.toLowerCase();
        return `https://gamebanana.com/${segment}s/${itemId}`;
    }

    public async getGameProfile(gameId: number) {
        const url = this.formatUrl(this.baseUrls.game.profilePage, gameId);
        return await this.requestJson(GameProfileSchema, url, {
            method: "GET",
            headers: {
                Referer: `https://gamebanana.com/games/${gameId}`,
            },
        });
    }

    public async getGameTopSubs(gameId: number) {
        const url = this.formatUrl(this.baseUrls.game.topSubs, gameId);
        return await this.requestJson(GameTopSubsSchema, url, {
            method: "GET",
            headers: {
                Referer: `https://gamebanana.com/games/${gameId}`,
            },
        });
    }

    public async getGameSubfeed({
        gameId,
        sort = "default",
        page = 1,
    }: {
        gameId: number;
        sort?: GameBananaFeedSort;
        page?: number;
    }) {
        const url = this.formatUrl(this.baseUrls.game.subfeed, gameId, sort, page);
        return await this.requestJson(GameSubfeedSchema, url, {
            method: "GET",
            headers: {
                Referer: `https://gamebanana.com/games/${gameId}`,
            },
        });
    }

    public async getModCategoryProfile(categoryId: number) {
        const url = this.formatUrl(this.baseUrls.modCategory.profilePage, categoryId);
        return await this.requestJson(ModCategoryProfileSchema, url, {
            method: "GET",
            headers: {
                Referer: `https://gamebanana.com/mods/cats/${categoryId}`,
            },
        });
    }

    public async getModIndex({
        categoryId,
        perPage = 15,
        page = 1,
    }: {
        categoryId: number;
        perPage?: number;
        page?: number;
    }) {
        const url = this.formatUrl(this.baseUrls.modCategory.index, perPage, categoryId, page);
        return await this.requestJson(ModIndexSchema, url, {
            method: "GET",
            headers: {
                Referer: `https://gamebanana.com/mods/cats/${categoryId}`,
            },
        });
    }

    public async getModCategories({
        categoryId,
        sort = "a_to_z",
        showEmpty = true,
    }: {
        categoryId: number;
        sort?: GameBananaCategorySort;
        showEmpty?: boolean;
    }) {
        const url = this.formatUrl(
            this.baseUrls.modCategory.categories,
            categoryId,
            sort,
            showEmpty,
        );
        return await this.requestJson(ModCategoriesSchema, url, {
            method: "GET",
            headers: {
                Referer: `https://gamebanana.com/mods/cats/${categoryId}`,
            },
        });
    }

    public async getModProfile(
        itemId: number,
        modelName: GameBananaSubmissionModel = "Mod",
    ) {
        const url = this.formatUrl(`${this.apiBaseUrl}/${modelName}/{}/ProfilePage`, itemId);
        return await this.requestJson(ModProfileSchema, url, {
            method: "GET",
            headers: {
                Referer: this.getSubmissionReferer(modelName, itemId),
            },
        });
    }

    public async getModConfig(
        itemId: number,
        modelName: GameBananaSubmissionModel = "Mod",
    ) {
        const url = this.formatUrl(`${this.apiBaseUrl}/${modelName}/{}/Config`, itemId);
        return await this.requestJson(ModConfigSchema, url, {
            method: "GET",
            headers: {
                Referer: this.getSubmissionReferer(modelName, itemId),
            },
        });
    }

    public async getModPosts({
        modId,
        modelName = "Mod",
        page = 1,
        perPage = 15,
        sort = "popular",
    }: {
        modId: number;
        modelName?: GameBananaSubmissionModel;
        page?: number;
        perPage?: number;
        sort?: GameBananaModPostsSort;
    }) {
        const url = this.formatUrl(
            `${this.apiBaseUrl}/${modelName}/{}/Posts?_nPage={}&_nPerpage={}&_sSort={}`,
            modId,
            page,
            perPage,
            sort,
        );
        return await this.requestJson(ModPostsSchema, url, {
            method: "GET",
            headers: {
                Referer: this.getSubmissionReferer(modelName, modId),
            },
        });
    }

    public async getGameOverview(gameId: number) {
        const [profile, topSubs, subfeed] = await Promise.all([
            this.getGameProfile(gameId),
            this.getGameTopSubs(gameId),
            this.getGameSubfeed({ gameId }),
        ]);

        return {
            profile,
            topSubs,
            subfeed,
        };
    }

    public async getModCategoryOverview({
        categoryId,
        perPage = 15,
        page = 1,
        sort = "a_to_z",
        showEmpty = true,
    }: {
        categoryId: number;
        perPage?: number;
        page?: number;
        sort?: GameBananaCategorySort;
        showEmpty?: boolean;
    }) {
        const [profile, index, categories] = await Promise.all([
            this.getModCategoryProfile(categoryId),
            this.getModIndex({ categoryId, perPage, page }),
            this.getModCategories({ categoryId, sort, showEmpty }),
        ]);

        return {
            profile,
            index,
            categories,
        };
    }

    public async getModOverview({
        itemId,
        modelName = "Mod",
    }: {
        itemId: number;
        modelName?: GameBananaSubmissionModel;
    }) {
        const [profile, config] = await Promise.all([
            this.getModProfile(itemId, modelName),
            this.getModConfig(itemId, modelName),
        ]);

        return {
            profile,
            config,
        };
    }
}

export type GameBananaGameOverview =
    z.infer<typeof GameProfileSchema> extends infer T
    ? {
        profile: T;
        topSubs: z.infer<typeof GameTopSubsSchema>;
        subfeed: z.infer<typeof GameSubfeedSchema>;
    }
    : never;
