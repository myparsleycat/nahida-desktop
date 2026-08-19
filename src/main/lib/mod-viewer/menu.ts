import { canonicalVarNames, type IniLine, type IniSections } from "./ini";

const SLOT_RE = /^\$(\w+)\s*={2,3}\s*(\d+)$/;
const ASSIGN_RE = /^\$(\w+)\s*=\s*(.+)$/;
const FLIP_RE = /^1\s*-\s*\$(\w+)$/;
const INCR_RE = /^\$(\w+)\s*\+\s*1$/;
const INCR_MOD_RE = /^\(\s*\$(\w+)\s*\+\s*1\s*\)\s*%\s*(\d+)$/;
const STEP_RE = /^\$(\w+)\s*([+-])\s*1$/;
const MOD_RE = /^\$(\w+)\s*%\s*(\d+)$/;
const GUARD_RE = /^\$(\w+)\s*(==|!=|>=|<=|>|<)\s*(-?\d+)$/;
const LITERAL_RE = /^-?\d+(?:\.\d+)?$/;
const ELSE_RE = /^(?:else\s+if|elif)\s+(.*)$/i;
const MIN_SLOTS = 2;
const MAX_RANGE_LENGTH = 4096;
const NEGATED_OP: Record<string, string> = {
    "==": "!=",
    "!=": "==",
    "<": ">=",
    ">=": "<",
    ">": "<=",
    "<=": ">",
};

export type MenuGuard = { var: string; op: string; value: string };

export type MenuEffect = {
    when: MenuGuard | null;
    var: string;
    value: string;
};

export type MenuSlot = {
    name: string;
    slot: number;
    var: string;
    values: string[];
    effects: MenuEffect[];
    source?: string;
    iniPath?: string;
    section: string;
};

type SlotBranch = { slotVar: string; slotValue: string; body: string[] };

export function extractMenuToggles(
    sections: IniSections,
    varPrefix?: string,
    source?: string,
    canonicalVars?: Record<string, string>,
): Record<string, MenuSlot> {
    const canon = canonicalVars ?? canonicalVarNames(sections);
    const declared = (name: string) => canon[name.toLowerCase()] ?? name;
    const menu: Record<string, MenuSlot> = {};

    for (const [name, lines] of Object.entries(sections)) {
        if (!name.toLowerCase().startsWith("commandlist")) {
            continue;
        }
        const parsed: Array<{
            slot: string;
            info: { var: string; values: string[]; effects: MenuEffect[] };
        }> = [];
        for (const branch of splitSlotBranches(lines)) {
            const info = parseBranch(branch.body);
            if (info) {
                parsed.push({ slot: branch.slotValue, info });
            }
        }
        if (parsed.length < MIN_SLOTS) {
            continue;
        }
        const src = lines[0];
        for (const item of parsed) {
            const variable = declared(item.info.var);
            const key = uniqueKey(menu, `${varPrefix ?? ""}${name}#${item.slot}`);
            menu[key] = {
                name: variable,
                slot: Number(item.slot),
                var: `${varPrefix ?? ""}${variable}`,
                values: item.info.values,
                effects: item.info.effects.map((effect) => ({
                    when: effect.when
                        ? { ...effect.when, var: `${varPrefix ?? ""}${declared(effect.when.var)}` }
                        : null,
                    var: `${varPrefix ?? ""}${declared(effect.var)}`,
                    value: effect.value,
                })),
                source,
                iniPath: src?.iniPath,
                section: name,
            };
        }
    }

    const arrowItems = new Map<
        number,
        Array<{ variable: string; values: string[]; section: string; iniPath?: string }>
    >();
    const buttonRe = /^CommandListButton(\d+)(Left|Right)$/i;
    for (const [name, lines] of Object.entries(sections)) {
        const match = buttonRe.exec(name);
        if (!match) {
            continue;
        }
        const parsed = parseArrowButton(lines);
        if (!parsed) {
            continue;
        }
        const slot = Number(match[1]);
        const bucket = arrowItems.get(slot) ?? [];
        bucket.push({
            variable: parsed.variable,
            values: parsed.values,
            section: name,
            iniPath: lines[0]?.iniPath,
        });
        arrowItems.set(slot, bucket);
    }

    if (arrowItems.size >= MIN_SLOTS) {
        for (const [slot, candidates] of [...arrowItems.entries()].sort(
            (left, right) => left[0] - right[0],
        )) {
            const first = candidates[0];
            const sameVar = candidates.filter(
                (item) => item.variable.toLowerCase() === first.variable.toLowerCase(),
            );
            const values =
                sameVar.length > 1 &&
                new Set(sameVar.map((item) => item.values.join(","))).size === 1
                    ? sameVar[0].values
                    : first.values;
            const variable = declared(first.variable);
            const buttonSection = first.section.replace(/(?:Left|Right)$/i, "");
            const key = uniqueKey(menu, `${varPrefix ?? ""}${buttonSection}#${slot}`);
            menu[key] = {
                name: variable,
                slot,
                var: `${varPrefix ?? ""}${variable}`,
                values,
                effects: [],
                source,
                iniPath: first.iniPath,
                section: first.section,
            };
        }
    }

    return menu;
}

function splitSlotBranches(lines: IniLine[]): SlotBranch[] {
    const cleaned = lines.map((line) => line.text.split(";")[0].trim());

    const scan = (block: string[]): SlotBranch[] => {
        const found: SlotBranch[] = [];
        let index = 0;
        while (index < block.length) {
            const line = block[index];
            if (!line.toLowerCase().startsWith("if ")) {
                index += 1;
                continue;
            }
            let depth = 1;
            let cursor = index + 1;
            const parts: Array<{ cond: string | null; start: number; stop: number | null }> = [
                { cond: line.slice(3).trim(), start: index + 1, stop: null },
            ];
            let end: number | null = null;
            while (cursor < block.length) {
                const current = block[cursor];
                const lowered = current.toLowerCase();
                if (lowered.startsWith("if ")) {
                    depth += 1;
                } else if (lowered === "endif") {
                    depth -= 1;
                    if (depth === 0) {
                        parts[parts.length - 1].stop = cursor;
                        end = cursor;
                        break;
                    }
                } else if (depth === 1) {
                    const elif = ELSE_RE.exec(current);
                    if (elif || lowered === "else") {
                        parts[parts.length - 1].stop = cursor;
                        parts.push({
                            cond: elif ? elif[1].trim() : null,
                            start: cursor + 1,
                            stop: null,
                        });
                    }
                }
                cursor += 1;
            }
            if (end === null) {
                index += 1;
                continue;
            }

            const nested = parts.flatMap((part) =>
                scan(block.slice(part.start, part.stop ?? part.start)),
            );
            const first = SLOT_RE.exec(parts[0].cond ?? "");
            const slotParts: SlotBranch[] = [];
            if (first) {
                const slotVar = first[1];
                for (const part of parts) {
                    const match = SLOT_RE.exec(part.cond ?? "");
                    if (match && match[1].toLowerCase() === slotVar.toLowerCase()) {
                        slotParts.push({
                            slotVar: match[1],
                            slotValue: match[2],
                            body: block.slice(part.start, part.stop ?? part.start).filter(Boolean),
                        });
                    }
                }
            }
            if (slotParts.length >= MIN_SLOTS && nested.length === 0) {
                found.push(...slotParts);
            }
            found.push(...nested);
            index = end + 1;
        }
        return found;
    };

    return scan(cleaned);
}

function parseBranch(
    body: string[],
): { var: string; values: string[]; effects: MenuEffect[] } | null {
    let variable: string | undefined;
    let values: string[] | undefined;
    const effects: MenuEffect[] = [];
    const stack: Array<{ guard: MenuGuard | null; branches: number }> = [];
    let wrap: { guard: MenuGuard; depth: number } | null = null;
    let inWrapElse = false;

    for (const line of body) {
        const lowered = line.toLowerCase();
        if (lowered.startsWith("if ")) {
            stack.push({ guard: parseGuard(line.slice(3)), branches: 1 });
            continue;
        }
        if (lowered === "endif") {
            stack.pop();
            if (wrap && stack.length < wrap.depth) {
                wrap = null;
                inWrapElse = false;
            }
            continue;
        }
        const elif = ELSE_RE.exec(line);
        if (elif || lowered === "else") {
            inWrapElse = Boolean(wrap) && stack.length === wrap?.depth && !elif;
            if (stack.length > 0) {
                const frame = stack[stack.length - 1];
                frame.guard = elif
                    ? parseGuard(elif[1])
                    : frame.branches === 1
                      ? negateGuard(frame.guard)
                      : null;
                frame.branches += 1;
            }
            continue;
        }

        const assign = ASSIGN_RE.exec(line);
        if (!assign) {
            continue;
        }
        const lhs = assign[1];
        const rhs = assign[2].trim();
        const guard = stack.length > 0 ? stack[stack.length - 1].guard : null;

        const flip = FLIP_RE.exec(rhs);
        if (flip && flip[1] === lhs) {
            variable = lhs;
            values = ["0", "1"];
            continue;
        }
        const incrMod = INCR_MOD_RE.exec(rhs);
        if (incrMod && incrMod[1].toLowerCase() === lhs.toLowerCase()) {
            const count = Number(incrMod[2]);
            if (count > 0) {
                variable = lhs;
                values = cycleValues(0, count - 1);
            }
            continue;
        }
        const incr = INCR_RE.exec(rhs);
        if (incr && incr[1].toLowerCase() === lhs.toLowerCase()) {
            variable = lhs;
            values = ["0", "1"];
            if (
                guard &&
                guard.var.toLowerCase() === lhs.toLowerCase() &&
                (guard.op === "<" || guard.op === "<=")
            ) {
                wrap = { guard, depth: stack.length };
            }
            continue;
        }
        const mod = MOD_RE.exec(rhs);
        if (
            mod &&
            mod[1].toLowerCase() === lhs.toLowerCase() &&
            lhs.toLowerCase() === variable?.toLowerCase()
        ) {
            const count = Number(mod[2]);
            if (count > 0) {
                values = cycleValues(0, count - 1);
            }
            continue;
        }
        if (!LITERAL_RE.test(rhs)) {
            continue;
        }
        if (
            inWrapElse &&
            variable &&
            lhs.toLowerCase() === variable.toLowerCase() &&
            wrap?.guard.var.toLowerCase() === variable.toLowerCase()
        ) {
            const hi = Number(wrap.guard.value) + (wrap.guard.op === "<=" ? 1 : 0);
            const lo = Number(rhs);
            if (hi >= lo) {
                values = cycleValues(lo, hi);
            }
            continue;
        }
        if (
            guard &&
            variable &&
            lhs.toLowerCase() === variable.toLowerCase() &&
            guard.var.toLowerCase() === variable.toLowerCase() &&
            (guard.op === ">" || guard.op === ">=")
        ) {
            const hi = Number(guard.value) - (guard.op === ">=" ? 1 : 0);
            const lo = Number(rhs);
            if (hi >= lo) {
                values = cycleValues(lo, hi);
            }
            continue;
        }
        effects.push({ when: guard, var: lhs, value: rhs });
    }

    if (!variable || !values || values.length === 0) {
        return null;
    }
    return { var: variable, values, effects };
}

function parseArrowButton(lines: IniLine[]): { variable: string; values: string[] } | null {
    const cleaned = lines.map((line) => line.text.split(";")[0].trim());
    let variable: string | undefined;
    let direction: string | undefined;
    for (const line of cleaned) {
        const assign = ASSIGN_RE.exec(line);
        if (!assign) {
            continue;
        }
        const step = STEP_RE.exec(assign[2].trim());
        if (step && step[1].toLowerCase() === assign[1].toLowerCase()) {
            variable = assign[1];
            direction = step[2];
            break;
        }
    }
    if (!variable) {
        return null;
    }

    let guard: MenuGuard | undefined;
    let reset: string | undefined;
    for (const [index, line] of cleaned.entries()) {
        if (!line.toLowerCase().startsWith("if ")) {
            continue;
        }
        const candidate = parseGuard(line.slice(3));
        if (!candidate || candidate.var.toLowerCase() !== variable.toLowerCase()) {
            continue;
        }
        const validOps = direction === "-" ? ["<", "<="] : [">", ">="];
        if (!validOps.includes(candidate.op)) {
            continue;
        }
        for (const later of cleaned.slice(index + 1)) {
            if (later.toLowerCase() === "endif") {
                break;
            }
            const assignment = ASSIGN_RE.exec(later);
            if (
                assignment &&
                assignment[1].toLowerCase() === variable.toLowerCase() &&
                LITERAL_RE.test(assignment[2].trim())
            ) {
                guard = candidate;
                reset = assignment[2].trim();
                break;
            }
        }
        if (guard) {
            break;
        }
    }
    if (!guard || reset === undefined) {
        return null;
    }

    const boundary = Number(guard.value);
    const resetValue = Number(reset);
    const lo = direction === "-" ? boundary + (guard.op === "<=" ? 1 : 0) : resetValue;
    const hi = direction === "-" ? resetValue : boundary - (guard.op === ">=" ? 1 : 0);
    if (hi < lo) {
        return null;
    }
    const values = cycleValues(lo, hi);
    if (values.length === 0) {
        return null;
    }
    return { variable, values };
}

function parseGuard(text: string): MenuGuard | null {
    const match = GUARD_RE.exec(text.trim());
    return match ? { var: match[1], op: match[2], value: match[3] } : null;
}

function negateGuard(guard: MenuGuard | null): MenuGuard | null {
    return guard ? { ...guard, op: NEGATED_OP[guard.op] } : null;
}

function cycleValues(lo: number, hi: number): string[] {
    const length = hi - lo + 1;
    if (!Number.isInteger(length) || length <= 0 || length > MAX_RANGE_LENGTH) {
        return [];
    }
    return Array.from({ length }, (_, index) => String(lo + index));
}

function uniqueKey(menu: Record<string, MenuSlot>, base: string): string {
    if (!menu[base]) {
        return base;
    }
    let suffix = 2;
    while (menu[`${base}_${suffix}`]) {
        suffix += 1;
    }
    return `${base}_${suffix}`;
}
