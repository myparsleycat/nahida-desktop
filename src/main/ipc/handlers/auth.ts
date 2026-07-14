import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerAuthHandlers(d: NahidaDesktop) {
    rh("auth:startLogin", () => d.service.auth.startLogin());
    rh("auth:startLogout", () => d.service.auth.startLogout());
    rh("auth:isLoggedIn", () => d.service.auth.isLoggedIn());
    rh("auth:getSession", () => d.service.auth.getSession());
    rh("auth:hasToken", () => d.service.auth.hasToken());
    rh("backend:getStatus", () => d.service.backendConnectivity.getStatus());
}
