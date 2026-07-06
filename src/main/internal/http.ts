import ky from "ky";
import { Agent, Pool } from "undici";

import type { NahidaDesktop } from "../index";

import { appVersion } from "../const";

const NHD_PREFIXES = ["http://localhost", "https://api.nahida.live"];

interface FetcherOptions extends RequestInit {}

export class DesktopHttpService {
    private cachedAgent: Agent | null = null;

    constructor(private readonly desktop: NahidaDesktop) {}

    private isNHD(url: string) {
        return NHD_PREFIXES.some((prefix) => url.startsWith(prefix));
    }

    public async getAgent() {
        if (this.cachedAgent) {
            return this.cachedAgent;
        }

        this.cachedAgent = new Agent({
            factory: (origin, options: Pool.Options) =>
                new Pool(origin, {
                    ...options,
                    allowH2: true,
                }),
        });

        return this.cachedAgent;
    }

    public async getHeaders(url: string) {
        const token = await this.desktop.service.auth.getToken();
        return {
            ...(token && this.isNHD(url) && { Authorization: `Bearer ${token}` }),
            "User-Agent": `Nahida Desktop/${appVersion}`,
        };
    }

    public async fetcher(url: string, options?: FetcherOptions) {
        const isNHD = this.isNHD(url);

        const resp = await ky(url, {
            ...options,
            headers: {
                ...(options?.headers instanceof Headers
                    ? Object.fromEntries(options.headers.entries())
                    : (options?.headers as Record<string, string> | undefined)),
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
                    async ({ response }) => {
                        if (response.status === 401 && isNHD) {
                            await this.desktop.service.auth.getSession();
                        }
                    },

                    ({ response }) => {
                        if (response.status === 524) {
                            return new Response("cloudflare timeout. but it's ok", { status: 200 });
                        } else {
                            return response;
                        }
                    },
                ],

                beforeError: [
                    // @ts-expect-error
                    async ({ response, error }) => {
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
