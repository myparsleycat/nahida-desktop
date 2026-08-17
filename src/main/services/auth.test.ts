import type { NahidaDesktop } from "@main/index";
import { describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
    ky: vi.fn(),
    post: vi.fn(),
}));

vi.mock("ky", () => ({
    default: Object.assign(mocks.ky, { post: mocks.post }),
}));

vi.mock("electron", () => ({ app: { getVersion: () => "test-version" } }));
vi.mock("@main/windows/utils", () => ({ focus: vi.fn() }));
vi.mock("./util", () => ({ openExternal: vi.fn() }));

import { Auth } from "./auth";

function deferred() {
    let resolve!: () => void;
    const promise = new Promise<void>((resolvePromise) => {
        resolve = resolvePromise;
    });
    return { promise, resolve };
}

function sessionResponse(token: string) {
    return new Response(
        JSON.stringify({
            session: {
                id: "session-id",
                userId: "user-id",
                createdAt: "2026-01-01T00:00:00.000Z",
                updatedAt: "2026-01-01T00:00:00.000Z",
                expiresAt: "2027-01-01T00:00:00.000Z",
                token,
            },
            user: {
                id: "user-id",
                name: "User",
                email: "user@example.com",
                role: "user",
                image: null,
            },
            drive: { id: "drive-id", rootId: "root-id" },
        }),
    );
}

describe("Auth token concurrency", () => {
    it("does not attribute an old-token 401 to a token being saved", async () => {
        let storedToken: string | null = "old-token";
        let auth!: Auth;
        const allowSave = deferred();
        const allowOldTokenResponse = deferred();
        const settings = {
            getValue: vi.fn(async () => storedToken),
            upsert: vi.fn(async (_key: string, value: string) => {
                await allowSave.promise;
                storedToken = value;
            }),
            updateValue: vi.fn(async (_key: string, value: null) => {
                storedToken = value;
            }),
        };
        const desktop = {
            lib: {
                crypto: {
                    encryptString: (value: string) => value,
                    decryptString: (value: string) => value,
                },
                db: { settings },
            },
            httpService: {
                fetcher: vi.fn(async () => {
                    const requestToken = await auth.getToken();
                    if (requestToken === "old-token") {
                        await allowOldTokenResponse.promise;
                        return new Response(null, { status: 401 });
                    }
                    return sessionResponse(requestToken ?? "missing-token");
                }),
            },
            ipc: { broadcast: vi.fn() },
            logger: { error: vi.fn() },
        } as unknown as NahidaDesktop;
        auth = new Auth(desktop);

        const savePromise = auth.saveToken("new-token");
        const sessionPromise = auth.getSession();
        await Promise.resolve();
        await Promise.resolve();
        allowSave.resolve();
        await savePromise;
        allowOldTokenResponse.resolve();

        await expect(sessionPromise).resolves.toMatchObject({
            session: { token: "new-token" },
        });
        expect(storedToken).toBe("new-token");
        expect(settings.updateValue).not.toHaveBeenCalled();
    });

    it("does not remove a token saved while guarded logout is reading it", async () => {
        let storedToken: string | null = "old-token";
        const readStarted = deferred();
        const allowRead = deferred();
        let blockNextRead = true;
        const broadcast = vi.fn();
        const settings = {
            getValue: vi.fn(async () => {
                if (blockNextRead) {
                    blockNextRead = false;
                    readStarted.resolve();
                    await allowRead.promise;
                }
                return storedToken;
            }),
            upsert: vi.fn(async (_key: string, value: string) => {
                storedToken = value;
            }),
            updateValue: vi.fn(async (_key: string, value: null) => {
                storedToken = value;
            }),
        };
        const desktop = {
            lib: {
                crypto: {
                    encryptString: (value: string) => value,
                    decryptString: (value: string) => value,
                },
                db: { settings },
            },
            ipc: { broadcast },
            logger: { error: vi.fn() },
        } as unknown as NahidaDesktop;
        const auth = new Auth(desktop);

        const logoutPromise = auth.startLogout(0);
        await readStarted.promise;
        await auth.saveToken("new-token");
        allowRead.resolve();
        await logoutPromise;

        expect(storedToken).toBe("new-token");
        expect(settings.updateValue).not.toHaveBeenCalled();
        expect(broadcast).not.toHaveBeenCalled();
    });
});
