import { treaty } from "@elysiajs/eden";
import type { App } from "@backend/index";
import { BACKEND_URL } from "@shared/const";
import ky from "ky";
import { isEmpty } from "es-toolkit/compat";
import { desktop } from "./index";
import { appVersion } from "@main/const";

const fetcher = async (url: string | Request | URL, init?: RequestInit) => {
    const headers = new Headers(init?.headers);

    const token = await desktop.service.auth.getToken();

    headers.set("User-Agent", `Nahida Desktop/${appVersion}`);
    headers.set("Authorization", `Bearer ${token}`);

    return ky(url, {
        ...init,
        headers,
        throwHttpErrors: false,
        timeout: 100000, // 100sec cloudflare 524 limit
        retry: {
            limit: 2,
        },
    });
};

export const eden = treaty<App>(BACKEND_URL, {
    fetcher: (async (input: URL | RequestInfo, init: RequestInit | undefined) => {
        let response = await fetcher(input, init);

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

    const target = () => { };
    return new Proxy(target, handler) as unknown as EdenProxy;
}

export const eden2url = createProxy();
