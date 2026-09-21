import type { AgentChatEntry, AgentContextUsage, AgentImage } from "@bindings/agent/models";

export interface SequencedAgentEvent {
    runId: string;
    sequence: number;
}

export interface AgentStreamEvent extends SequencedAgentEvent {
    sessionId: string;
    type: string;
    payload?: {
        delta?: string;
        status?: string;
        message?: string;
        error?: string;
        title?: string;
        toolName?: string;
        toolCallId?: string;
        name?: string;
        id?: string;
        result?: unknown;
        images?: AgentImage[];
        changedFiles?: string[];
        contextUsage?: AgentContextUsage;
    };
}

export interface LiveAgentChatEntry extends AgentChatEntry {
    streaming?: boolean;
    reasoningStreaming?: boolean;
}

export interface AgentApprovalState {
    status: string;
}

export function acceptAgentStreamEvent(
    latestSequences: Map<string, number>,
    event: SequencedAgentEvent,
) {
    const previous = latestSequences.get(event.runId) ?? 0;
    if (event.sequence <= previous) return false;
    latestSequences.set(event.runId, event.sequence);
    return true;
}

export function updateAgentLiveEntries(
    entries: LiveAgentChatEntry[],
    event: AgentStreamEvent,
): LiveAgentChatEntry[] {
    if (event.type === "assistant-delta" || event.type === "reasoning-delta") {
        return appendAssistantDelta(entries, event);
    }
    if (event.type === "tool-start") {
        return [
            ...settleLastAssistant(entries),
            {
                sequence: event.sequence,
                turnId: event.runId,
                type: "tool/start",
                toolName: event.payload?.toolName ?? event.payload?.name,
                toolCallId: event.payload?.toolCallId ?? event.payload?.id,
                createdAt: new Date().toISOString(),
            },
        ];
    }
    if (event.type !== "tool-end") return entries;

    const toolCallId = event.payload?.toolCallId;
    const toolIndex = toolCallId
        ? entries.findIndex((entry) => entry.toolCallId === toolCallId)
        : -1;
    const completedTool: LiveAgentChatEntry = {
        sequence: event.sequence,
        turnId: event.runId,
        type: "tool/end",
        toolName: event.payload?.toolName,
        toolCallId,
        result: event.payload?.result,
        images: event.payload?.images,
        changedFiles: event.payload?.changedFiles,
        error: event.payload?.error,
        createdAt: new Date().toISOString(),
    };
    if (toolIndex < 0) return [...entries, completedTool];

    return entries.map((entry, index) =>
        index === toolIndex ? { ...completedTool, sequence: entry.sequence } : entry,
    );
}

function appendAssistantDelta(
    entries: LiveAgentChatEntry[],
    event: AgentStreamEvent,
): LiveAgentChatEntry[] {
    const delta = event.payload?.delta ?? "";
    const last = entries.at(-1);
    const continuing =
        last?.role === "assistant" &&
        last.turnId === event.runId &&
        ((event.type === "assistant-delta" && last.streaming) ||
            (event.type === "reasoning-delta" && last.reasoningStreaming));
    const assistant = continuing
        ? last
        : {
              sequence: event.sequence,
              turnId: event.runId,
              type: "message/assistant",
              role: "assistant",
              createdAt: new Date().toISOString(),
          };
    const next =
        event.type === "assistant-delta"
            ? {
                  ...assistant,
                  text: (assistant.text ?? "") + delta,
                  streaming: true,
                  reasoningStreaming: false,
              }
            : {
                  ...assistant,
                  reasoning: (assistant.reasoning ?? "") + delta,
                  reasoningStreaming: true,
              };

    return continuing ? [...entries.slice(0, -1), next] : [...settleLastAssistant(entries), next];
}

function settleLastAssistant(entries: LiveAgentChatEntry[]) {
    const last = entries.at(-1);
    if (last?.role !== "assistant") return entries;

    return [...entries.slice(0, -1), { ...last, streaming: false, reasoningStreaming: false }];
}

export function isAgentApprovalEvent(type: string) {
    return type === "approval-requested" || type === "approval-updated";
}

export function hasPendingAgentApproval(approvals: AgentApprovalState[] | null | undefined) {
    return (
        approvals?.some(
            (approval) => approval.status === "pending" || approval.status === "executing",
        ) ?? false
    );
}
