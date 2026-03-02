/** biome-ignore-all lint/suspicious/noExplicitAny: <> */
import type { App } from "@backend/index";
import { treaty } from "@elysiajs/eden";
import { BACKEND_URL } from "@shared/const";
import { isEmpty } from "es-toolkit/compat";
import { desktop } from "./index";

export const eden = treaty<App>(BACKEND_URL, {
    fetcher: (async (input: URL | RequestInfo, init: RequestInit | undefined) => {
        const response = await desktop.httpService.fetcher(input.toString(), init);

        if (response.status === 401) {
            await desktop.service.auth.startLogout();
        }

        return response;
    }) as typeof fetch,
});

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

