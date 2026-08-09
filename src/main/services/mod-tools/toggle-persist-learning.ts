import { createHash } from "node:crypto";
import path from "node:path";

export const TOGGLE_PERSIST_PROFILE_FILE = ".togglepersistprofile.json";
export const TOGGLE_PERSIST_PROFILE_VERSION = 1;

const observationWindowMs = 600_000;
const observationLimit = 128;
const initialQuietMs = 3_000;
const maximumQuietMs = 10_000;

export interface TogglePersistLearnedVariable {
    name: string;
    medianIntervalMs: number;
    learnedAt: string;
}

export interface TogglePersistProfile {
    version: typeof TOGGLE_PERSIST_PROFILE_VERSION;
    files: Record<
        string,
        {
            fingerprint: string;
            variables: Record<string, TogglePersistLearnedVariable>;
        }
    >;
}

interface Observation {
    at: number;
    revision: number;
    value: string;
    numericValue: number | null;
}

interface VariableState {
    name: string;
    observations: Observation[];
    pendingValue?: string;
    pendingDueAt?: number;
    status: "observing" | "suspected" | "suppressed";
    learnedProfile?: TogglePersistLearnedVariable;
    learnedInSession: boolean;
    cohortSuppressed: boolean;
    sparseCohortSuppressed: boolean;
    suppressedObservationCount?: number;
}

interface FileState {
    variables: Map<string, VariableState>;
}

export interface TogglePersistObservationResult {
    newlySuppressed: string[];
    newlyLearned: TogglePersistLearnedVariable[];
    nextDueAt?: number;
}

export interface TogglePersistReadyResult {
    updates: Map<string, string>;
    nextDueAt?: number;
}

export class TogglePersistLearner {
    private files = new Map<string, FileState>();

    public registerLearnedVariables(
        targetIniPath: string,
        variables: Record<string, TogglePersistLearnedVariable>,
    ) {
        const file = this.requireFile(targetIniPath);
        Object.entries(variables).forEach(([varName, learned]) => {
            const varKey = varName.toLowerCase();
            const state = file.variables.get(varKey);
            if (state) {
                state.learnedProfile = learned;
                return;
            }
            file.variables.set(varKey, {
                name: learned.name,
                observations: [],
                status: "observing",
                learnedProfile: learned,
                learnedInSession: false,
                cohortSuppressed: false,
                sparseCohortSuppressed: false,
            });
        });
    }

    public observe(input: {
        targetIniPath: string;
        varName: string;
        value: string;
        revision: number;
        at: number;
    }): TogglePersistObservationResult {
        const file = this.requireFile(input.targetIniPath);
        const varKey = input.varName.toLowerCase();
        const state = file.variables.get(varKey) ?? {
            name: input.varName,
            observations: [],
            status: "observing" as const,
            learnedInSession: false,
            cohortSuppressed: false,
            sparseCohortSuppressed: false,
        };
        const previousMedianInterval = medianInterval(state.observations);
        const lastObservation = state.observations.at(-1);
        if (lastObservation && input.at - lastObservation.at > observationWindowMs) {
            state.observations = [];
            state.status = "observing";
            state.cohortSuppressed = false;
            state.sparseCohortSuppressed = false;
            state.suppressedObservationCount = undefined;
        }
        const cooldownMs = Math.min(
            120_000,
            Math.max(30_000, (previousMedianInterval ?? initialQuietMs) * 5),
        );

        if (
            state.status === "suppressed" &&
            lastObservation &&
            input.at - lastObservation.at > cooldownMs
        ) {
            state.observations = [];
            state.status = "observing";
            state.cohortSuppressed = false;
            state.sparseCohortSuppressed = false;
            state.suppressedObservationCount = undefined;
        }

        state.name = input.varName;
        state.observations.push({
            at: input.at,
            revision: input.revision,
            value: input.value,
            numericValue: parseFiniteNumber(input.value),
        });
        state.observations = state.observations
            .filter((observation) => observation.at >= input.at - observationWindowMs)
            .slice(-observationLimit);
        state.pendingValue = input.value;
        state.pendingDueAt = input.at + quietWindow(state);
        file.variables.set(varKey, state);

        const newlySuppressed = this.evaluateSuppression(file);
        const newlyLearned = this.evaluateLearning(file, input.at);
        if (state.status === "suppressed") {
            state.pendingValue = undefined;
            state.pendingDueAt = undefined;
        }

        return {
            newlySuppressed,
            newlyLearned,
            nextDueAt: nextDueAt(file),
        };
    }

    public takeReady(targetIniPath: string, at: number): TogglePersistReadyResult {
        const file = this.files.get(fileKey(targetIniPath));
        if (!file) return { updates: new Map() };

        const updates = new Map<string, string>();
        file.variables.forEach((state, varName) => {
            if (
                state.status === "suppressed" ||
                state.pendingValue === undefined ||
                state.pendingDueAt === undefined ||
                state.pendingDueAt > at
            ) {
                return;
            }

            updates.set(varName, state.pendingValue);
            state.pendingValue = undefined;
            state.pendingDueAt = undefined;
            if (state.status === "suspected") {
                state.status = "observing";
                state.cohortSuppressed = false;
                state.sparseCohortSuppressed = false;
                state.suppressedObservationCount = undefined;
            }
        });

        return { updates, nextDueAt: nextDueAt(file) };
    }

    public getNextDueAt(targetIniPath: string) {
        const file = this.files.get(fileKey(targetIniPath));
        return file ? nextDueAt(file) : undefined;
    }

    public clear() {
        this.files.clear();
    }

    private requireFile(targetIniPath: string) {
        const key = fileKey(targetIniPath);
        const existing = this.files.get(key);
        if (existing) return existing;
        const created = { variables: new Map<string, VariableState>() };
        this.files.set(key, created);
        return created;
    }

    private evaluateSuppression(file: FileState) {
        const newlySuppressed: string[] = [];

        file.variables.forEach((state) => {
            if (state.status === "suppressed") return;
            const evidence = runtimeEvidence(file, state);
            const observations = state.observations;
            const span = observationSpan(observations);
            const distinctValues = new Set(observations.map((observation) => observation.value));
            const observedMedianInterval = medianInterval(observations);
            const learnedThreshold =
                state.learnedProfile &&
                observations.length >= 3 &&
                span >= Math.max(2_000, state.learnedProfile.medianIntervalMs * 2) &&
                observedMedianInterval !== undefined &&
                observedMedianInterval >= state.learnedProfile.medianIntervalMs * 0.5 &&
                observedMedianInterval <= state.learnedProfile.medianIntervalMs * 2 &&
                evidence.regularCadence;
            const continuousThreshold =
                observations.length >= 8 &&
                span >= 15_000 &&
                distinctValues.size >= 6 &&
                evidence.count >= 2;
            const discreteThreshold =
                observations.length >= 10 &&
                span >= 30_000 &&
                distinctValues.size >= 2 &&
                distinctValues.size <= 4 &&
                evidence.regularCadence &&
                isDeterministicCycle(observations);

            if (learnedThreshold || continuousThreshold || discreteThreshold) {
                state.status = "suppressed";
                state.suppressedObservationCount = state.observations.length;
                state.pendingValue = undefined;
                state.pendingDueAt = undefined;
                newlySuppressed.push(state.name);
                return;
            }

            if (
                state.status === "observing" &&
                observations.length >= 4 &&
                span >= 6_000 &&
                evidence.count >= 1
            ) {
                state.status = "suspected";
            }
        });

        const suppressedStates = [...file.variables.values()].filter(
            (state) => state.status === "suppressed" && !state.cohortSuppressed,
        );
        file.variables.forEach((state) => {
            if (state.status === "suppressed" || state.observations.length < 3) return;
            if (observationSpan(state.observations) < 15_000) return;
            const primary = suppressedStates.find(
                (candidate) => conditionalCochangeRate(state, candidate) >= 0.9,
            );
            if (!primary) return;
            state.status = "suppressed";
            state.cohortSuppressed = true;
            state.suppressedObservationCount = state.observations.length;
            state.pendingValue = undefined;
            state.pendingDueAt = undefined;
            newlySuppressed.push(state.name);
        });

        if (suppressedStates.length > 0) {
            const sparseCandidates = [...file.variables.values()].filter(
                (state) => state.status !== "suppressed" && isSparseCycleCandidate(state),
            );
            sparseCandidates.forEach((state) => {
                const peer = sparseCandidates.find(
                    (candidate) => candidate !== state && isSparseRuntimePair(state, candidate),
                );
                if (!peer) return;
                [state, peer].forEach((candidate) => {
                    if (candidate.status === "suppressed") return;
                    candidate.status = "suppressed";
                    candidate.cohortSuppressed = true;
                    candidate.sparseCohortSuppressed = true;
                    candidate.suppressedObservationCount = candidate.observations.length;
                    candidate.pendingValue = undefined;
                    candidate.pendingDueAt = undefined;
                    newlySuppressed.push(candidate.name);
                });
            });
        }

        return newlySuppressed;
    }

    private evaluateLearning(file: FileState, at: number) {
        const newlyLearned: TogglePersistLearnedVariable[] = [];
        file.variables.forEach((state) => {
            if (state.status !== "suppressed" || state.learnedInSession) return;
            const evidence = runtimeEvidence(file, state);
            const individuallyLearned =
                !state.cohortSuppressed &&
                state.observations.length >= 12 &&
                observationSpan(state.observations) >= 30_000 &&
                evidence.count >= 3;
            const directCohortLearned =
                state.cohortSuppressed &&
                !state.sparseCohortSuppressed &&
                state.observations.length >= 4 &&
                observationSpan(state.observations) >= 30_000 &&
                [...file.variables.values()].some(
                    (candidate) =>
                        candidate !== state &&
                        candidate.status === "suppressed" &&
                        !candidate.cohortSuppressed &&
                        conditionalCochangeRate(state, candidate) >= 0.9,
                );
            const sparseCohortLearned =
                state.sparseCohortSuppressed &&
                state.observations.length >= 5 &&
                state.observations.length > (state.suppressedObservationCount ?? Infinity) &&
                observationSpan(state.observations) >= 30_000 &&
                [...file.variables.values()].some(
                    (candidate) =>
                        candidate !== state &&
                        candidate.status === "suppressed" &&
                        candidate.sparseCohortSuppressed &&
                        isSparseRuntimePair(state, candidate),
                ) &&
                [...file.variables.values()].some(
                    (candidate) => candidate.status === "suppressed" && !candidate.cohortSuppressed,
                );
            const cohortLearned = directCohortLearned || sparseCohortLearned;
            if (!individuallyLearned && !cohortLearned) return;

            const learned = {
                name: state.name,
                medianIntervalMs: Math.round(medianInterval(state.observations) ?? initialQuietMs),
                learnedAt: new Date(at).toISOString(),
            };
            state.learnedProfile = learned;
            state.learnedInSession = true;
            newlyLearned.push(learned);
        });
        return newlyLearned;
    }
}

export function fingerprintTogglePersistIni(content: string) {
    const normalized = content
        .replaceAll("\r\n", "\n")
        .split("\n")
        .reduce(
            (result, line) => {
                const trimmed = line.trim();
                if (trimmed.startsWith("[")) {
                    result.inConstants = /^\[Constants\]$/i.test(trimmed);
                }
                result.lines.push(
                    result.inConstants && /^global\s+persist\s+\$/i.test(trimmed)
                        ? line.replace(
                              /^(\s*global\s+persist\s+\$[^=]+\s*=\s*).*$/i,
                              "$1<persist-value>",
                          )
                        : line,
                );
                return result;
            },
            { inConstants: false, lines: [] as string[] },
        )
        .lines.join("\n");
    return createHash("sha256").update(normalized).digest("hex");
}

export function parseTogglePersistProfile(value: unknown): TogglePersistProfile {
    if (!isRecord(value) || value.version !== TOGGLE_PERSIST_PROFILE_VERSION) {
        throw new Error("Unsupported toggle persist profile version");
    }
    if (!isRecord(value.files)) throw new Error("Invalid toggle persist profile files");

    const files = Object.fromEntries(
        Object.entries(value.files).map(([fileName, rawFile]) => {
            if (!isRecord(rawFile) || typeof rawFile.fingerprint !== "string") {
                throw new Error(`Invalid toggle persist profile file entry: ${fileName}`);
            }
            if (!isRecord(rawFile.variables)) {
                throw new Error(`Invalid toggle persist profile variables: ${fileName}`);
            }
            const variables = Object.fromEntries(
                Object.entries(rawFile.variables).map(([varName, rawVariable]) => {
                    if (
                        !isRecord(rawVariable) ||
                        typeof rawVariable.name !== "string" ||
                        typeof rawVariable.medianIntervalMs !== "number" ||
                        !Number.isFinite(rawVariable.medianIntervalMs) ||
                        rawVariable.medianIntervalMs <= 0 ||
                        typeof rawVariable.learnedAt !== "string"
                    ) {
                        throw new Error(
                            `Invalid toggle persist profile variable: ${fileName}:${varName}`,
                        );
                    }
                    return [
                        varName.toLowerCase(),
                        {
                            name: rawVariable.name,
                            medianIntervalMs: rawVariable.medianIntervalMs,
                            learnedAt: rawVariable.learnedAt,
                        },
                    ];
                }),
            );
            return [fileName, { fingerprint: rawFile.fingerprint, variables }];
        }),
    );
    return { version: TOGGLE_PERSIST_PROFILE_VERSION, files };
}

export function createEmptyTogglePersistProfile(): TogglePersistProfile {
    return { version: TOGGLE_PERSIST_PROFILE_VERSION, files: {} };
}

export function togglePersistProfilePath(targetIniPath: string) {
    return path.join(path.dirname(targetIniPath), TOGGLE_PERSIST_PROFILE_FILE);
}

function runtimeEvidence(file: FileState, state: VariableState) {
    const active = activityRate(state.observations) >= 0.6;
    const regularCadence = isRegularCadence(state.observations);
    const continuousNumeric = isContinuousNumeric(state.observations);
    const correlated = [...file.variables.values()].some(
        (candidate) => candidate !== state && isStronglyCorrelated(state, candidate),
    );
    return {
        count: [active, regularCadence, continuousNumeric, correlated].filter(Boolean).length,
        regularCadence,
    };
}

function activityRate(observations: Observation[]) {
    const first = observations[0];
    const last = observations.at(-1);
    if (!first || !last) return 0;
    return (
        new Set(observations.map((observation) => observation.revision)).size /
        Math.max(1, last.revision - first.revision + 1)
    );
}

function isRegularCadence(observations: Observation[]) {
    const intervals = observationIntervals(observations);
    if (intervals.length < 2) return false;
    const middle = median(intervals);
    if (!middle || middle <= 0) return false;
    return median(intervals.map((interval) => Math.abs(interval - middle))) / middle <= 0.5;
}

function isContinuousNumeric(observations: Observation[]) {
    const values = observations
        .map((observation) => observation.numericValue)
        .filter((value): value is number => value !== null);
    if (values.length < 6 || values.length !== observations.length) return false;
    const steps = values.slice(1).map((value, index) => Math.abs(value - values[index]));
    const nonZeroSteps = steps.filter((step) => step > 0);
    if (nonZeroSteps.length < 5) return false;
    const middle = median(nonZeroSteps);
    if (!middle || middle <= 0) return false;
    return (
        nonZeroSteps.filter((step) => step >= middle * 0.25 && step <= middle * 4).length /
            nonZeroSteps.length >=
        0.75
    );
}

function isStronglyCorrelated(left: VariableState, right: VariableState) {
    const leftRevisions = new Set(left.observations.map((observation) => observation.revision));
    const rightRevisions = new Set(right.observations.map((observation) => observation.revision));
    const intersection = [...leftRevisions].filter((revision) => rightRevisions.has(revision));
    const unionSize = new Set([...leftRevisions, ...rightRevisions]).size;
    if (intersection.length >= 4 && intersection.length / unionSize >= 0.8) return true;

    const leftByRevision = new Map(
        left.observations.map((observation) => [observation.revision, observation.numericValue]),
    );
    const pairs = right.observations
        .map((observation) => [leftByRevision.get(observation.revision), observation.numericValue])
        .filter(
            (pair): pair is [number, number] =>
                pair[0] !== null && pair[0] !== undefined && pair[1] !== null,
        );
    if (pairs.length < 5) return false;
    return Math.abs(pearsonCorrelation(pairs)) >= 0.98;
}

function conditionalCochangeRate(dependent: VariableState, primary: VariableState) {
    const primaryRevisions = new Set(
        primary.observations.map((observation) => observation.revision),
    );
    const dependentRevisions = new Set(
        dependent.observations.map((observation) => observation.revision),
    );
    if (dependentRevisions.size === 0) return 0;
    return (
        [...dependentRevisions].filter((revision) => primaryRevisions.has(revision)).length /
        dependentRevisions.size
    );
}

function isSparseCycleCandidate(state: VariableState) {
    const distinctValues = new Set(state.observations.map((observation) => observation.value)).size;
    return (
        state.observations.length >= 4 &&
        observationSpan(state.observations) >= 30_000 &&
        distinctValues >= 2 &&
        distinctValues <= 4 &&
        isRegularCadence(state.observations) &&
        isDeterministicCycle(state.observations)
    );
}

function isSparseRuntimePair(left: VariableState, right: VariableState) {
    if (!isSparseCycleCandidate(left) || !isSparseCycleCandidate(right)) return false;
    const leftRevisions = new Set(left.observations.map((observation) => observation.revision));
    const rightRevisions = new Set(right.observations.map((observation) => observation.revision));
    const sharedRevisions = [...leftRevisions].filter((revision) =>
        rightRevisions.has(revision),
    ).length;
    return sharedRevisions / new Set([...leftRevisions, ...rightRevisions]).size >= 0.8;
}

function isDeterministicCycle(observations: Observation[]) {
    const transitions = new Map<string, Set<string>>();
    observations.slice(1).forEach((observation, index) => {
        const previous = observations[index].value;
        const nextValues = transitions.get(previous) ?? new Set<string>();
        nextValues.add(observation.value);
        transitions.set(previous, nextValues);
    });
    return [...transitions.values()].every((nextValues) => nextValues.size === 1);
}

function quietWindow(state: VariableState) {
    const interval = medianInterval(state.observations) ?? state.learnedProfile?.medianIntervalMs;
    if (interval === undefined) return initialQuietMs;
    return Math.min(maximumQuietMs, Math.max(initialQuietMs, interval * 3));
}

function medianInterval(observations: Observation[]) {
    const intervals = observationIntervals(observations).slice(-5);
    return intervals.length > 0 ? median(intervals) : undefined;
}

function observationIntervals(observations: Observation[]) {
    return observations
        .slice(1)
        .map((observation, index) => observation.at - observations[index].at);
}

function observationSpan(observations: Observation[]) {
    const first = observations[0];
    const last = observations.at(-1);
    return first && last ? last.at - first.at : 0;
}

function nextDueAt(file: FileState) {
    const dueTimes = [...file.variables.values()]
        .map((state) => state.pendingDueAt)
        .filter((dueAt): dueAt is number => dueAt !== undefined);
    return dueTimes.length > 0 ? Math.min(...dueTimes) : undefined;
}

function median(values: number[]) {
    const sorted = [...values].sort((left, right) => left - right);
    const middle = Math.floor(sorted.length / 2);
    return sorted.length % 2 === 0 ? (sorted[middle - 1] + sorted[middle]) / 2 : sorted[middle];
}

function pearsonCorrelation(pairs: [number, number][]) {
    const leftMean = pairs.reduce((sum, pair) => sum + pair[0], 0) / pairs.length;
    const rightMean = pairs.reduce((sum, pair) => sum + pair[1], 0) / pairs.length;
    const numerator = pairs.reduce(
        (sum, pair) => sum + (pair[0] - leftMean) * (pair[1] - rightMean),
        0,
    );
    const leftVariance = pairs.reduce((sum, pair) => sum + (pair[0] - leftMean) ** 2, 0);
    const rightVariance = pairs.reduce((sum, pair) => sum + (pair[1] - rightMean) ** 2, 0);
    const denominator = Math.sqrt(leftVariance * rightVariance);
    return denominator === 0 ? 0 : numerator / denominator;
}

function parseFiniteNumber(value: string) {
    if (value.trim() === "") return null;
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
}

function fileKey(targetIniPath: string) {
    return path.resolve(targetIniPath).toLowerCase();
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}
