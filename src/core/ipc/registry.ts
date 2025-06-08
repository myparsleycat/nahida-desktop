// src/core/ipc/registry.ts
import { ipcRenderer, ipcMain } from 'electron';

export type ServiceHandler = (...args: any[]) => Promise<any> | any;
export type EventCallback = (state: any) => boolean | void;

export interface ChannelDefinition {
    name: string;
    handler?: ServiceHandler;
    isEvent?: boolean;
    eventName?: string;
}

export class ChannelGroup {
    private channels: Map<string, ChannelDefinition> = new Map();
    private subGroups: Map<string, ChannelGroup> = new Map();
    private parent?: ChannelGroup;
    private path: string[] = [];

    constructor(private name: string, parent?: ChannelGroup) {
        this.parent = parent;
        this.path = parent ? [...parent.path, name] : [name];
    }

    addChannel(name: string, handler?: ServiceHandler, isEvent: boolean = false, eventName?: string): ChannelDefinition {
        const channelName = name;
        const definition = { name: channelName, handler, isEvent, eventName };
        this.channels.set(name, definition);
        return definition;
    }

    addGroup(name: string): ChannelGroup {
        if (!this.subGroups.has(name)) {
            const group = new ChannelGroup(name, this);
            this.subGroups.set(name, group);
            return group;
        }
        return this.subGroups.get(name)!;
    }

    getFullChannelId(name: string): string {
        return [...this.path, name].join('.');
    }

    registerAllChannels(ipcMainInstance: typeof ipcMain): void {
        this.channels.forEach((definition, name) => {
            const fullChannelId = this.getFullChannelId(name);
            if (definition.handler && !definition.isEvent) {
                ipcMainInstance.handle(fullChannelId,
                    (_event: any, ...args: any[]) => definition.handler!(...args));
            }
        });

        this.subGroups.forEach(group => {
            group.registerAllChannels(ipcMainInstance);
        });
    }

    createApiMethods(ipcRendererInstance: typeof ipcRenderer): any {
        const methods: Record<string, any> = {};

        this.channels.forEach((definition, name) => {
            const fullChannelId = this.getFullChannelId(name);

            if (definition.isEvent) {
                const eventChannelId = definition.eventName || fullChannelId;

                if (this.name === 'auth' && name === 'authStateChanged') {
                    methods['onAuthStateChanged'] = (callback: EventCallback) => {
                        ipcRendererInstance.on(eventChannelId, (_event: any, state: any) => callback(state));
                        return () => ipcRendererInstance.removeListener(eventChannelId, callback);
                    };
                } else {
                    methods[name] = (callback: EventCallback) => {
                        ipcRendererInstance.on(eventChannelId, (_event: any, state: any) => callback(state));
                        return () => ipcRendererInstance.removeListener(eventChannelId, callback);
                    };
                }
            } else {
                methods[name] = (...args: any[]) => ipcRendererInstance.invoke(fullChannelId, ...args);
            }
        });

        this.subGroups.forEach((group, groupName) => {
            methods[groupName] = group.createApiMethods(ipcRendererInstance);
        });

        return methods;
    }

    generateChannelConstants(): Record<string, any> {
        const constants: Record<string, any> = {};

        this.channels.forEach((definition, name) => {
            if (definition.isEvent && definition.eventName) {
                constants[definition.eventName.replace(/-/g, '_').toUpperCase()] = definition.eventName;
            } else {
                constants[name.toUpperCase()] = this.getFullChannelId(name);
            }

            if (this.name === 'auth' && name === 'authStateChanged') {
                constants['AUTH_STATE_CHANGED'] = 'auth-state-changed';
            }
        });

        this.subGroups.forEach((group, groupName) => {
            constants[groupName.toUpperCase()] = group.generateChannelConstants();
        });

        if (this.name === 'window') {
            constants['CONTROL'] = 'window-control';
        }

        return constants;
    }

    clearChannels(): void {
        this.channels.clear();
        this.subGroups.clear();
    }

    getSubGroup(name: string): ChannelGroup | undefined {
        return this.subGroups.get(name);
    }

    setChannelHandler(channelName: string, handler: ServiceHandler): void {
        const channel = this.channels.get(channelName);
        if (channel) {
            channel.handler = handler;
        } else {
            console.warn(`Channel not found: ${channelName}`);
        }
    }
}

export class ServiceRegistry {
    private rootGroup: ChannelGroup;

    constructor() {
        this.rootGroup = new ChannelGroup('root');
    }

    getRootGroup(): ChannelGroup {
        return this.rootGroup;
    }

    injectHandlers(groupPath: string, handlers: Record<string, ServiceHandler>): void {
        const pathParts = groupPath.split('.');
        let currentGroup = this.rootGroup;

        for (const part of pathParts) {
            const subGroup = currentGroup.getSubGroup(part);
            if (subGroup) {
                currentGroup = subGroup;
            } else {
                console.warn(`Group not found: ${groupPath}`);
                return;
            }
        }

        Object.entries(handlers).forEach(([channelName, handler]) => {
            currentGroup.setChannelHandler(channelName, handler);
        });
    }

    registerServices(ipcMainInstance: typeof ipcMain): void {
        this.rootGroup.registerAllChannels(ipcMainInstance);

        ipcMainInstance.on('window-control', (_event, _command) => {
            // window-control
        });
    }

    createApiInterface(ipcRendererInstance: typeof ipcRenderer): Record<string, any> {
        const api: Record<string, any> = this.rootGroup.createApiMethods(ipcRendererInstance);

        api.window = {
            minimize: () => ipcRendererInstance.send('window-control', 'minimize'),
            maximize: () => ipcRendererInstance.send('window-control', 'maximize'),
            close: () => ipcRendererInstance.send('window-control', 'close')
        };

        return api;
    }

    getChannelConstants(): Record<string, any> {
        return this.rootGroup.generateChannelConstants();
    }
}