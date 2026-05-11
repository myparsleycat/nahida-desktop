import crypto from "node:crypto";

export const MODEL_VIEWER_MEMORY_PROTOCOL = "model-viewer-memory";

const sessions = new Map<string, Map<string, Buffer>>();

export function createModelViewerMemorySession() {
    const sessionId = crypto.randomUUID();
    sessions.set(sessionId, new Map());
    return sessionId;
}

export function writeModelViewerMemoryBuffer(sessionId: string, bufferId: string, buffer: Buffer) {
    const session = sessions.get(sessionId);
    if (!session) {
        throw new Error(`Missing model viewer memory session: ${sessionId}`);
    }

    session.set(bufferId, buffer);
    return `${MODEL_VIEWER_MEMORY_PROTOCOL}://${sessionId}/${encodeURIComponent(bufferId)}`;
}

export function cleanupModelViewerMemorySession(sessionId: string | undefined) {
    if (sessionId) {
        sessions.delete(sessionId);
    }
}

export async function handleModelViewerMemoryProtocol(request: Request) {
    const url = new URL(request.url);
    const buffer = sessions.get(url.host)?.get(decodeURIComponent(url.pathname.slice(1)));
    if (!buffer) {
        return new Response("not found", { status: 404 });
    }

    return new Response(buffer as BodyInit, {
        headers: {
            "Cache-Control": "no-store",
            "Content-Length": buffer.byteLength.toString(),
            "Content-Type": "application/octet-stream",
        },
    });
}
