import { Service as Agent } from "@bindings/agent";
import type { NavigateFn } from "@tanstack/react-router";

export async function openGlobalAgent(navigate: NavigateFn) {
    const session = await Agent.OpenSession({ type: "global" });
    await navigate({ to: "/agent", search: { session: session.id }, replace: true });
}

export async function openModAgent(navigate: NavigateFn, mod: { path: string; name: string }) {
    const session = await Agent.OpenSession({ type: "mod", modPath: mod.path, modName: mod.name });
    await navigate({ to: "/agent", search: { session: session.id }, replace: true });
}
