import { describe, expect, it } from "vitest";

import {
    acceptAgentStreamEvent,
    hasPendingAgentApproval,
    isAgentApprovalEvent,
    updateAgentLiveEntries,
} from "./agent-stream";

describe("acceptAgentStreamEvent", () => {
    it("rejects duplicate, stale, and out-of-order events per run", () => {
        const latest = new Map<string, number>();
        expect(acceptAgentStreamEvent(latest, { runId: "a", sequence: 2 })).toBe(true);
        expect(acceptAgentStreamEvent(latest, { runId: "a", sequence: 2 })).toBe(false);
        expect(acceptAgentStreamEvent(latest, { runId: "a", sequence: 1 })).toBe(false);
        expect(acceptAgentStreamEvent(latest, { runId: "a", sequence: 3 })).toBe(true);
        expect(acceptAgentStreamEvent(latest, { runId: "b", sequence: 1 })).toBe(true);
    });
});

describe("agent approval stream state", () => {
    it("recognizes approval events", () => {
        expect(isAgentApprovalEvent("approval-requested")).toBe(true);
        expect(isAgentApprovalEvent("approval-updated")).toBe(true);
        expect(isAgentApprovalEvent("tool-end")).toBe(false);
    });

    it("only blocks input for pending approvals", () => {
        expect(hasPendingAgentApproval(undefined)).toBe(false);
        expect(hasPendingAgentApproval([{ status: "completed" }])).toBe(false);
        expect(hasPendingAgentApproval([{ status: "failed" }, { status: "pending" }])).toBe(true);
        expect(hasPendingAgentApproval([{ status: "executing" }])).toBe(true);
    });
});

describe("updateAgentLiveEntries", () => {
    it("preserves assistant and tool event order across model rounds", () => {
        const base = { sessionId: "session", runId: "run" };
        let entries = updateAgentLiveEntries([], {
            ...base,
            sequence: 1,
            type: "assistant-delta",
            payload: { delta: "조사 시작" },
        });
        entries = updateAgentLiveEntries(entries, {
            ...base,
            sequence: 2,
            type: "tool-start",
            payload: { id: "tool-1", name: "read_file" },
        });
        entries = updateAgentLiveEntries(entries, {
            ...base,
            sequence: 3,
            type: "tool-end",
            payload: { toolCallId: "tool-1", toolName: "read_file", result: { ok: true } },
        });
        entries = updateAgentLiveEntries(entries, {
            ...base,
            sequence: 4,
            type: "reasoning-delta",
            payload: { delta: "결과 확인" },
        });
        entries = updateAgentLiveEntries(entries, {
            ...base,
            sequence: 5,
            type: "assistant-delta",
            payload: { delta: "완료했습니다" },
        });

        expect(entries).toHaveLength(4);
        expect(entries.map((entry) => entry.type)).toEqual([
            "message/assistant",
            "tool/end",
            "message/assistant",
            "message/assistant",
        ]);
        expect(entries[0]).toMatchObject({ text: "조사 시작", streaming: false });
        expect(entries[1]).toMatchObject({ toolCallId: "tool-1", sequence: 2 });
        expect(entries[2]).toMatchObject({
            reasoning: "결과 확인",
            reasoningStreaming: false,
        });
        expect(entries[3]).toMatchObject({
            text: "완료했습니다",
            streaming: true,
        });
    });

    it("keeps text before reasoning when their deltas arrive in that order", () => {
        const base = { sessionId: "session", runId: "run" };
        let entries = updateAgentLiveEntries([], {
            ...base,
            sequence: 1,
            type: "assistant-delta",
            payload: { delta: "먼저 알림" },
        });
        entries = updateAgentLiveEntries(entries, {
            ...base,
            sequence: 2,
            type: "reasoning-delta",
            payload: { delta: "이후 추론" },
        });

        expect(entries).toHaveLength(2);
        expect(entries[0]).toMatchObject({ text: "먼저 알림", streaming: false });
        expect(entries[1]).toMatchObject({ reasoning: "이후 추론", reasoningStreaming: true });
    });
});
