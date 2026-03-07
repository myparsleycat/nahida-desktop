import ky from "ky";
import { Agent, Pool } from "undici";
import { appVersion } from "../const";
import type { NahidaDesktop } from "../index";

const NHD_PREFIXES = ["http://localhost", "https://api.nahida.live"];

export class DesktopHttpService {
    private cachedAgent: Agent | null = null;

    constructor(private readonly desktop: NahidaDesktop) {}

    public async getAgent() {
        if (this.cachedAgent) {
            return this.cachedAgent;
        }

        this.cachedAgent = new Agent({
            factory(origin, options) {
                return new Pool(origin, {
                    ...options,
                    allowH2: true,
                });
            },
        });

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
