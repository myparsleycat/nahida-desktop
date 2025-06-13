import { ChannelGroup } from '../registry';

export function defineOverlayChannels(rootGroup: ChannelGroup) {
    const authGroup = rootGroup.addGroup('overlay');
    authGroup.addChannel('visibilityChange', undefined, true, 'visibility-change');
}

export function injectOverlayHandlers(registry: any) {
    registry.injectHandlers('overlay', {

    });
}