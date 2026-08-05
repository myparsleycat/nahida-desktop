import { createAnthropic } from "@ai-sdk/anthropic";
import { createOpenAI } from "@ai-sdk/openai";
import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import {
    DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT,
    DEFAULT_TOUCH_PROFILE_LLM_MODEL,
    DEFAULT_TOUCH_PROFILE_LLM_PROTOCOL,
    DEFAULT_TOUCH_PROFILE_LLM_REASONING,
    type TouchProfileLlmSettings,
} from "@shared/touch-profile-llm";
import { generateText, Output, type LanguageModel } from "ai";
import { z } from "zod";

export const LLM_MODEL = DEFAULT_TOUCH_PROFILE_LLM_MODEL;
export const LLM_BASE_URL = DEFAULT_TOUCH_PROFILE_LLM_ENDPOINT;
export const LLM_API_KEY_SETTING_KEY = "tools_touch_profile_llm_api_key";
export const LLM_MAX_OUTPUT_TOKENS = 16_384;

const OPENAI_COMPATIBLE_PROVIDER_NAME = "touchProfileOpenAiCompatible";

export type LlmConfig = TouchProfileLlmSettings & {
    apiKey: string;
};

type LlmProviderOptions = NonNullable<Parameters<typeof generateText>[0]["providerOptions"]>;

export const DEFAULT_LLM_CONFIG: LlmConfig = {
    protocol: DEFAULT_TOUCH_PROFILE_LLM_PROTOCOL,
    endpoint: LLM_BASE_URL,
    model: LLM_MODEL,
    reasoning: DEFAULT_TOUCH_PROFILE_LLM_REASONING,
    apiKey: resolveLlmApiKey({
        protocol: DEFAULT_TOUCH_PROFILE_LLM_PROTOCOL,
        endpoint: LLM_BASE_URL,
        model: LLM_MODEL,
        reasoning: DEFAULT_TOUCH_PROFILE_LLM_REASONING,
    }),
};

export function resolveLlmApiKey(settings: TouchProfileLlmSettings, storedApiKey?: string) {
    const saved = storedApiKey?.trim();
    if (saved) return saved;

    const environmentKey =
        settings.protocol === "anthropic"
            ? process.env.ANTHROPIC_API_KEY?.trim() || process.env.NAHIDA_LLM_API_KEY?.trim()
            : process.env.NAHIDA_LLM_API_KEY?.trim() || process.env.OPENAI_API_KEY?.trim();
    if (environmentKey) return environmentKey;

    return "";
}

export async function createLlmJsonCompletion<T>(options: {
    system: string;
    userText: string;
    images?: Array<{
        mimeType: string;
        bytes: Buffer | Uint8Array;
        detail?: "auto" | "low" | "high";
    }>;
    schema: z.ZodType<T>;
    config?: LlmConfig;
    model?: string;
    maxRetries?: number;
}): Promise<{ data: T; rawText: string; model: string }> {
    const config = options.config ?? DEFAULT_LLM_CONFIG;
    const maxRetries = options.maxRetries ?? 2;
    let lastError: unknown;
    let repairHint = "";

    for (let attempt = 0; attempt <= maxRetries; attempt++) {
        try {
            const result = await generateText({
                model: createLlmModel({ ...config, model: options.model ?? config.model }),
                system: options.system,
                messages: [
                    {
                        role: "user",
                        content: [
                            {
                                type: "text",
                                text: repairHint
                                    ? `${options.userText}\n\nPrevious response was invalid. Fix and return JSON only.\n${repairHint}`
                                    : options.userText,
                            },
                            ...(options.images ?? []).map((image) => ({
                                type: "file" as const,
                                data: image.bytes,
                                mediaType: image.mimeType,
                            })),
                        ],
                    },
                ],
                output: Output.object({ schema: options.schema }),
                maxOutputTokens: LLM_MAX_OUTPUT_TOKENS,
                maxRetries: 0,
                providerOptions: createProviderOptions(config),
            });

            return {
                data: result.output,
                rawText: JSON.stringify(result.output),
                model: result.response.modelId || config.model,
            };
        } catch (error) {
            lastError = error;
            repairHint = error instanceof Error ? error.message : String(error);
        }
    }

    throw lastError instanceof Error
        ? lastError
        : new Error("LLM returned invalid JSON after retries");
}

function createLlmModel(config: LlmConfig): LanguageModel {
    if (config.protocol === "openai-response") {
        return createOpenAI({
            apiKey: config.apiKey,
            baseURL: config.endpoint,
        }).responses(config.model);
    }

    if (config.protocol === "openai-compatible") {
        return createOpenAICompatible({
            name: OPENAI_COMPATIBLE_PROVIDER_NAME,
            apiKey: config.apiKey,
            baseURL: config.endpoint,
            supportsStructuredOutputs: true,
        })(config.model);
    }

    return createAnthropic({
        apiKey: config.apiKey,
        baseURL: config.endpoint,
    })(config.model);
}

function createProviderOptions(config: LlmConfig): LlmProviderOptions | undefined {
    if (config.protocol === "openai-response") {
        return {
            openai: {
                strictJsonSchema: false,
                ...(config.reasoning === "auto" ? {} : { reasoningEffort: config.reasoning }),
            },
        } as LlmProviderOptions;
    }

    if (config.protocol === "openai-compatible") {
        return {
            [OPENAI_COMPATIBLE_PROVIDER_NAME]: {
                strictJsonSchema: false,
                ...(config.reasoning === "auto" ? {} : { reasoningEffort: config.reasoning }),
            },
        } as LlmProviderOptions;
    }

    if (config.reasoning === "auto") return undefined;

    return {
        anthropic: {
            thinking: { type: "adaptive" },
            effort: config.reasoning,
        },
    } as LlmProviderOptions;
}
