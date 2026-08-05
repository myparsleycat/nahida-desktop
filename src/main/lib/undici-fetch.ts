import { Agent, fetch as undiciFetch } from "undici";

export function createUndiciFetcher(agent: Agent) {
    return async (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
        const request = input instanceof Request ? input : null;
        const url =
            typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
        const body = init?.body ?? request?.body;

        return (await undiciFetch(url, {
            ...init,
            ...(request && {
                method: init?.method ?? request.method,
                headers: init?.headers ?? request.headers,
                body,
                signal: init?.signal ?? request.signal,
            }),
            ...(body !== undefined && body !== null && { duplex: "half" }),
            dispatcher: agent,
        } as Parameters<typeof undiciFetch>[1])) as unknown as Response;
    };
}
