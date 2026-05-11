import path from "node:path";
import {
    generateToggleViewerArtifact,
    type GeneratedToggleViewerArtifact,
} from "@main/lib/toggle-viewer-core";
import { findFiles } from "@native/fs";
import fse from "fs-extra";

interface WorkerRequestBase {
    reqId: string;
}

interface ScanModsPathsRequest extends WorkerRequestBase {
    type: "scanModsPaths";
    modsPaths: string[];
    hotkey: string;
}

interface ProcessIniPathsRequest extends WorkerRequestBase {
    type: "processIniPaths";
    iniPaths: string[];
    hotkey: string;
}

interface AbortRequest extends WorkerRequestBase {
    type: "abort";
}

type WorkerRequest = ScanModsPathsRequest | ProcessIniPathsRequest | AbortRequest;

interface WorkerSuccessResponse {
    type: "success";
    reqId: string;
    artifacts: GeneratedToggleViewerArtifact[];
    seenTargetIniPaths?: string[];
    invalidIniPaths?: string[];
    logs: string[];
}

interface WorkerErrorResponse {
    type: "error";
    reqId: string;
    error: string;
}

const abortedJobs = new Set<string>();

process.parentPort?.on("message", async (event) => {
    const request = event.data as WorkerRequest;

    if (request.type === "abort") {
        abortedJobs.add(request.reqId);
        return;
    }

    try {
        if (request.type === "scanModsPaths") {
            const response = await handleScanModsPaths(request);
            postMessage(response);
            return;
        }

        if (request.type === "processIniPaths") {
            const response = await handleProcessIniPaths(request);
            postMessage(response);
        }
    } catch (error) {
        postMessage({
            type: "error",
            reqId: request.reqId,
            error: error instanceof Error ? error.message : String(error),
        } satisfies WorkerErrorResponse);
    } finally {
        abortedJobs.delete(request.reqId);
    }
});

async function handleScanModsPaths(request: ScanModsPathsRequest): Promise<WorkerSuccessResponse> {
    const logs: string[] = [];
    const artifacts: GeneratedToggleViewerArtifact[] = [];
    const seenTargetIniPaths = new Set<string>();

    for (const modsPath of request.modsPaths) {
        throwIfAborted(request.reqId);

        if (!(await fse.pathExists(modsPath))) {
            continue;
        }

        const iniPaths = (
            await findFiles([modsPath], [".ini"], ["toggle-viewer.ini", "disabled*"])
        ).map((candidate) => path.resolve(candidate));
        const result = await processIniPaths(request.reqId, iniPaths, request.hotkey, logs);
        for (const artifact of result.artifacts) {
            artifacts.push(artifact);
            seenTargetIniPaths.add(artifact.targetIniPath);
        }
    }

    return {
        type: "success",
        reqId: request.reqId,
        artifacts,
        seenTargetIniPaths: [...seenTargetIniPaths],
        logs,
    };
}

async function handleProcessIniPaths(
    request: ProcessIniPathsRequest,
): Promise<WorkerSuccessResponse> {
    const logs: string[] = [];
    const result = await processIniPaths(request.reqId, request.iniPaths, request.hotkey, logs);

    return {
        type: "success",
        reqId: request.reqId,
        artifacts: result.artifacts,
        invalidIniPaths: result.invalidIniPaths,
        logs,
    };
}

async function processIniPaths(
    reqId: string,
    iniPaths: string[],
    hotkey: string,
    logs: string[],
): Promise<{
    artifacts: GeneratedToggleViewerArtifact[];
    invalidIniPaths: string[];
}> {
    const artifacts: GeneratedToggleViewerArtifact[] = [];
    const invalidIniPaths: string[] = [];

    for (const rawIniPath of iniPaths) {
        throwIfAborted(reqId);

        const iniPath = path.resolve(rawIniPath);
        if (!(await fse.pathExists(iniPath))) {
            invalidIniPaths.push(iniPath);
            continue;
        }

        try {
            const content = await fse.readFile(iniPath, "utf-8");
            const artifact = generateToggleViewerArtifact(iniPath, content, hotkey);
            if (artifact) {
                artifacts.push(artifact);
            } else {
                invalidIniPaths.push(iniPath);
            }
        } catch (error) {
            logs.push(`Failed to read ini ${iniPath}: ${error}`);
            invalidIniPaths.push(iniPath);
        }
    }

    return { artifacts, invalidIniPaths };
}

function throwIfAborted(reqId: string) {
    if (abortedJobs.has(reqId)) {
        throw new Error("Aborted");
    }
}

function postMessage(response: WorkerSuccessResponse | WorkerErrorResponse) {
    process.parentPort?.postMessage(response);
}
