import type { ViewerStateRule } from "@shared/mod-viewer/types";

import {
    DNF_TRUE,
    buildBoolAliasMap,
    dnfAnd,
    dnfNot,
    dnfOr,
    normalizeDnf,
    parseConditionDnf,
    possibleGroups,
} from "./dnf";
import { canonicalVarNames, stripComment, type IniSections } from "./ini";

const ASSIGN_RE = /^\$(\w+)\s*=\s*(-?\d+(?:\.\d+)?)\s*$/;
const ELIF_RE = /^(?:else\s+if|elif)\s+(.*)$/i;

export function extractStateRules(
    sections: IniSections,
    varPrefix?: string,
    canonicalVars?: Record<string, string>,
): ViewerStateRule[] {
    const present = Object.entries(sections).find(([name]) => name.toLowerCase() === "present");
    if (!present) {
        return [];
    }
    const canon = canonicalVars ?? canonicalVarNames(sections);
    const tracked = new Set(Object.values(canon));
    const aliases = buildBoolAliasMap(sections);
    const stack: Array<{
        cur: ReturnType<typeof parseConditionDnf>;
        seen: ReturnType<typeof parseConditionDnf>;
    }> = [];
    const rules: ViewerStateRule[] = [];

    for (const raw of present[1]) {
        const line = stripComment(raw.text);
        if (!line) {
            continue;
        }
        const lowered = line.toLowerCase();
        const elif = ELIF_RE.exec(line);
        if (elif) {
            if (stack.length > 0) {
                const frame = stack[stack.length - 1];
                const branch = parseConditionDnf(elif[1], aliases);
                frame.cur = dnfAnd(dnfNot(frame.seen), branch);
                frame.seen = dnfOr(frame.seen, branch);
            }
            continue;
        }
        if (lowered.startsWith("if ")) {
            const branch = parseConditionDnf(line.slice(3), aliases);
            stack.push({ cur: branch, seen: branch });
            continue;
        }
        if (lowered === "else") {
            if (stack.length > 0) {
                stack[stack.length - 1].cur = dnfNot(stack[stack.length - 1].seen);
            }
            continue;
        }
        if (lowered === "endif") {
            stack.pop();
            continue;
        }
        const assign = ASSIGN_RE.exec(line);
        if (!assign) {
            continue;
        }
        let combined = DNF_TRUE;
        for (const frame of stack) {
            combined = dnfAnd(combined, frame.cur);
        }
        const conditions = possibleGroups(normalizeDnf(combined, tracked, varPrefix));
        if (conditions.length === 0) {
            continue;
        }
        const variable = canon[assign[1].toLowerCase()] ?? assign[1];
        rules.push({
            var: `${varPrefix ?? ""}${variable}`,
            value: assign[2],
            conditions,
        });
    }
    return rules;
}
