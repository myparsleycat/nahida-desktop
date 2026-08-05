import assert from "node:assert/strict";

import { afterEach, describe, it, vi } from "vitest";
import { z } from "zod";

import { createLlmJsonCompletion, type LlmConfig } from "./llm";

const schema = z.object({ result: z.string() });

describe("LLM protocol adapters", () => {
    afterEach(() => {
        vi.unstubAllGlobals();
    });

    it.each([
        ["openai-response", "/responses", "authorization"],
        ["openai-compatible", "/chat/completions", "authorization"],
        ["anthropic", "/messages", "x-api-key"],
    ] as const)("uses the %s endpoint contract", async (protocol, path, authHeader) => {
        const requests: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
        const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
            requests.push({ input, init });
            return new Response(JSON.stringify({ error: { message: "test adapter request" } }), {
                status: 500,
                headers: { "content-type": "application/json" },
            });
        });
        vi.stubGlobal("fetch", fetchMock);

        const config = {
            protocol,
            endpoint: "https://example.com/v1",
            model: "test-model",
            reasoning: "auto",
            apiKey: "test-key",
        } satisfies LlmConfig;

        await assert.rejects(
            createLlmJsonCompletion({
                system: "Return JSON.",
                userText: "Return a result.",
                schema,
                config,
                maxRetries: 0,
            }),
        );

        assert.equal(fetchMock.mock.calls.length, 1);
        const request = requests[0];
        assert.ok(request);
        const requestUrl =
            typeof request.input === "string"
                ? request.input
                : request.input instanceof URL
                  ? request.input.toString()
                  : request.input.url;
        assert.equal(requestUrl, `https://example.com/v1${path}`);
        const headers = new Headers(request.init?.headers);
        assert.ok(headers.has(authHeader));
    });
});
