type IniExpressionValue = number | string | boolean;

type IniExpressionToken =
    | { type: "number"; value: number }
    | { type: "string"; value: string }
    | { type: "boolean"; value: boolean }
    | { type: "identifier"; value: string }
    | { type: "operator"; value: string }
    | { type: "paren"; value: "(" | ")" };

type IniExpressionState = {
    tokens: IniExpressionToken[];
    index: number;
    variables: Map<string, number | string>;
    runtimeValues?: Record<string, number | string>;
};

export function evaluateIniCondition(
    expression: string,
    variables: Map<string, number | string>,
    normalizeKey: (value: string) => string,
    runtimeValues?: Record<string, number | string>,
): boolean {
    try {
        return Boolean(evaluateIniExpression(expression, variables, normalizeKey, runtimeValues));
    } catch {
        return false;
    }
}

export function evaluateIniNumericExpression(
    expression: string,
    variables: Map<string, number | string>,
    normalizeKey: (value: string) => string,
    runtimeValues?: Record<string, number | string>,
): number | null {
    try {
        const value = evaluateIniExpression(expression, variables, normalizeKey, runtimeValues);
        return typeof value === "number" && Number.isFinite(value) ? value : null;
    } catch {
        return null;
    }
}

function evaluateIniExpression(
    expression: string,
    variables: Map<string, number | string>,
    normalizeKey: (value: string) => string,
    runtimeValues?: Record<string, number | string>,
): IniExpressionValue {
    const tokens = tokenizeIniExpression(expression);
    const state: IniExpressionState = { tokens, index: 0, variables, runtimeValues };
    const value = parseIniLogicalOr(state, normalizeKey);
    if (state.index !== tokens.length) {
        throw new Error(`Unexpected token in expression: ${expression}`);
    }
    return value;
}

function tokenizeIniExpression(expression: string): IniExpressionToken[] {
    const tokens: IniExpressionToken[] = [];

    for (let index = 0; index < expression.length; ) {
        const char = expression[index];
        if (/\s/.test(char)) {
            index++;
            continue;
        }

        const numberMatch = expression.slice(index).match(/^\d+(?:\.\d+)?/);
        if (numberMatch) {
            tokens.push({ type: "number", value: Number(numberMatch[0]) });
            index += numberMatch[0].length;
            continue;
        }

        if (char === '"' || char === "'") {
            let value = "";
            let closed = false;
            let cursor = index + 1;
            while (cursor < expression.length) {
                const current = expression[cursor];
                if (current === "\\") {
                    const next = expression[cursor + 1];
                    if (!next) break;
                    value += next;
                    cursor += 2;
                    continue;
                }
                if (current === char) {
                    tokens.push({ type: "string", value });
                    index = cursor + 1;
                    closed = true;
                    break;
                }
                value += current;
                cursor++;
            }
            if (!closed) {
                throw new Error(`Unterminated string literal in expression: ${expression}`);
            }
            continue;
        }

        const threeCharOperator = expression.slice(index, index + 3);
        if (["===", "!=="].includes(threeCharOperator)) {
            tokens.push({ type: "operator", value: threeCharOperator });
            index += 3;
            continue;
        }

        const twoCharOperator = expression.slice(index, index + 2);
        if (["&&", "||", "==", "!=", "<=", ">=", "//"].includes(twoCharOperator)) {
            tokens.push({ type: "operator", value: twoCharOperator });
            index += 2;
            continue;
        }

        if (["(", ")"].includes(char)) {
            tokens.push({ type: "paren", value: char as "(" | ")" });
            index++;
            continue;
        }

        if (["+", "-", "*", "/", "%", "!", "<", ">", "="].includes(char)) {
            tokens.push({ type: "operator", value: char });
            index++;
            continue;
        }

        const identifierMatch = expression.slice(index).match(/^\$?[A-Za-z_\\][A-Za-z0-9_\\.\\]*/);
        if (identifierMatch) {
            const value = identifierMatch[0];
            const lower = value.toLowerCase();
            if (lower === "true" || lower === "false") {
                tokens.push({ type: "boolean", value: lower === "true" });
            } else {
                tokens.push({ type: "identifier", value });
            }
            index += value.length;
            continue;
        }

        throw new Error(`Unsupported token "${char}" in expression: ${expression}`);
    }

    return tokens;
}

function parseIniLogicalOr(
    state: IniExpressionState,
    normalizeKey: (value: string) => string,
): IniExpressionValue {
    let left = parseIniLogicalAnd(state, normalizeKey);
    while (matchIniOperator(state, "||")) {
        left = Boolean(left) || Boolean(parseIniLogicalAnd(state, normalizeKey));
    }
    return left;
}

function parseIniLogicalAnd(
    state: IniExpressionState,
    normalizeKey: (value: string) => string,
): IniExpressionValue {
    let left = parseIniEquality(state, normalizeKey);
    while (matchIniOperator(state, "&&")) {
        left = Boolean(left) && Boolean(parseIniEquality(state, normalizeKey));
    }
    return left;
}

function parseIniEquality(
    state: IniExpressionState,
    normalizeKey: (value: string) => string,
): IniExpressionValue {
    let left = parseIniComparison(state, normalizeKey);
    while (true) {
        if (
            matchIniOperator(state, "===") ||
            matchIniOperator(state, "==") ||
            matchIniOperator(state, "=")
        ) {
            left = compareIniEquality(left, parseIniComparison(state, normalizeKey));
            continue;
        }
        if (matchIniOperator(state, "!==") || matchIniOperator(state, "!=")) {
            left = !compareIniEquality(left, parseIniComparison(state, normalizeKey));
            continue;
        }
        return left;
    }
}

function parseIniComparison(
    state: IniExpressionState,
    normalizeKey: (value: string) => string,
): IniExpressionValue {
    let left = parseIniAdditive(state, normalizeKey);
    while (true) {
        if (matchIniOperator(state, "<")) {
            left = compareIniValues(left, parseIniAdditive(state, normalizeKey), (a, b) => a < b);
            continue;
        }
        if (matchIniOperator(state, "<=")) {
            left = compareIniValues(left, parseIniAdditive(state, normalizeKey), (a, b) => a <= b);
            continue;
        }
        if (matchIniOperator(state, ">")) {
            left = compareIniValues(left, parseIniAdditive(state, normalizeKey), (a, b) => a > b);
            continue;
        }
        if (matchIniOperator(state, ">=")) {
            left = compareIniValues(left, parseIniAdditive(state, normalizeKey), (a, b) => a >= b);
            continue;
        }
        return left;
    }
}

function parseIniAdditive(
    state: IniExpressionState,
    normalizeKey: (value: string) => string,
): IniExpressionValue {
    let left = parseIniMultiplicative(state, normalizeKey);
    while (true) {
        if (matchIniOperator(state, "+")) {
            const right = parseIniMultiplicative(state, normalizeKey);
            if (typeof left === "string" || typeof right === "string") {
                left = String(left) + String(right);
            } else {
                left = toIniNumber(left) + toIniNumber(right);
            }
            continue;
        }
        if (matchIniOperator(state, "-")) {
            left = toIniNumber(left) - toIniNumber(parseIniMultiplicative(state, normalizeKey));
            continue;
        }
        return left;
    }
}

function parseIniMultiplicative(
    state: IniExpressionState,
    normalizeKey: (value: string) => string,
): IniExpressionValue {
    let left = parseIniUnary(state, normalizeKey);
    while (true) {
        if (matchIniOperator(state, "*")) {
            left = toIniNumber(left) * toIniNumber(parseIniUnary(state, normalizeKey));
            continue;
        }
        if (matchIniOperator(state, "/")) {
            left = toIniNumber(left) / toIniNumber(parseIniUnary(state, normalizeKey));
            continue;
        }
        if (matchIniOperator(state, "//")) {
            left = Math.floor(toIniNumber(left) / toIniNumber(parseIniUnary(state, normalizeKey)));
            continue;
        }
        if (matchIniOperator(state, "%")) {
            left = toIniNumber(left) % toIniNumber(parseIniUnary(state, normalizeKey));
            continue;
        }
        return left;
    }
}

function parseIniUnary(
    state: IniExpressionState,
    normalizeKey: (value: string) => string,
): IniExpressionValue {
    if (matchIniOperator(state, "!")) {
        return !parseIniUnary(state, normalizeKey);
    }
    if (matchIniOperator(state, "-")) {
        return -toIniNumber(parseIniUnary(state, normalizeKey));
    }
    if (matchIniOperator(state, "+")) {
        return toIniNumber(parseIniUnary(state, normalizeKey));
    }
    return parseIniPrimary(state, normalizeKey);
}

function parseIniPrimary(
    state: IniExpressionState,
    normalizeKey: (value: string) => string,
): IniExpressionValue {
    const token = state.tokens[state.index++];
    if (!token) {
        throw new Error("Unexpected end of expression");
    }

    if (token.type === "number" || token.type === "string" || token.type === "boolean") {
        return token.value;
    }

    if (token.type === "identifier") {
        const runtimeValue = state.runtimeValues?.[normalizeKey(token.value)];
        if (runtimeValue !== undefined) {
            return runtimeValue;
        }
        return state.variables.get(normalizeKey(token.value)) ?? 0;
    }

    if (token.type === "paren" && token.value === "(") {
        const value = parseIniLogicalOr(state, normalizeKey);
        const closing = state.tokens[state.index++];
        if (!closing || closing.type !== "paren" || closing.value !== ")") {
            throw new Error("Expected closing parenthesis");
        }
        return value;
    }

    throw new Error(`Unexpected token type: ${token.type}`);
}

function matchIniOperator(state: IniExpressionState, operator: string): boolean {
    const token = state.tokens[state.index];
    if (token?.type !== "operator" || token.value !== operator) {
        return false;
    }
    state.index++;
    return true;
}

function toIniNumber(value: IniExpressionValue): number {
    if (typeof value === "number") {
        return value;
    }
    if (typeof value === "boolean") {
        return value ? 1 : 0;
    }
    const numeric = Number(value);
    if (!Number.isFinite(numeric)) {
        throw new Error(`Expression value is not numeric: ${value}`);
    }
    return numeric;
}

function compareIniEquality(left: IniExpressionValue, right: IniExpressionValue): boolean {
    const [a, b] = normalizeIniComparableValues(left, right);
    return a === b;
}

function compareIniValues(
    left: IniExpressionValue,
    right: IniExpressionValue,
    compare: (left: number | string, right: number | string) => boolean,
): boolean {
    const [a, b] = normalizeIniComparableValues(left, right);
    return compare(a, b);
}

function normalizeIniComparableValues(
    left: IniExpressionValue,
    right: IniExpressionValue,
): [number | string, number | string] {
    if (typeof left === "boolean" || typeof right === "boolean") {
        return [toIniNumber(left), toIniNumber(right)];
    }

    const leftNumber = Number(left);
    const rightNumber = Number(right);
    if (Number.isFinite(leftNumber) && Number.isFinite(rightNumber)) {
        return [leftNumber, rightNumber];
    }

    return [String(left), String(right)];
}
