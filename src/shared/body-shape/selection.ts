import { brushFalloff, clamp01, eraseVertex, paintVertex, type BrushMode } from "./weights";

export function buildVertexAdjacency(
    vertexCount: number,
    indices: ArrayLike<number>,
): readonly Uint32Array[] {
    const neighbors = Array.from({ length: Math.max(0, vertexCount) }, () => new Set<number>());

    for (let i = 0; i + 2 < indices.length; i += 3) {
        const a = indices[i];
        const b = indices[i + 1];
        const c = indices[i + 2];
        if (
            !Number.isInteger(a) ||
            !Number.isInteger(b) ||
            !Number.isInteger(c) ||
            a < 0 ||
            b < 0 ||
            c < 0 ||
            a >= neighbors.length ||
            b >= neighbors.length ||
            c >= neighbors.length
        ) {
            continue;
        }

        if (a !== b) {
            neighbors[a].add(b);
            neighbors[b].add(a);
        }
        if (a !== c) {
            neighbors[a].add(c);
            neighbors[c].add(a);
        }
        if (b !== c) {
            neighbors[b].add(c);
            neighbors[c].add(b);
        }
    }

    return neighbors.map((vertexNeighbors) =>
        Uint32Array.from(vertexNeighbors).sort((a, b) => a - b),
    );
}

/** Applies a radius-limited brush using accumulated mesh-edge distance from all seeds. */
export function applyGeodesicBrush(options: {
    positions: Float32Array;
    adjacency: readonly Uint32Array[];
    weights: Float32Array;
    seedVertexIndices: Iterable<number>;
    radius: number;
    strength: number;
    hardness?: number;
    mode: BrushMode;
    frontFacingOnly?: boolean;
    normals?: Float32Array;
    hitNormal?: readonly [number, number, number];
    normalThreshold?: number;
}): Uint32Array {
    const vertexCount = Math.min(
        options.weights.length,
        options.adjacency.length,
        Math.floor(options.positions.length / 3),
    );
    if (vertexCount === 0 || options.radius <= 0) return new Uint32Array();

    const distances = new Float64Array(vertexCount);
    distances.fill(Number.POSITIVE_INFINITY);
    const heapVertices: number[] = [];
    const heapDistances: number[] = [];

    for (const seed of options.seedVertexIndices) {
        if (!Number.isInteger(seed) || seed < 0 || seed >= vertexCount || distances[seed] === 0) {
            continue;
        }
        distances[seed] = 0;
        pushMinHeap(heapVertices, heapDistances, seed, 0);
    }

    const changed: number[] = [];
    const threshold = options.normalThreshold ?? 0.15;

    while (heapVertices.length > 0) {
        const current = popMinHeap(heapVertices, heapDistances);
        if (!current || current.distance !== distances[current.vertex]) continue;
        if (current.distance >= options.radius) continue;

        let allowVertex = true;
        if (options.frontFacingOnly && options.normals && options.hitNormal) {
            const v = current.vertex * 3;
            const nx = options.normals[v];
            const ny = options.normals[v + 1];
            const nz = options.normals[v + 2];
            const dot =
                nx * options.hitNormal[0] + ny * options.hitNormal[1] + nz * options.hitNormal[2];
            if (dot < threshold) {
                allowVertex = false;
            }
        }

        if (allowVertex) {
            const falloff = brushFalloff(current.distance, options.radius, options.hardness);
            const previousWeight = options.weights[current.vertex];
            const nextWeight =
                options.mode === "paint"
                    ? paintVertex(previousWeight, falloff, options.strength)
                    : eraseVertex(previousWeight, falloff, options.strength);
            if (nextWeight !== previousWeight) {
                options.weights[current.vertex] = nextWeight;
                changed.push(current.vertex);
            }
        }

        for (const neighbor of options.adjacency[current.vertex]) {
            if (neighbor >= vertexCount) continue;
            const currentOffset = current.vertex * 3;
            const neighborOffset = neighbor * 3;
            const dx = options.positions[currentOffset] - options.positions[neighborOffset];
            const dy = options.positions[currentOffset + 1] - options.positions[neighborOffset + 1];
            const dz = options.positions[currentOffset + 2] - options.positions[neighborOffset + 2];
            const distance = current.distance + Math.sqrt(dx * dx + dy * dy + dz * dz);
            if (distance >= options.radius || distance >= distances[neighbor]) continue;

            distances[neighbor] = distance;
            pushMinHeap(heapVertices, heapDistances, neighbor, distance);
        }
    }

    return Uint32Array.from(changed);
}

export function buildSymmetryMap(
    positions: Float32Array,
    axis: "x" | "y" | "z" = "x",
    tolerance = 1e-3,
): Int32Array {
    const vertexCount = Math.floor(positions.length / 3);
    const mirrorMap = new Int32Array(vertexCount);
    mirrorMap.fill(-1);
    if (vertexCount === 0) return mirrorMap;

    const cellSize = Math.max(tolerance * 2, 1e-4);
    const grid = new Map<string, number[]>();

    for (let i = 0; i < vertexCount; i += 1) {
        const o = i * 3;
        const cx = Math.floor(positions[o] / cellSize);
        const cy = Math.floor(positions[o + 1] / cellSize);
        const cz = Math.floor(positions[o + 2] / cellSize);
        const key = `${cx},${cy},${cz}`;
        const list = grid.get(key);
        if (list) {
            list.push(i);
        } else {
            grid.set(key, [i]);
        }
    }

    const tol2 = tolerance * tolerance;
    const axisIdx = axis === "x" ? 0 : axis === "y" ? 1 : 2;

    for (let i = 0; i < vertexCount; i += 1) {
        const o = i * 3;
        const targetPos: [number, number, number] = [
            axisIdx === 0 ? -positions[o] : positions[o],
            axisIdx === 1 ? -positions[o + 1] : positions[o + 1],
            axisIdx === 2 ? -positions[o + 2] : positions[o + 2],
        ];

        const cx = Math.floor(targetPos[0] / cellSize);
        const cy = Math.floor(targetPos[1] / cellSize);
        const cz = Math.floor(targetPos[2] / cellSize);

        let best = -1;
        let bestDist2 = tol2;

        for (let dx = -1; dx <= 1; dx += 1) {
            for (let dy = -1; dy <= 1; dy += 1) {
                for (let dz = -1; dz <= 1; dz += 1) {
                    const key = `${cx + dx},${cy + dy},${cz + dz}`;
                    const candidates = grid.get(key);
                    if (!candidates) continue;
                    for (const cand of candidates) {
                        const co = cand * 3;
                        const diffX = positions[co] - targetPos[0];
                        const diffY = positions[co + 1] - targetPos[1];
                        const diffZ = positions[co + 2] - targetPos[2];
                        const dist2 = diffX * diffX + diffY * diffY + diffZ * diffZ;
                        if (dist2 < bestDist2) {
                            bestDist2 = dist2;
                            best = cand;
                        }
                    }
                }
            }
        }
        mirrorMap[i] = best;
    }

    return mirrorMap;
}

export function mirrorWeightsWithCache(
    weights: Float32Array,
    mirrorMap: Int32Array,
    sourceIndices: Iterable<number>,
    mode: BrushMode = "paint",
): Uint32Array {
    const vertexCount = Math.min(weights.length, mirrorMap.length);
    const changed: number[] = [];

    for (const source of sourceIndices) {
        if (source < 0 || source >= vertexCount) continue;
        const mirror = mirrorMap[source];
        if (mirror < 0 || mirror >= vertexCount) continue;

        const prev = weights[mirror];
        const next =
            mode === "erase"
                ? Math.min(weights[mirror], weights[source])
                : Math.max(weights[mirror], weights[source]);

        if (next !== prev) {
            weights[mirror] = next;
            changed.push(mirror);
        }
    }

    return Uint32Array.from(changed);
}

export function smoothSelectionWeights(
    weights: Float32Array,
    adjacency: readonly Uint32Array[],
    strength: number,
    iterations: number,
): void {
    const blend = clamp01(strength);
    const vertexCount = Math.min(weights.length, adjacency.length);
    if (blend === 0 || vertexCount === 0) return;

    for (let iteration = 0; iteration < Math.floor(iterations); iteration += 1) {
        const previousWeights = new Float32Array(weights);
        for (let vertex = 0; vertex < vertexCount; vertex += 1) {
            let neighborWeightSum = 0;
            let neighborCount = 0;
            for (const neighbor of adjacency[vertex]) {
                if (neighbor >= weights.length) continue;
                neighborWeightSum += previousWeights[neighbor];
                neighborCount += 1;
            }
            if (neighborCount === 0) continue;

            weights[vertex] =
                previousWeights[vertex] +
                (neighborWeightSum / neighborCount - previousWeights[vertex]) * blend;
        }
    }
}

function pushMinHeap(
    vertices: number[],
    distances: number[],
    vertex: number,
    distance: number,
): void {
    let index = vertices.length;
    vertices.push(vertex);
    distances.push(distance);

    while (index > 0) {
        const parent = Math.floor((index - 1) / 2);
        if (distances[parent] <= distance) break;
        vertices[index] = vertices[parent];
        distances[index] = distances[parent];
        index = parent;
    }

    vertices[index] = vertex;
    distances[index] = distance;
}

function popMinHeap(
    vertices: number[],
    distances: number[],
): { vertex: number; distance: number } | undefined {
    if (vertices.length === 0) return undefined;

    const vertex = vertices[0];
    const distance = distances[0];
    const lastVertex = vertices.pop();
    const lastDistance = distances.pop();
    if (vertices.length === 0 || lastVertex === undefined || lastDistance === undefined) {
        return { vertex, distance };
    }

    let index = 0;
    while (true) {
        const left = index * 2 + 1;
        if (left >= vertices.length) break;
        const right = left + 1;
        const child = right < vertices.length && distances[right] < distances[left] ? right : left;
        if (distances[child] >= lastDistance) break;
        vertices[index] = vertices[child];
        distances[index] = distances[child];
        index = child;
    }
    vertices[index] = lastVertex;
    distances[index] = lastDistance;

    return { vertex, distance };
}

export function growSelectionWeights(
    weights: Float32Array,
    adjacency: readonly Uint32Array[],
    amount: number,
    iterations: number,
): void {
    const strength = clamp01(amount);
    const vertexCount = Math.min(weights.length, adjacency.length);
    if (strength === 0 || vertexCount === 0) return;

    const numIterations = Math.floor(iterations);
    for (let iter = 0; iter < numIterations; iter += 1) {
        const previousWeights = new Float32Array(weights);
        for (let v = 0; v < vertexCount; v += 1) {
            let maxNeighbor = previousWeights[v];
            for (const neighbor of adjacency[v]) {
                if (neighbor >= weights.length) continue;
                if (previousWeights[neighbor] > maxNeighbor) {
                    maxNeighbor = previousWeights[neighbor];
                }
            }
            if (maxNeighbor > previousWeights[v]) {
                weights[v] = clamp01(
                    previousWeights[v] + (maxNeighbor - previousWeights[v]) * strength,
                );
            }
        }
    }
}

export function shrinkSelectionWeights(
    weights: Float32Array,
    adjacency: readonly Uint32Array[],
    amount: number,
    iterations: number,
): void {
    const strength = clamp01(amount);
    const vertexCount = Math.min(weights.length, adjacency.length);
    if (strength === 0 || vertexCount === 0) return;

    const numIterations = Math.floor(iterations);
    for (let iter = 0; iter < numIterations; iter += 1) {
        const previousWeights = new Float32Array(weights);
        for (let v = 0; v < vertexCount; v += 1) {
            if (previousWeights[v] === 0) continue;
            let minNeighbor = previousWeights[v];
            const neighbors = adjacency[v];
            if (neighbors.length === 0) {
                minNeighbor = 0;
            } else {
                for (const neighbor of neighbors) {
                    if (neighbor >= weights.length) {
                        minNeighbor = 0;
                        break;
                    }
                    if (previousWeights[neighbor] < minNeighbor) {
                        minNeighbor = previousWeights[neighbor];
                    }
                }
            }
            if (minNeighbor < previousWeights[v]) {
                weights[v] = clamp01(
                    previousWeights[v] - (previousWeights[v] - minNeighbor) * strength,
                );
            }
        }
    }
}

export function buildConnectedComponents(
    vertexCount: number,
    adjacency: readonly Uint32Array[],
): Uint32Array {
    const count = Math.min(vertexCount, adjacency.length);
    const componentIds = new Uint32Array(vertexCount);
    if (count === 0) return componentIds;

    const visited = new Uint8Array(count);
    let currentComponentId = 0;

    for (let i = 0; i < count; i += 1) {
        if (visited[i]) continue;
        currentComponentId += 1;
        visited[i] = 1;
        componentIds[i] = currentComponentId - 1;

        const queue: number[] = [i];
        let head = 0;
        while (head < queue.length) {
            const current = queue[head]!;
            head += 1;
            for (const neighbor of adjacency[current]) {
                if (neighbor >= count || visited[neighbor]) continue;
                visited[neighbor] = 1;
                componentIds[neighbor] = currentComponentId - 1;
                queue.push(neighbor);
            }
        }
    }

    return componentIds;
}

export function selectConnectedComponent(options: {
    weights: Float32Array;
    componentIds: Uint32Array;
    targetComponentId: number;
    mode?: "add" | "subtract" | "replace";
    weightValue?: number;
}): Uint32Array {
    const targetValue = clamp01(options.weightValue ?? 1);
    const mode = options.mode ?? "add";
    const vertexCount = Math.min(options.weights.length, options.componentIds.length);
    if (vertexCount === 0) return new Uint32Array();

    const changed: number[] = [];
    for (let i = 0; i < vertexCount; i += 1) {
        const isTarget = options.componentIds[i] === options.targetComponentId;
        let nextWeight = options.weights[i];

        if (mode === "replace") {
            nextWeight = isTarget ? targetValue : 0;
        } else if (mode === "subtract") {
            if (isTarget) nextWeight = 0;
        } else {
            if (isTarget) nextWeight = Math.max(options.weights[i], targetValue);
        }

        if (nextWeight !== options.weights[i]) {
            options.weights[i] = nextWeight;
            changed.push(i);
        }
    }

    return Uint32Array.from(changed);
}
