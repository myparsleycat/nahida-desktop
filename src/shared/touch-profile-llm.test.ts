import assert from "node:assert/strict";

import { describe, it } from "vitest";

import {
    DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT,
    DEFAULT_TOUCH_PROFILE_LLM_MODEL,
    normalizeTouchProfileLlmEndpoint,
    normalizeTouchProfileLlmSettings,
} from "./touch-profile-llm";

describe("touch profile LLM settings", () => {
    it("normalizes a valid endpoint without trailing slashes", () => {
        assert.equal(
            normalizeTouchProfileLlmEndpoint("https://example.com/v1///"),
            "https://example.com/v1",
        );
    });

    it("falls back when an endpoint is invalid", () => {
        assert.equal(
            normalizeTouchProfileLlmEndpoint("file:///secret"),
            DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT,
        );
        assert.equal(
            normalizeTouchProfileLlmEndpoint("https://example.com/v1?key=secret"),
            DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT,
        );
    });

    it("normalizes unknown values to safe defaults", () => {
        assert.deepEqual(
            normalizeTouchProfileLlmSettings({ protocol: "unknown" as never, model: " " }),
            {
                protocol: "openai-response",
                endpoint: DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT,
                model: DEFAULT_TOUCH_PROFILE_LLM_MODEL,
                reasoning: "auto",
            },
        );
    });
});
