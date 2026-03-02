import { app, session } from "electron";
import ky from "ky";
import { Agent, Pool, ProxyAgent } from "undici";
import { appVersion } from "../const";
import type { NahidaDesktop } from "../index";

const NHD_PREFIXES = ["http://localhost", "https://api.nahida.live"];

export class DesktopHttpService {
    private cachedAgent: Agent | ProxyAgent | null = null;
    private cachedProxyHash = "";

    constructor(private readonly desktop: NahidaDesktop) {}

    public async updateProxy() {
        const proxy = await this.desktop.setting.net.getProxy();
        if (proxy && proxy.type !== "disabled" && proxy.host && proxy.port) {
            const protocol = proxy.type === "socks5" ? "socks5" : "http";
            const proxyRules = `${protocol}://${proxy.host}:${proxy.port}`;
            await app.whenReady();
            await session.defaultSession.setProxy({ proxyRules });
            this.desktop.logger.info(`Proxy updated: ${proxyRules}`, "DesktopHttpService:updateProxy");
        } else {
            await app.whenReady();
            await session.defaultSession.setProxy({ proxyRules: "" });
            this.desktop.logger.info("Proxy disabled", "DesktopHttpService:updateProxy");
        }
    }

    public async getAgent() {
        const proxy = await this.desktop.setting.net.getProxy();
        const proxyHash = JSON.stringify(proxy);

        if (this.cachedAgent && this.cachedProxyHash === proxyHash) {
            return this.cachedAgent;
        }

        if (proxy && proxy.type !== "disabled" && proxy.host && proxy.port) {
            let url: string;
            let token: string | undefined;

            if (proxy.type === "socks5") {
                const auth =
                    proxy.requiresAuth && proxy.username && proxy.password
                        ? `${encodeURIComponent(proxy.username)}:${encodeURIComponent(proxy.password)}@`
                        : "";
                url = `socks5://${auth}${proxy.host}:${proxy.port}`;
            } else {
                url = `http://${proxy.host}:${proxy.port}`;
                if (proxy.requiresAuth && proxy.username && proxy.password) {
                    token = `Basic ${Buffer.from(`${proxy.username}:${proxy.password}`).toString("base64")}`;
                }
            }

            this.cachedAgent = new ProxyAgent({
                uri: url,
                token,
                factory(origin, options) {
                    return new Pool(origin, {
                        ...options,
                        allowH2: true,
                    });
                },
            });
        } else {
            this.cachedAgent = new Agent({
                factory(origin, options) {
                    return new Pool(origin, {
                        ...options,
                        allowH2: true,
                    });
                },
            });
        }

        this.cachedProxyHash = proxyHash;
        return this.cachedAgent;
    }

    public async getHeaders(url: string) {
        const token = await this.desktop.service.auth.getToken();
        const isNHD = NHD_PREFIXES.some((prefix) => url.startsWith(prefix));
        return {
            ...(token && isNHD && { Authorization: `Bearer ${token}` }),
            Origin: "https://nahida.live",
            "User-Agent": `Nahida Desktop/${appVersion}`,
        };
    }

    public async fetcher(url: string, options?: RequestInit) {
        const isNHD = NHD_PREFIXES.some((prefix) => url.startsWith(prefix));

        const resp = await ky(url, {
            ...options,
            headers: {
                ...options?.headers,
                ...(await this.getHeaders(url)),
            },
            timeout: 100000,
            retry: {
                limit: 2,
            },
            // @ts-expect-error - dispatcher is not in the type definition, but it's passed through to fetch.
            dispatcher: await this.getAgent(),
            hooks: {
                afterResponse: [
                    async (_req, _opt, response) => {
                        if (response.status === 401 && isNHD) {
                            await this.desktop.service.auth.getSession();
                        }
                    },

                    (_request, _options, response) => {
                        if (response.status === 524) {
                            return new Response("cloudflare timeout. but it's ok", { status: 200 });
                        } else {
                            return response;
                        }
                    },
                ],

                beforeError: [
                    async (error) => {
                        const { response } = error;
                        if (response && response.status === 524) {
                            return error;
                        }

                        return error;
                    },
                ],
            },
        });

        return resp;
    }
}
