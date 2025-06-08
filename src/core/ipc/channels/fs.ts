// src/core/ipc/channels/fs.ts
import { ChannelGroup } from '../registry';
import type { ReadDirectoryOptions } from '@shared/types/fs.types';

export function defineFsChannels(rootGroup: ChannelGroup) {
    const fssGroup = rootGroup.addGroup('fss');
    fssGroup.addChannel('readDir');
    fssGroup.addChannel('readFile');
    fssGroup.addChannel('writeFile');
    fssGroup.addChannel('getStat');
    fssGroup.addChannel('openPath');
    fssGroup.addChannel('deletePath');
    fssGroup.addChannel('getImgBase64');
    fssGroup.addChannel('watchFolder');
    fssGroup.addChannel('unwatchFolder');
    fssGroup.addChannel('getWatchedFolders');
    fssGroup.addChannel('folderEvent', undefined, true, 'fss-folder-event');
    fssGroup.addChannel('select');

    const fsDirGroup = fssGroup.addGroup('dir');
    fsDirGroup.addChannel('read');
}

export function injectFsHandlers(registry: any, fss: any) {
    registry.injectHandlers('fss', {
        readDir: (path: string, options: ReadDirectoryOptions) => fss.readDirectory(path, options),
        readFile: (path: string) => fss.readFile(path, "arrbuf"),
        writeFile: (path: string, data: ArrayBuffer) => fss.writeFile(path, Buffer.from(data)),
        getStat: (path: string) => fss.getStat(path),
        openPath: (path: string) => fss.openPath(path),
        deletePath: (path: string) => fss.deletePath(path),
        getImgBase64: (path: string) => fss.getImgBase64(path),
        watchFolder: (path: string, options: any) => fss.watchFolderChanges(path, options),
        unwatchFolder: (path: string) => fss.unwatchFolder(path),
        getWatchedFolders: () => fss.getWatchedFolders(),
        select: (opt: Electron.OpenDialogOptions) => fss.select(opt)
    });

    registry.injectHandlers('fss.dir', {
        read: (path: string) => fss.readDirectory(path)
    });
}