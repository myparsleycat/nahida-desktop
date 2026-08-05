import ky, { isNetworkError, isTimeoutError } from "ky";

import type { NahidaDesktop } from "../index";

import { appVersion } from "../const";

const NHD_PREFIXES = ["http://localhost", "https://api.nahida.live"];

interface FetcherOptions extends RequestInit {}

function isUnreachableError(error: unknown) {
    return isTimeoutError(error) || isNetworkError(error);
}

export class DesktopHttpService {
    constructor(private readonly desktop: NahidaDesktop) {}

    private isNHD(url: string) {
        return NHD_PREFIXES.some((prefix) => url.startsWith(prefix));
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
        const headers = {
            ...(options?.headers instanceof Headers
                ? Object.fromEntries(options.headers.entries())
                : (options?.headers as Record<string, string> | undefined)),
            ...(await this.getHeaders(url)),
        };

        try {
            const resp = await ky(url, {
                ...options,
                headers,
                timeout: 100000,
                retry: {
                    limit: 2,
                },
                hooks: {
                    afterResponse: [
                        async ({ response }) => {
                            if (response.status === 401 && isNHD) {
                                await this.desktop.service.auth.getSession();
                            }
                        },

                        ({ response }) => {
                            if (response.status === 524) {
                                return new Response("cloudflare timeout. but it's ok", {
                                    status: 200,
                                });
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

            if (isNHD) this.desktop.service.backendConnectivity.setOnline();
            return resp;
        } catch (error) {
            if (isNHD && isUnreachableError(error)) {
                this.desktop.service.backendConnectivity.setOffline();
            }
            throw error;
        }
    }
}
