export type Vec3 = [number, number, number];

export type FlowTrajectory = {
    points: Vec3[];
    times: number[];
    lifetime: number;
};

const add = (a: Vec3, b: Vec3): Vec3 => [a[0] + b[0], a[1] + b[1], a[2] + b[2]];
const sub = (a: Vec3, b: Vec3): Vec3 => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
const mul = (a: Vec3, s: number): Vec3 => [a[0] * s, a[1] * s, a[2] * s];
const dot = (a: Vec3, b: Vec3) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
const cross = (a: Vec3, b: Vec3): Vec3 => [
    a[1] * b[2] - a[2] * b[1],
    a[2] * b[0] - a[0] * b[2],
    a[0] * b[1] - a[1] * b[0],
];
const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value));

export function normalize(value: Vec3): Vec3 {
    return mul(value, 1 / Math.max(Math.hypot(...value), 1e-10));
}

function smooth(edge0: number, edge1: number, x: number) {
    const t = clamp((x - edge0) / (edge1 - edge0), 0, 1);
    return t * t * (3 - 2 * t);
}

/**
 * The mean path of an emitter's force in source mesh space (Z up), ported from
 * LiquidGravityProbe's AttachmentEditor; force 1 is the runtime's former
 * default strength (1.69), as in Slots.hlsl slotDrive. For the spray nozzle it
 * includes the nozzle exit correction, the high-pressure jet boost and
 * near-field gravity relief; the jet nozzle's speed follows SoftNozzle.hlsl
 * shapeJetNozzle under plain gravity. It ignores collision, motion, pulses and
 * particle interaction.
 */
export function flowTrajectory(
    point: Vec3,
    offset: Vec3,
    direction: Vec3,
    force: number,
    nozzle: "spray" | "jet" = "spray",
): FlowTrajectory {
    const setting = clamp(1.69 * force, 0.28, 3.94);
    const lifetime = 0.48;
    const step = 1 / 240;
    const jet = nozzle === "jet";
    const speed = jet ? clamp(3 * Math.sqrt(setting / 1.69), 0.8, 5) : sprayNozzleSpeed(setting);

    const dir = normalize(direction);
    const up = mul(dir, -1);
    let reference: Vec3 = [0, -1, 0];
    if (Math.abs(dot(up, reference)) > 0.95) reference = [1, 0, 0];
    const right = normalize(cross(up, reference));
    const forward = normalize(cross(right, up));

    let position = add(add(point, offset), add(mul(dir, 0.004), mul(forward, -0.001)));
    let velocity = mul(dir, speed);
    let travel = 0;
    const points: Vec3[] = [position];
    const times = [0];
    for (let time = 0; time < lifetime - 1e-9;) {
        const h = Math.min(step, lifetime - time);
        const gravity = jet
            ? -9.81
            : -9.81 * (1 - 0.45 * smooth(0.4, 1.2, speed) * (1 - smooth(0.12, 0.3, travel)));
        const next = add(position, add(mul(velocity, h), [0, 0, 0.5 * gravity * h * h]));
        velocity = add(velocity, [0, 0, gravity * h]);
        travel += Math.hypot(...sub(next, position));
        position = next;
        time += h;
        points.push(position);
        times.push(time);
    }
    return { points, times, lifetime };
}

function sprayNozzleSpeed(setting: number) {
    const opening = clamp(0.65 + 0.35 * Math.sqrt(setting), 0.8, 1.75);
    const discharge = 0.78 + 0.22 * clamp((opening - 0.7) / 0.7, 0, 1);
    const jetBoost = 1 + 0.35 * smooth(1.69, 3.94, setting);
    return (0.3807886553 * Math.sqrt(setting) * discharge * jetBoost) / (0.78 + 0.22 * (0.3 / 0.7));
}

/** Position of a tracer that left the nozzle `age` seconds ago. */
export function trajectoryPointAt(trajectory: FlowTrajectory, age: number): Vec3 {
    const { points, times } = trajectory;
    let index = 0;
    while (index < times.length - 2 && times[index + 1] < age) index++;
    const span = times[index + 1] - times[index];
    const fraction = span > 0 ? clamp((age - times[index]) / span, 0, 1) : 0;
    return add(points[index], mul(sub(points[index + 1], points[index]), fraction));
}
