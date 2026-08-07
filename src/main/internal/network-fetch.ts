import { net } from "electron";

type FetchInput = string | URL | Request;

/**
 * Use Chromium's network stack for long-lived download responses.
 *
 * Node's bundled Undici parser can assert when a response is aborted while its
 * body is backpressured. Electron's network stack exposes the same Fetch API,
 * while the fallback keeps unit tests runnable in Node.
 */
export function networkFetch(input: FetchInput, init?: RequestInit): Promise<Response> {
    if (typeof net?.fetch === "function") {
        return net.fetch(input instanceof URL ? input.toString() : input, init);
    }

    return globalThis.fetch(input, init);
}
