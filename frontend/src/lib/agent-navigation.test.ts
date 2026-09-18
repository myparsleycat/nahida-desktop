import { Service as Agent } from "@bindings/agent";
import type { NavigateFn } from "@tanstack/react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { openGlobalAgent, openModAgent } from "./agent-navigation";

vi.mock("@bindings/agent", () => ({
    Service: { OpenSession: vi.fn() },
}));

describe("agent navigation", () => {
    const navigate = vi.fn() as unknown as NavigateFn;

    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(Agent.OpenSession).mockResolvedValue({
            id: "session-1",
            title: "Test",
            scope: { type: "global" },
            updatedAt: "now",
            running: false,
        });
    });

    it("opens the global scope from the sidebar", async () => {
        await openGlobalAgent(navigate);
        expect(Agent.OpenSession).toHaveBeenCalledWith({ type: "global" });
        expect(navigate).toHaveBeenCalledWith({
            to: "/agent",
            search: { session: "session-1" },
            replace: true,
        });
    });

    it("opens the exact mod scope from mod entry points", async () => {
        await openModAgent(navigate, { path: "C:\\Mods\\Nahida", name: "Nahida" });
        expect(Agent.OpenSession).toHaveBeenCalledWith({
            type: "mod",
            modPath: "C:\\Mods\\Nahida",
            modName: "Nahida",
        });
    });
});
