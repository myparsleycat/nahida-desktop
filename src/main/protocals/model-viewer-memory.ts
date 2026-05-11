import {
    handleModelViewerMemoryProtocol,
    MODEL_VIEWER_MEMORY_PROTOCOL,
} from "@main/services/protocol/model-viewer-memory";
import { protocol } from "electron";

export function registerModelViewerMemoryProtocal() {
    protocol.handle(MODEL_VIEWER_MEMORY_PROTOCOL, handleModelViewerMemoryProtocol);
}
