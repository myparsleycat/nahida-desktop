// src/core/ipc/channels/auth.ts
import { ChannelGroup } from '../registry';

export function defineAuthChannels(rootGroup: ChannelGroup) {
    const authGroup = rootGroup.addGroup('auth');
    authGroup.addChannel('startOAuth2Login');
    authGroup.addChannel('checkSessionState');
    authGroup.addChannel('logout');
    authGroup.addChannel('authStateChanged', undefined, true, 'auth-state-changed');
}

export function injectAuthHandlers(registry: any, auth: any) {
    registry.injectHandlers('auth', {
        startOAuth2Login: () => auth.StartOAuth2Login(),
        checkSessionState: () => auth.CheckSessionState(),
        logout: () => auth.Logout()
    });
}