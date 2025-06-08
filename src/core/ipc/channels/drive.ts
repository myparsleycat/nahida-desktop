// src/core/ipc/channels/drive.ts
import { ChannelGroup } from '../registry';

export function defineDriveChannels(rootGroup: ChannelGroup) {
    const driveGroup = rootGroup.addGroup('drive');
    const driveItemGroup = driveGroup.addGroup('item');
    driveItemGroup.addChannel('get');
    driveItemGroup.addChannel('move');
    driveItemGroup.addChannel('rename');
    driveItemGroup.addChannel('trash_many');

    const driveItemDirGroup = driveItemGroup.addGroup('dir');
    driveItemDirGroup.addChannel('create');

    const driveItemDownloadGroup = driveItemGroup.addGroup('download');
    driveItemDownloadGroup.addChannel('enqueue');

    const driveUtilGroup = driveGroup.addGroup('util');
    const driveUtilImageCacheGroup = driveUtilGroup.addGroup('imageCache');
    driveUtilImageCacheGroup.addChannel('get');
    driveUtilImageCacheGroup.addChannel('sizes');
    driveUtilImageCacheGroup.addChannel('clear');
    driveUtilImageCacheGroup.addChannel('getStates');
    driveUtilImageCacheGroup.addChannel('change');
}

export function injectDriveHandlers(registry: any, ADS: any) {
    registry.injectHandlers('drive.item', {
        get: (id: string) => ADS.item.get(id),
        move: (current: string, ids: string[], newParentId: string) => ADS.item.move(current, ids, newParentId),
        rename: (id: string, rename: string) => ADS.item.rename(id, rename),
        trash_many: (ids: string[]) => ADS.item.trash_many(ids)
    });

    registry.injectHandlers('drive.item.dir', {
        create: (parentId: string, dirs: { name: string; path: string; }[]) => ADS.item.dir.create(parentId, dirs)
    });

    registry.injectHandlers('drive.item.download', {
        enqueue: (id: string, _name: string) => ADS.item.download(id)
    });

    registry.injectHandlers('drive.util.imageCache', {
        get: (url: string) => ADS.util.imageCache.get(url),
        sizes: () => ADS.util.imageCache.sizes(),
        clear: () => ADS.util.imageCache.clear(),
        getStates: () => ADS.util.imageCache.getStates(),
        change: (v: boolean) => ADS.util.imageCache.change(v)
    });
}