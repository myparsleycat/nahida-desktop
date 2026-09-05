import { describe, expect, it } from "vitest";

import { isSilentDiagnostic, serializeDiagnostic } from "./diagnostic";

describe("diagnostics", () => {
    it("preserves non-enumerable error details and nested causes", () => {
        const error = new Error("save failed", { cause: new Error("disk full") });
        const result = serializeDiagnostic(error);
        expect(result).toMatchObject({
            name: "Error",
            message: "save failed",
            cause: { message: "disk full" },
        });
        expect(JSON.stringify(result)).toContain("stack");
    });

    it("bounds cycles and removes credentials before crossing the bridge", () => {
        const value: Record<string, unknown> = {
            url: "https://user:pass@example.com/path?token=secret#state",
            token: "secret",
        };
        value.self = value;
        const encoded = JSON.stringify(serializeDiagnostic(value));
        expect(encoded).toContain("[circular]");
        expect(encoded).not.toContain("secret");
        expect(encoded).not.toContain("user:pass");
        expect(encoded).toContain("https://example.com/path");
    });

    it("only suppresses service envelopes and intentional cancellation", () => {
        const service = new Error("failed", { cause: "SERVER_ERROR" });
        service.name = "RuntimeError";
        expect(isSilentDiagnostic(service)).toBe(true);
        expect(isSilentDiagnostic(new Error("failed to send binding call"))).toBe(false);
        expect(isSilentDiagnostic(new DOMException("cancelled", "AbortError"))).toBe(true);
    });

    it("handles aggregate errors and hostile objects without throwing", () => {
        expect(
            serializeDiagnostic(
                new AggregateError([new Error("first"), new Error("second")], "both"),
            ),
        ).toMatchObject({ errors: [{ message: "first" }, { message: "second" }] });
        const value = Object.defineProperty({}, "bad", {
            enumerable: true,
            get() {
                throw new Error("getter");
            },
        });
        expect(() => serializeDiagnostic(value)).not.toThrow();
        expect(
            JSON.stringify(serializeDiagnostic(new Error("x".repeat(10000))))!.length,
        ).toBeLessThan(22000);
    });
    it("redacts secrets from string errors before console fallback and marks omitted data", () => {
        expect(
            JSON.stringify(
                serializeDiagnostic(new Error("token=private-token password=private-password")),
            ),
        ).not.toContain("private-");
        expect(
            JSON.stringify(serializeDiagnostic(Array.from({ length: 40 }, (_, index) => index))),
        ).toContain("8 omitted");
    });
});
