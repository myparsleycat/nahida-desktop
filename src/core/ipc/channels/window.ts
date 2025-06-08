// src/core/ipc/channels/window.ts
import { ChannelGroup } from '../registry';

export function defineWindowChannels(rootGroup: ChannelGroup) {
    const windowGroup = rootGroup.addGroup('window');
    windowGroup.addChannel('windowControl', undefined, true, 'window-control');
}