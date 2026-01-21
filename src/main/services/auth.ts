import { eq } from "drizzle-orm";
import { db } from "@main/internal/db";
import { setting } from "@main/internal/db/schema";
import { BACKEND_URL } from "@shared/const";
import { fetcher } from "@main/internal/fetcher";
import { SessionSchema } from "@shared/schemas/auth";
import ky from "ky";
import { Nullable, validate } from "valdex";
import { shell } from "electron";
import { parseServerSentEvents } from "parse-sse";
import { closeAllWindows } from "./util";
import { focus } from "@main/windows/utils";
import type { NahidaDesktop } from "@main/index";
import { appVersion } from "@main/const";

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

        await db.update(setting).set({ value: encryptedKey }).where(eq(setting.key, "token"));
    }

    public async getToken() {
        const key = await db.query.setting.findFirst({
            where: eq(setting.key, "token"),
        });
        if (!key || !key.value) {
            return null;
        }
        try {
            return this.desktop.lib.crypto.decryptString(key.value);
        } catch {
            this.removeToken();
            return null;
        }
    }

    public async removeToken() {
        await db.update(setting).set({ value: null }).where(eq(setting.key, "token"));
    }

    public async getSession() {
        const token = await this.getToken();
        if (!token) return null;

        const url = `${BACKEND_URL}/api/auth/get-session`;
        const resp = await fetcher(url);
        if (!resp.ok) {
            await this.startLogout();
            return null;
        }
        const data = await resp.json();
        if (!data) {
            await this.startLogout();
            return null;
        }
        return SessionSchema.parse(data);
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
            headers: {
                "User-Agent": `Nahida Desktop/${appVersion}`,
            },
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

        await shell.openExternal(data.pageUrl);

        const resp = await ky(data.stateResponse, {
            throwHttpErrors: false,
            headers: {
                "User-Agent": `Nahida Desktop/${appVersion}`,
            },
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
        if (token) {
            const url = `${BACKEND_URL}/api/auth/sign-out`;
            await fetcher(url, {
                method: "POST",
            });
        }

        await this.removeToken();
        closeAllWindows();
        this.desktop.window.auth.createLoginWindow();
    }
}

export default Auth;
