import { appVersion } from "@main/const";
import type { NahidaDesktop } from "@main/index";
import { focus } from "@main/windows/utils";
import { BACKEND_URL } from "@shared/const";
import { SessionSchema } from "@shared/schemas/auth";
import ky from "ky";
import { parseServerSentEvents } from "parse-sse";
import { Nullable, validate } from "valdex";

import { openExternal } from "./util";

export class Auth {
    private desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public async setLoggedIn({ loggedIn, token }: { loggedIn: boolean; token?: string | null }) {
        if (loggedIn && token) {
            await this.saveToken(token);
        } else {
            await this.removeToken();
        }
    }

    public async saveToken(key: string) {
        const encryptedKey = this.desktop.lib.crypto.encryptString(key);
        await this.desktop.lib.db.settings.upsert("token", encryptedKey);
    }

    public async getToken() {
        const key = await this.desktop.lib.db.settings.getValue("token");
        if (!key) {
            return null;
        }
        try {
            return this.desktop.lib.crypto.decryptString(key);
        } catch {
            void this.removeToken();
            return null;
        }
    }

    public async removeToken() {
        await this.desktop.lib.db.settings.updateValue("token", null);
    }

    public async hasToken() {
        return !!(await this.getToken());
    }

    public async getSession() {
        const token = await this.getToken();
        if (!token) return null;

        const url = `${BACKEND_URL}/api/auth/get-session`;
        const resp = await this.desktop.httpService.fetcher(url, { throwHttpErrors: false });
        if (!resp.ok) {
            if (resp.status === 401) await this.startLogout();
            return null;
        }
        const data = await resp.text();
        if (data === "null") {
            await this.startLogout();
            return null;
        }
        return SessionSchema.parse(JSON.parse(data));
    }

    public async isLoggedIn() {
        const session = await this.getSession();
        return !!session;
    }

    public async startLogin() {
        this.desktop.logger.info("start login", "Auth");

        const iWantToLoginUrl = `${BACKEND_URL}/api/auth/desktop/auth/i-want-to-login`;

        const iWantToLoginResp = await ky(iWantToLoginUrl, {
            credentials: "include",
            throwHttpErrors: false,
            headers: await this.desktop.httpService.getHeaders(iWantToLoginUrl),
        });

        if (!iWantToLoginResp.ok) {
            throw new Error("Failed to get iWantToLogin data");
        }

        const data = await iWantToLoginResp.json();
        validate(data, {
            state: String,
            pageUrl: String,
            stateResponse: String,
        });

        await openExternal(data.pageUrl);

        const resp = await ky(data.stateResponse, {
            throwHttpErrors: false,
            headers: await this.desktop.httpService.getHeaders(data.stateResponse),
        });

        if (!resp.body) {
            throw new Error("SSE response body is null");
        }

        this.desktop.logger.info("start parse sse", "Auth");

        for await (const e of parseServerSentEvents(resp)) {
            if (e.type === "state-response") {
                try {
                    const payload = JSON.parse(e.data);

                    validate(payload, {
                        state: String,
                        status: String,
                        session: Nullable({
                            userId: String,
                            token: String,
                        }),
                    });

                    if (payload.status === "loggedin" && payload.session?.token) {
                        await this.saveToken(payload.session.token);

                        this.desktop.logger.info("Login successful: Session saved.", "Auth");

                        const session = await this.getSession();
                        this.desktop.ipc.broadcast("auth:update", session);

                        if (!this.desktop.lib.tray.tray) {
                            this.desktop.lib.tray.createTray();
                        }
                        await this.desktop.window.main.createMainWindow().then((window) => {
                            if (window) focus(window);
                        });
                        if (this.desktop.window.auth.window)
                            this.desktop.window.auth.window.close();
                    }

                    if (payload.status === "expired") {
                        this.desktop.logger.error("Auth state expired", "Auth");
                        break;
                    }
                } catch (err) {
                    this.desktop.logger.error(err, "Auth");
                }
            }

            if (e.type === "ping") {
            }
        }
    }

    public async startLogout() {
        const token = await this.getToken();
        await this.removeToken();
        this.desktop.ipc.broadcast("auth:update", null);

        if (!token) return;

        try {
            await ky.post(`${BACKEND_URL}/api/auth/sign-out`, {
                timeout: 10000,
                retry: 0,
                throwHttpErrors: false,
                headers: {
                    Authorization: `Bearer ${token}`,
                    "User-Agent": `Nahida Desktop/${appVersion}`,
                },
            });
        } catch (err) {
            this.desktop.logger.error(err, "Auth");
        }
    }
}

export default Auth;
