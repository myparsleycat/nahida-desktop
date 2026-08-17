import ky, { isNetworkError, isTimeoutError } from "ky";

import type { NahidaDesktop } from "../index";

import { appVersion } from "../const";
import { isBackendUnavailableStatus } from "../services/drive-errors";

const NHD_PREFIXES = ["http://localhost", "https://api.nahida.live"];

interface FetcherOptions extends RequestInit {
    throwHttpErrors?: boolean;
}

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
        const isSessionRequest = URL.parse(url)?.pathname === "/api/auth/get-session";
        const optionHeaders =
            options?.headers instanceof Headers
                ? Object.fromEntries(options.headers.entries())
                : ((options?.headers as Record<string, string> | undefined) ?? {});
        const hasAuthorization = Object.keys(optionHeaders).some(
            (key) => key.toLowerCase() === "authorization",
        );
        // Keep a caller-supplied Authorization instead of resolving a newer token mid-request.
        const headers = hasAuthorization
            ? { ...optionHeaders, "User-Agent": `Nahida Desktop/${appVersion}` }
            : { ...optionHeaders, ...(await this.getHeaders(url)) };

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
                            if (response.status === 401 && isNHD && !isSessionRequest) {
                                const responseBody = await response
                                    .clone()
                                    .text()
                                    .catch(() => "");
                                const normalizedResponseBody = responseBody
                                    .toLowerCase()
                                    .replace(/[_-]+/g, " ");
                                if (
                                    normalizedResponseBody.includes("password required") ||
                                    normalizedResponseBody.includes("missing password")
                                ) {
                                    return;
                                }

                                try {
                                    await this.desktop.service.auth.getSession();
                                } catch (error) {
                                    this.desktop.logger.warn(
                                        {
                                            url,
                                            status: response.status,
                                            stage: "refresh-session",
                                            error:
                                                error instanceof Error
                                                    ? error.message
                                                    : String(error),
                                        },
                                        "Http:AuthRefreshFailed",
                                    );
                                }
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

            if (isNHD) {
                if (isBackendUnavailableStatus(resp.status)) {
                    this.desktop.service.backendConnectivity.setOffline();
                } else {
                    this.desktop.service.backendConnectivity.setOnline();
                }
            }
            return resp;
        } catch (error) {
            if (isNHD && isUnreachableError(error)) {
                this.desktop.service.backendConnectivity.setOffline();
            }
            throw error;
        }
    }
}
