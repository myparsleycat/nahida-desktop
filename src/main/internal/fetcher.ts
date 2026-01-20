import ky from "ky";
import { Agent, Pool } from "undici";
import { desktop } from "@main/index";
import { appVersion } from "@main/const";

const agent = new Agent({
    factory(origin, options) {
        return new Pool(origin, {
            ...options,
            allowH2: true,
        });
    },
});

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
        dispatcher: agent,
    });

    if (resp.status === 401) {
        await desktop.service.auth.startLogout();
    }

    return resp;
}
