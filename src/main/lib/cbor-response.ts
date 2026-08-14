import { Decoder } from "cbor-x";

const decoder = new Decoder({ useRecords: false, mapsAsObjects: true });

export function isCborContentType(contentType: string | null) {
    return contentType?.includes("application/cbor") ?? false;
}

export function decodeCborBody(bytes: Uint8Array) {
    return decoder.decode(bytes);
}

export function jsonResponseFrom(response: Response, data: unknown) {
    const headers = new Headers(response.headers);
    headers.set("Content-Type", "application/json");
    headers.delete("Content-Length");
    return new Response(JSON.stringify(data), {
        status: response.status,
        statusText: response.statusText,
        headers,
    });
}

export async function readApiBody(response: Response) {
    if (isCborContentType(response.headers.get("Content-Type"))) {
        return decodeCborBody(new Uint8Array(await response.arrayBuffer()));
    }

    const text = await response.text();
    if (!text.trim()) return undefined;
    try {
        return JSON.parse(text) as unknown;
    } catch {
        return text;
    }
}

export async function jsonResponseFromBody(
    response: Response,
    mapData?: (data: unknown) => unknown,
) {
    const data = await readApiBody(response);
    try {
        return jsonResponseFrom(response, mapData ? mapData(data) : data);
    } catch {
        return jsonResponseFrom(response, data);
    }
}

function parseDecodedHttpResult(status: number, value: unknown) {
    if (typeof value === "string") return { status, reason: value };
    if (value && typeof value === "object") {
        const payload = value as { status?: string; reason?: string; message?: string };
        return { status, payload, reason: payload.reason ?? payload.message };
    }
    return { status };
}

export function parseHttpBody(
    status: number,
    contentType: string | null,
    body: Uint8Array | string,
) {
    if (body.length === 0) return { status };

    try {
        if (isCborContentType(contentType)) {
            const bytes = typeof body === "string" ? new TextEncoder().encode(body) : body;
            return parseDecodedHttpResult(status, decodeCborBody(bytes));
        }

        const text = typeof body === "string" ? body : new TextDecoder().decode(body);
        if (!text) return { status };
        return parseDecodedHttpResult(status, JSON.parse(text) as unknown);
    } catch {
        const text = typeof body === "string" ? body : new TextDecoder().decode(body);
        return { status, reason: text.slice(0, 200) };
    }
}
