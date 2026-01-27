import { IpcHandlers, IpcEvents } from "./types";
import { IPC_HANDLER_CHANNELS, IPC_EVENT_CHANNELS } from "./ipc-spec";

type AssertEmpty<T extends never> = T;

type _CheckHandlerKeys_ = AssertEmpty<
    | Exclude<keyof IpcHandlers, (typeof IPC_HANDLER_CHANNELS)[number]>
    | Exclude<(typeof IPC_HANDLER_CHANNELS)[number], keyof IpcHandlers>
>;

type _CheckEventKeys_ = AssertEmpty<
    | Exclude<keyof IpcEvents, (typeof IPC_EVENT_CHANNELS)[number]>
    | Exclude<(typeof IPC_EVENT_CHANNELS)[number], keyof IpcEvents>
>;
