// oxlint-disable typescript/no-explicit-any
import type { App } from "@backend/index";
import { isMinified, unminify } from "@backend/utils/jsonMinify";
import { treaty } from "@elysiajs/eden";
import { BACKEND_URL } from "@shared/const";
import { isEmpty } from "es-toolkit/compat";

import { desktop } from "./index";
import {
    decodeCborBody,
    isCborContentType,
    jsonResponseFrom,
    readApiBody,
} from "./lib/cbor-response";

export const eden = treaty<App>(BACKEND_URL, {
    fetcher: (async (input: URL | RequestInfo, init: RequestInit | undefined) => {
        const url = input instanceof Request ? input.url : input.toString();
        let response = await desktop.httpService.fetcher(url, init);

        if (response.status === 401) {
            await desktop.service.auth.startLogout();
        }

        const contentType = response.headers.get("Content-Type");
        if (isCborContentType(contentType)) {
            try {
                return rewriteEdenBody(
                    response,
                    decodeCborBody(new Uint8Array(await response.arrayBuffer())),
                );
            } catch (error) {
                desktop.logger.error(error, "EdenCborDecodeFailed");
                const retryUrl = new URL(url);
                retryUrl.searchParams.set("res", "json");
                response = await desktop.httpService.fetcher(retryUrl.toString(), init);
            }
        }

        if (!contentType?.includes("application/json") && !isCborContentType(contentType)) {
            return response;
        }

        try {
            return rewriteEdenBody(response, await readApiBody(response));
        } catch {
            return response;
        }
    }) as typeof fetch,
    parseDate: false,
});

function rewriteEdenBody(response: Response, data: unknown) {
    return jsonResponseFrom(response, isMinified(data) ? unminify(data) : data);
}

export type Eden = typeof eden;

type EdenProxy = {
    [K in string]: EdenProxy;
} & ((args?: Record<string, any>) => EdenProxy) & {
        url: (options?: { query?: Record<string, any> }) => string;
    };

function createProxy(pathSegments: string[] = []): EdenProxy {
    const handler: ProxyHandler<any> = {
        get(_target, prop: string) {
            if (prop === "url") {
                return ({ query }: { query?: Record<string, any> } = {}) => {
                    const path = pathSegments.join("/");
                    const url = new URL(`${BACKEND_URL}/${path}`);

                    if (query && !isEmpty(query)) {
                        Object.entries(query).forEach(([key, value]) => {
                            if (value !== undefined && value !== null) {
                                url.searchParams.append(key, String(value));
                            }
                        });
                    }

                    return url.toString();
                };
            }
            return createProxy([...pathSegments, prop]);
        },
        apply(_target, _thisArg, args) {
            const firstArg = args[0];
            if (firstArg && typeof firstArg === "object") {
                const pathValues = Object.values(firstArg).map(String);
                return createProxy([...pathSegments, ...pathValues]);
            }
            return createProxy(pathSegments);
        },
    };

    const target = () => {};
    return new Proxy(target, handler) as unknown as EdenProxy;
}

export const eden2url = createProxy();
