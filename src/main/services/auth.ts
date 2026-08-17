import { appVersion } from "@main/const";
import type { NahidaDesktop } from "@main/index";
import { focus } from "@main/windows/utils";
import { BACKEND_URL } from "@shared/const";
import { SessionSchema, type Session } from "@shared/schemas/auth";
import ky from "ky";
import { parseServerSentEvents } from "parse-sse";
import { Nullable, validate } from "valdex";

import { openExternal } from "./util";

export class Auth {
    private desktop: NahidaDesktop;
    private sessionInFlight: Promise<Session | null> | null = null;
    private tokenMutationInFlight = Promise.resolve();
    private tokenGeneration = 0;

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
        await this.mutateToken(() => this.desktop.lib.db.settings.upsert("token", encryptedKey));
    }

    public async getToken() {
        await this.tokenMutationInFlight;
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
        await this.mutateToken(() => this.desktop.lib.db.settings.updateValue("token", null));
    }

    private async mutateToken(mutation: () => Promise<unknown>) {
        this.sessionInFlight = null;
        this.tokenGeneration++;
        const queuedMutation = this.tokenMutationInFlight.then(mutation);
        this.tokenMutationInFlight = queuedMutation.then(
            () => undefined,
            () => undefined,
        );
        await queuedMutation;
    }

    public async hasToken() {
        return !!(await this.getToken());
    }

    public async getSession() {
        if (this.sessionInFlight) return this.sessionInFlight;

        const fetchPromise = this.fetchSession();
        this.sessionInFlight = fetchPromise;
        try {
            return await fetchPromise;
        } finally {
            if (this.sessionInFlight === fetchPromise) {
                this.sessionInFlight = null;
            }
        }
    }

    private async fetchSession(): Promise<Session | null> {
        const capturedGeneration = this.tokenGeneration;
        const token = await this.getToken();
        if (this.tokenGeneration !== capturedGeneration) return this.fetchSession();
        if (!token) return null;

        const url = `${BACKEND_URL}/api/auth/get-session`;
        const resp = await this.desktop.httpService.fetcher(url, {
            throwHttpErrors: false,
            headers: { Authorization: `Bearer ${token}` },
        });

        // A newer token landed in flight; this response no longer belongs to the current session.
        if (this.tokenGeneration !== capturedGeneration) return this.fetchSession();

        if (!resp.ok) {
            if (resp.status === 401) await this.startLogout(capturedGeneration);
            return null;
        }
        const data = await resp.text();
        if (data === "null") {
            await this.startLogout(capturedGeneration);
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

    public async startLogout(expectedGeneration?: number) {
        // If a generation was provided, verify we're still on that generation
        if (expectedGeneration !== undefined && this.tokenGeneration !== expectedGeneration) {
            return;
        }

        const token = await this.getToken();
        if (expectedGeneration !== undefined && this.tokenGeneration !== expectedGeneration) {
            return;
        }
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
