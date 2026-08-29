import { Auth, type Session } from "@bindings/auth";
import { Setting } from "@bindings/setting";
import { Events } from "@wailsio/runtime";

import { loadFrontendBootstrap, type FrontendBootstrap } from "./bootstrap";

const services = {
    getSession: Auth.GetSession,
    hasToken: Auth.HasToken,
    getBackendStatus: Auth.GetBackendStatus,
    getDefaultStartPage: Setting.GetDefaultStartPage,
};

export function loadWailsFrontendBootstrap(): Promise<FrontendBootstrap<Session>> {
    return loadFrontendBootstrap(services);
}

export function onAuthUpdate(callback: (session: Session | null) => void) {
    return Events.On("auth:update", (event) => callback(event.data as Session | null));
}

export function onBackendStatus(callback: (status: string) => void) {
    return Events.On("backend:status", (event) => callback(String(event.data)));
}
