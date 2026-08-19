import type { Dnf, DnfClause } from "@shared/mod-viewer/types";

export const DNF_TRUE: Dnf = [[]];
export const DNF_FALSE: Dnf = [];

const MAX_DNF_GROUPS = 128;
const CLAUSE_RE = /^\$(\w+)\s*(==|!=|<=|>=|<|>)\s*(-?[\w.]+)$/;
const ASSIGN_BOOL_RE = /^\$(\w+)\s*=\s*(.+)$/;
const STRUCT_RE = /(\(|\)|&&|\|\||!(?!=))/;

type IniSections = Record<string, Array<{ text: string } | string>>;

export function dnfOr(left: Dnf, right: Dnf): Dnf {
    if (sameDnf(left, DNF_TRUE) || sameDnf(right, DNF_TRUE)) {
        return DNF_TRUE;
    }
    if (sameDnf(left, DNF_FALSE)) {
        return right;
    }
    if (sameDnf(right, DNF_FALSE)) {
        return left;
    }
    const out = [...left];
    for (const group of right) {
        if (!out.some((existing) => sameGroup(existing, group))) {
            out.push(group);
        }
    }
    if (out.some((group) => group.length === 0)) {
        return DNF_TRUE;
    }
    return out.length <= MAX_DNF_GROUPS ? out : DNF_TRUE;
}

export function dnfAnd(left: Dnf, right: Dnf): Dnf {
    if (left.length === 0 || right.length === 0) {
        return DNF_FALSE;
    }
    if (sameDnf(left, DNF_TRUE)) {
        return right;
    }
    if (sameDnf(right, DNF_TRUE)) {
        return left;
    }
    if (left.length * right.length > MAX_DNF_GROUPS) {
        return DNF_TRUE;
    }

    const out: Dnf = [];
    for (const leftGroup of left) {
        for (const rightGroup of right) {
            const merged = simplifyGroup([
                ...leftGroup,
                ...rightGroup.filter(
                    (clause) => !leftGroup.some((existing) => sameClause(existing, clause)),
                ),
            ]);
            if (!out.some((existing) => sameGroup(existing, merged))) {
                out.push(merged);
            }
        }
    }
    return out;
}

export function dnfNot(dnf: Dnf): Dnf {
    if (dnf.length === 0) {
        return DNF_TRUE;
    }
    if (sameDnf(dnf, DNF_TRUE)) {
        return DNF_FALSE;
    }
    let result: Dnf = DNF_TRUE;
    for (const group of dnf) {
        if (group.length === 0) {
            return DNF_FALSE;
        }
        result = dnfAnd(
            result,
            group.map((clause) => [
                { var: clause.var, value: clause.value, negate: !clause.negate },
            ]),
        );
    }
    return result;
}

export function parseConditionDnf(
    content: string,
    aliasMap: Record<string, Dnf>,
    valueDomains?: Record<string, string[]>,
): Dnf {
    const tokens = content
        .split(STRUCT_RE)
        .map((token) => token.trim())
        .filter(Boolean);
    let pos = 0;

    const parseOr = (): Dnf => {
        let node = parseAnd();
        while (pos < tokens.length && tokens[pos] === "||") {
            pos += 1;
            node = dnfOr(node, parseAnd());
        }
        return node;
    };

    const parseAnd = (): Dnf => {
        let node = parseAtom();
        while (pos < tokens.length && tokens[pos] === "&&") {
            pos += 1;
            node = dnfAnd(node, parseAtom());
        }
        return node;
    };

    const parseAtom = (): Dnf => {
        if (pos >= tokens.length) {
            return DNF_TRUE;
        }
        const tok = tokens[pos];
        if (tok === "!") {
            pos += 1;
            return dnfNot(parseAtom());
        }
        if (tok === "(") {
            pos += 1;
            const node = parseOr();
            if (pos < tokens.length && tokens[pos] === ")") {
                pos += 1;
            }
            return node;
        }
        if (tok === ")" || tok === "&&" || tok === "||") {
            pos += 1;
            return DNF_TRUE;
        }
        pos += 1;
        return atomToDnf(tok, aliasMap, valueDomains);
    };

    return parseOr();
}

export function normalizeDnf(dnf: Dnf, toggleVars: Iterable<string>, varPrefix?: string): Dnf {
    if (dnf.length === 0) {
        return DNF_FALSE;
    }
    if (sameDnf(dnf, DNF_TRUE)) {
        return DNF_TRUE;
    }
    const tracked = new Map(
        [...toggleVars].map((variable) => [variable.toLowerCase(), variable] as const),
    );
    const out: Dnf = [];
    for (const group of dnf) {
        const kept = group
            .filter((clause) => tracked.has(clause.var.toLowerCase()))
            .map((clause) => ({
                var: tracked.get(clause.var.toLowerCase())!,
                value: clause.value,
                negate: clause.negate,
            }));
        if (kept.length === 0) {
            return DNF_TRUE;
        }
        const prefixed = varPrefix
            ? kept.map((clause) => ({ ...clause, var: `${varPrefix}${clause.var}` }))
            : kept;
        if (!out.some((existing) => sameGroup(existing, prefixed))) {
            out.push(prefixed);
        }
    }
    return out.length > 0 ? out : DNF_FALSE;
}

export function buildBoolAliasMap(sections: IniSections): Record<string, Dnf> {
    const rawDefs: Record<string, string> = {};
    for (const lines of Object.values(sections)) {
        for (const raw of lines) {
            const line = stripComment(lineText(raw));
            const match = ASSIGN_BOOL_RE.exec(line);
            if (!match) {
                continue;
            }
            const alias = match[1];
            const rhs = match[2].trim();
            if (!rhs.includes("==") && !rhs.includes("!=")) {
                continue;
            }
            rawDefs[alias] ??= rhs;
        }
    }

    const aliasMap: Record<string, Dnf> = {};
    for (let pass = 0; pass < 2; pass++) {
        for (const [alias, rhs] of Object.entries(rawDefs)) {
            const dnf = parseConditionDnf(rhs, aliasMap);
            if (dnf.length > 0 && !sameDnf(dnf, DNF_TRUE)) {
                aliasMap[alias] = dnf;
            }
        }
    }
    return aliasMap;
}

export function possibleGroups(groups: Dnf): Dnf {
    const out: Dnf = [];
    for (const group of groups) {
        const equals = new Map<string, string>();
        const excluded = new Map<string, Set<string>>();
        let impossible = false;
        for (const clause of group) {
            if (clause.negate) {
                const bucket = excluded.get(clause.var) ?? new Set<string>();
                bucket.add(clause.value);
                excluded.set(clause.var, bucket);
                if (equals.get(clause.var) === clause.value) {
                    impossible = true;
                }
            } else {
                if (equals.has(clause.var) && equals.get(clause.var) !== clause.value) {
                    impossible = true;
                }
                equals.set(clause.var, clause.value);
                if (excluded.get(clause.var)?.has(clause.value)) {
                    impossible = true;
                }
            }
        }
        if (!impossible && !out.some((existing) => sameGroup(existing, group))) {
            out.push(group);
        }
    }
    return out;
}

export function sameDnf(left: Dnf, right: Dnf): boolean {
    return (
        left.length === right.length && left.every((group, index) => sameGroup(group, right[index]))
    );
}

export function dnfCovers(drawCond: Dnf, variantCond: Dnf): boolean {
    if (isUnconstrained(variantCond)) {
        return true;
    }
    if (isUnconstrained(drawCond)) {
        return false;
    }
    return variantCond.some((variantGroup) =>
        drawCond.some((drawGroup) =>
            variantGroup.every((clause) =>
                drawGroup.some((candidate) => sameClause(candidate, clause)),
            ),
        ),
    );
}

export function isUnconstrained(dnf: Dnf): boolean {
    return dnf.length === 1 && dnf[0].length === 0;
}

function atomToDnf(
    atomRaw: string,
    aliasMap: Record<string, Dnf>,
    valueDomains?: Record<string, string[]>,
): Dnf {
    let atom = atomRaw.trim();
    if (!atom) {
        return DNF_TRUE;
    }
    let negateAtom = false;
    while (atom.startsWith("!")) {
        negateAtom = !negateAtom;
        atom = atom.slice(1).trim();
    }

    const clauseMatch = CLAUSE_RE.exec(atom);
    let dnf: Dnf;
    if (clauseMatch) {
        const op = clauseMatch[2];
        dnf =
            op === "==" || op === "!="
                ? [
                      [
                          {
                              var: clauseMatch[1],
                              value: clauseMatch[3],
                              negate: op === "!=",
                          },
                      ],
                  ]
                : expandComparison(clauseMatch[1], op, clauseMatch[3], valueDomains);
    } else {
        const bare = /^\$(\w+)$/.exec(atom);
        if (bare) {
            dnf = aliasMap[bare[1]] ?? [[{ var: bare[1], value: "0", negate: true }]];
        } else if (/^-?\d+(?:\.\d+)?$/.test(atom) && Number(atom) === 0) {
            dnf = DNF_FALSE;
        } else {
            dnf = DNF_TRUE;
        }
    }
    return negateAtom ? dnfNot(dnf) : dnf;
}

function expandComparison(
    variable: string,
    op: string,
    rhs: string,
    valueDomains?: Record<string, string[]>,
): Dnf {
    const domain =
        valueDomains?.[variable] ??
        valueDomains?.[variable.toLowerCase()] ??
        Object.entries(valueDomains ?? {}).find(
            ([key]) => key.toLowerCase() === variable.toLowerCase(),
        )?.[1];
    if (!domain?.length) {
        return DNF_TRUE;
    }
    const right = Number(rhs);
    if (!Number.isFinite(right)) {
        return DNF_TRUE;
    }
    const matched = domain.filter((value) => {
        const left = Number(value);
        return Number.isFinite(left) && compareOp(op, left, right);
    });
    if (matched.length === 0) {
        return DNF_FALSE;
    }
    return matched.map((value) => [{ var: variable, value, negate: false }]);
}

function compareOp(op: string, left: number, right: number): boolean {
    if (op === "<") {
        return left < right;
    }
    if (op === "<=") {
        return left <= right;
    }
    if (op === ">") {
        return left > right;
    }
    if (op === ">=") {
        return left >= right;
    }
    return false;
}

function simplifyGroup(group: DnfClause[]): DnfClause[] {
    const equals = new Map<string, Set<string>>();
    for (const clause of group) {
        if (!clause.negate) {
            const bucket = equals.get(clause.var) ?? new Set<string>();
            bucket.add(clause.value);
            equals.set(clause.var, bucket);
        }
    }
    const redundant = new Map(
        [...equals.entries()]
            .filter(([, values]) => values.size === 1)
            .map(([variable, values]) => [variable, [...values][0]] as const),
    );
    if (redundant.size === 0) {
        return group;
    }
    return group.filter(
        (clause) =>
            !(clause.negate && (redundant.get(clause.var) ?? clause.value) !== clause.value),
    );
}

function sameGroup(left: DnfClause[], right: DnfClause[]): boolean {
    return (
        left.length === right.length &&
        left.every((clause, index) => sameClause(clause, right[index]))
    );
}

function sameClause(left: DnfClause, right: DnfClause): boolean {
    return left.var === right.var && left.value === right.value && left.negate === right.negate;
}

function lineText(line: { text: string } | string): string {
    return typeof line === "string" ? line : line.text;
}

function stripComment(line: string): string {
    return line.split(";")[0].trim();
}
