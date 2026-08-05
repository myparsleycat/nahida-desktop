export type TouchProfileLlmProtocol = "openai-response" | "openai-compatible" | "anthropic";

export type TouchProfileLlmReasoning = "auto" | "low" | "medium" | "high";

export type TouchProfileLlmSettings = {
    protocol: TouchProfileLlmProtocol;
    endpoint: string;
    model: string;
    reasoning: TouchProfileLlmReasoning;
};

export const TOUCH_PROFILE_LLM_PROTOCOLS = [
    "openai-response",
    "openai-compatible",
    "anthropic",
] as const satisfies readonly TouchProfileLlmProtocol[];

export const TOUCH_PROFILE_LLM_REASONING_LEVELS = [
    "auto",
    "low",
    "medium",
    "high",
] as const satisfies readonly TouchProfileLlmReasoning[];

export const DEFAULT_TOUCH_PROFILE_LLM_PROTOCOL: TouchProfileLlmProtocol = "openai-response";
export const DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT = "https://api.openai.com/v1";
export const DEFAULT_TOUCH_PROFILE_LLM_MODEL = "openai/gpt-5.6-luna";
export const DEFAULT_TOUCH_PROFILE_LLM_REASONING: TouchProfileLlmReasoning = "auto";

export function isTouchProfileLlmProtocol(value: unknown): value is TouchProfileLlmProtocol {
    return TOUCH_PROFILE_LLM_PROTOCOLS.includes(value as TouchProfileLlmProtocol);
}

export function isTouchProfileLlmReasoning(value: unknown): value is TouchProfileLlmReasoning {
    return TOUCH_PROFILE_LLM_REASONING_LEVELS.includes(value as TouchProfileLlmReasoning);
}

export function normalizeTouchProfileLlmEndpoint(value: string) {
    const endpoint = value.trim().replace(/\/+$/, "");
    if (!endpoint) return DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT;

    let url: URL;
    try {
        url = new URL(endpoint);
    } catch {
        return DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT;
    }

    if (url.protocol !== "http:" && url.protocol !== "https:") {
        return DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT;
    }

    if (url.search || url.hash) {
        return DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT;
    }

    return url.toString().replace(/\/+$/, "");
}

export function normalizeTouchProfileLlmSettings(
    input: Partial<TouchProfileLlmSettings> | null | undefined,
): TouchProfileLlmSettings {
    return {
        protocol: isTouchProfileLlmProtocol(input?.protocol)
            ? input.protocol
            : DEFAULT_TOUCH_PROFILE_LLM_PROTOCOL,
        endpoint: normalizeTouchProfileLlmEndpoint(
            typeof input?.endpoint === "string"
                ? input.endpoint
                : DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT,
        ),
        model:
            typeof input?.model === "string" && input.model.trim()
                ? input.model.trim()
                : DEFAULT_TOUCH_PROFILE_LLM_MODEL,
        reasoning: isTouchProfileLlmReasoning(input?.reasoning)
            ? input.reasoning
            : DEFAULT_TOUCH_PROFILE_LLM_REASONING,
    };
}
