// src/core/services/mods.service.ts

import { db } from "../db";
import { FSService } from "./fs.service";
import type { FileInfo, ModFolders, ReadDirectoryOptions } from "@shared/types/fs.types";
import { nanoid } from 'nanoid';
import { basename, dirname, join } from "node:path";
import { iniutil as ini, IniParseResult } from "@core/lib/InIUtil";
import { ToastService } from "./toast.service";
import Validator from "@shared/utils/Validator";

class ModsServiceClass {
    currentFolderPath: string | null = null;
    currentCharPath: string | null = null;

    ui = {
        resizable: {
            get: async () => {
                const size = await db.get("LocalStorage", "mods_resizable_default");
                return size || 20;
            },

            set: async (size: number) => {
                await db.update("LocalStorage", "mods_resizable_default", size);
            }
        },
        layout: {
            get: async () => {
                const layout = await db.get('LocalStorage', 'mods_layout');
                if (!layout) return 'grid';
                return layout
            },
            set: async (layout: 'grid' | 'list') => {
                await db.update('LocalStorage', 'mods_layout', layout);
            }
        }
    }

    folder = {
        getAll: async () => {
            return await db.query(`SELECT * from ModFolders ORDER BY seq ASC`) as ModFolders[];
        },
        create: async (path: string, name: string) => {
            try {
                const folder = await db.get('ModFolders', path);
                if (folder) {
                    ToastService.warning('이미 등록된 폴더입니다');
                    return false;
                } else {
                    const folder = (await db.query(`select * from ModFolders where name = '${name}'`))[0];
                    if (folder) {
                        ToastService.warning('이미 존재하는 폴더 이름입니다');
                        return false;
                    }
                }
                // @ts-ignore
                const seq = (await db.query('SELECT COUNT(*) as count FROM ModFolders'))[0].count as number;
                await db.insert("ModFolders", nanoid(), { path, name, seq: seq + 1 });
                return true;
            } catch (e: any) {
                console.error('folder.create Error', e);
                ToastService.error('항목 생성중 오류 발생', {
                    description: e.message
                });
                return false;
            }
        },
        delete: async (path: string) => {
            return await db.exec(`DELETE FROM ModFolders WHERE path = '${path}'`);
        },
        changeSeq: async (path: string, newSeq: number) => {
            try {
                const rawDb = await db.getDb();
                const currentFolder = (await db.query(
                    `SELECT * from ModFolders WHERE path = '${path}'`
                ))[0] as { id: string; seq: number };
                const currentSeq = currentFolder.seq;
                if (currentSeq === newSeq) return;
                const transaction = rawDb.transaction(() => {
                    if (newSeq > currentSeq) {
                        const shiftStmt = rawDb.prepare("UPDATE ModFolders SET seq = seq - 1 WHERE seq > ? AND seq <= ?");
                        shiftStmt.run(currentSeq, newSeq);
                    } else {
                        const shiftStmt = rawDb.prepare("UPDATE ModFolders SET seq = seq + 1 WHERE seq >= ? AND seq < ?");
                        shiftStmt.run(newSeq, currentSeq);
                    }

                    const updateStmt = rawDb.prepare("UPDATE ModFolders SET seq = ? WHERE id = ?");
                    updateStmt.run(newSeq, currentFolder.id);
                });

                transaction();
                ToastService.success("폴더 순서가 변경되었습니다");
                return true;
            } catch (e: any) {
                console.error('changeSeq Error', e);
                ToastService.error('순서 변경 중 오류가 발생했습니다', {
                    description: e.message
                });
                return false;
            }
        },
        read: async (path: string) => {
            const resp = await this.getDirectChildren(path, 1);
            const filteredResp = resp.filter((item) => !item.ini);
            this.currentFolderPath = path;
            return filteredResp;
        },
        dir: {
            read: async (path: string, options?: ReadDirectoryOptions) => {
                return await FSService.readDirectory(path, options) as FileInfo[];
            },
            disableAll: async (path: string) => {
                try {
                    const children = await FSService.readDirectory(path);
                    for (const folder of children) {
                        const dirName = basename(folder.path);
                        const parentDir = dirname(folder.path);
                        if (!dirName.toLowerCase().startsWith('disabled')) {
                            const newName = `DISABLED ${dirName}`;
                            await FSService.rename(folder.path, join(parentDir, newName));
                        }
                    }

                    ToastService.success(`${(basename(path))}의 모든 모드가 비활성화 되었습니다`);
                    return true;
                } catch (e: any) {
                    ToastService.error("전체 비활성화 중 오류 발생", {
                        description: e.message
                    });
                    return false;
                }
            },

            enableAll: async (path: string) => {
                try {
                    const children = await FSService.readDirectory(path);
                    for (const folder of children) {
                        const dirName = basename(folder.path);
                        const parentDir = dirname(folder.path);
                        if (dirName.toLowerCase().startsWith('disabled')) {
                            const newName = dirName.replace(/^disabled\s+/i, "");
                            await FSService.rename(folder.path, join(parentDir, newName));
                        }
                    }

                    ToastService.success(`${(basename(path))}의 모든 모드가 활성화 되었습니다`);
                    return true;
                } catch (e: any) {
                    ToastService.error("전체 활성화 중 오류 발생", {
                        description: e.message
                    });
                    return false;
                }
            }
        },
    }

    mod = {
        read: async (path: string) => {
            this.currentCharPath = path;
            return this.getDirectChildren(path, 2);
        },

        toggle: async (path: string) => {
            const dirName = basename(path);
            const parentDir = dirname(path);

            if (dirName.toLowerCase().startsWith("disabled")) {
                const newName = dirName.replace(/^disabled\s+/i, "");
                await FSService.rename(path, join(parentDir, newName));
                return true;
            } else {
                const newName = "DISABLED " + dirName;
                await FSService.rename(path, join(parentDir, newName));
                return true;
            }
        }
    }

    ini = {
        parse: async (path: string) => {
            try {
                const content = await FSService.readFile(path, "utf8");
                return ini.parse(content);
            } catch (err: any) {
                console.error(`Error parsing INI file: ${err.message}`);
                return null;
            }
        },

        update: async (path: string, section: string, key: 'key' | 'back', value: string) => {
            try {
                const content = await FSService.readFile(path, "utf8");
                const updatedContent = ini.update(content, section, key, value);
                await FSService.writeFile(path, updatedContent, { encoding: 'utf8' });
                ToastService.success(`토글 키가 변경되었습니다`);
                return true;
            } catch (err: any) {
                console.error(`Error updating INI file: ${err.message}`);
                ToastService.error('토글 수정중 오류 발생', {
                    description: err.message
                });
                return false;
            }
        }
    }

    intx = {
        drop: async (data: string[]) => {
            if (!this.currentCharPath) {
                ToastService.warning('모드가 저장될 폴더를 선택해주세요');
                return false;
            }

            const urls: string[] = [];
            const folders: string[] = [];
            const archives: string[] = [];

            for (const item of data) {
                if (Validator.url(item)) {
                    urls.push(item);
                } else if (Validator.windowsPath(item)) {
                    try {
                        const stats = await FSService.getStat(item);

                        if (stats.isDirectory()) {
                            folders.push(item);
                        } else if (stats.isFile() && /\.(zip|rar|7z)$/i.test(item)) {
                            archives.push(item);
                        }
                    } catch (err: any) {
                        ToastService.error(`경로를 확인할 수 없습니다: ${item}`, err.message);
                        return false;
                    }
                }
            }

            if (urls.length === 0 && folders.length === 0 && archives.length === 0) {
                ToastService.warning('처리할 수 있는 URL 또는 파일 데이터가 없습니다');
                return false;
            }

            try {
                for (const url of urls) {
                    // await NTS.download.enqueue(url, this.currentCharPath);
                }

                for (const folder of folders) {
                    await FSService.copy(folder, this.currentCharPath);
                }

                ToastService.success('작업이 완료되었습니다');
                return true;
            } catch (e: any) {
                ToastService.error('작업중 오류 발생', {
                    description: e.message
                });
                return false;
            }
        }
    }

    async getDirectChildren(path: string, recursive: number) {
        const children = await FSService.readDirectory(path, { recursive });

        const filteredChildren = children.filter(child => child.isDirectory);

        const data = filteredChildren.map(async (child) => {
            let iniData: IniParseResult[] | null = null;

            const iniFiles = child.children?.filter(item =>
                !item.isDirectory &&
                item.name.endsWith('.ini') &&
                !item.name.toLowerCase().startsWith('disabled')
            ) || [];

            const subFolderIniFiles: FileInfo[] = [];
            if (child.children) {
                child.children.forEach(subChild => {
                    if (subChild.isDirectory && subChild.children) {
                        const subIniFiles = subChild.children.filter(item =>
                            !item.isDirectory &&
                            item.name.endsWith('.ini') &&
                            !item.name.toLowerCase().startsWith('disabled')
                        );
                        subFolderIniFiles.push(...subIniFiles);
                    }
                });
            }

            const allIniFiles = [...iniFiles, ...subFolderIniFiles];

            if (allIniFiles.length > 0) {
                try {
                    iniData = await this.ini.parse(allIniFiles[0].path);
                } catch (error) {
                    console.error(`Error parsing INI file at ${allIniFiles[0].path}:`, error);
                }
            }

            let previewPath: string | null = null;
            const previewImage = child.children?.find(item => item.name === 'preview');
            const firstImage = child.children?.find(item => !item.isDirectory &&
                ['jpg', 'jpeg', 'png', 'gif', 'avif', 'avifs', 'webp'].includes(item.name.split('.').pop()!.toLowerCase()));

            if (previewImage) {
                previewPath = previewImage.path;
            } else if (firstImage) {
                previewPath = firstImage.path;
            }

            let preview: { path: string, base64: string | null } | null = null;
            if (previewPath) {
                preview = { path: previewPath, base64: null };
            }

            return {
                path: child.path,
                name: child.name,
                ini: iniData ? {
                    path: allIniFiles[0].path,
                    data: iniData
                } : null,
                preview
            };
        });

        return Promise.all(data);
    }

    clearPath() {
        this.currentCharPath = null;
        this.currentFolderPath = null;
    }

    async fix(path: string) {
        console.log(path);
    }
}

export const ModsService = new ModsServiceClass();