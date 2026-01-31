import ky from "ky";
import { Agent, Pool, ProxyAgent } from "undici";
import { desktop } from "@main/index";
import { appVersion } from "@main/const";

let cachedAgent: ProxyAgent | null = null;
let cachedProxyHash: string = "";

export async function getAgent() {
    const proxy = await desktop.setting.net.getProxy();
    const proxyHash = JSON.stringify(proxy);

    if (cachedAgent && cachedProxyHash === proxyHash) {
        return cachedAgent;
    }

    if (proxy && proxy.type !== "disabled" && proxy.host && proxy.port) {
        let url: string;
        let token: string | undefined = undefined;

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

        cachedAgent = new ProxyAgent({
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
        cachedAgent = new Agent({
            factory(origin, options) {
                return new Pool(origin, {
                    ...options,
                    allowH2: true,
                });
            },
        });
    }

    cachedProxyHash = proxyHash;
    return cachedAgent;
}

export async function fetcher(url: string, options?: RequestInit) {
    const token = await desktop.service.auth.getToken();
    const prefixs = ["http://localhost", "https://api.nahida.live"];
    const isNHD = prefixs.some((prefix) => url.startsWith(prefix));

    const resp = await ky(url, {
        ...options,
        headers: {
            ...options?.headers,
            ...(token && isNHD && { Authorization: `Bearer ${token}` }),
            "User-Agent": `Nahida Desktop/${appVersion}`,
        },
        // @ts-expect-error - dispatcher is not in the type definition, but it's passed through to fetch.
        dispatcher: await getAgent(),
        hooks: {
            afterResponse: [
                async (_req, _opt, resp) => {
                    if (resp.status === 401 && isNHD) {
                        await desktop.service.auth.getSession();
                    }
                },
            ],
        },
    });

    return resp;
}
