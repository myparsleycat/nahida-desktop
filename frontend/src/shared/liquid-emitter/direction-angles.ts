import { normalize, type Vec3 } from "./flow-trajectory";

export type DirectionAngles = {
    /** Degrees away from straight down: 0 is down, 90 horizontal, 180 up. */
    tilt: number;
    /** Degrees counterclockwise seen from above, from the mesh's horizontal X axis. */
    azimuth: number;
};

const RADIANS = Math.PI / 180;

export function directionToAngles(direction: Vec3, down: Vec3): DirectionAngles {
    const [axis, side, across] = frame(down);
    const unit = normalize(direction);
    const tilt = Math.acos(Math.min(Math.max(dot(unit, axis), -1), 1)) / RADIANS;
    const azimuth = Math.atan2(dot(unit, across), dot(unit, side)) / RADIANS;
    return { tilt, azimuth: (azimuth + 360) % 360 };
}

export function anglesToDirection({ tilt, azimuth }: DirectionAngles, down: Vec3): Vec3 {
    const [axis, side, across] = frame(down);
    const vertical = Math.cos(tilt * RADIANS);
    const horizontal = Math.sin(tilt * RADIANS);
    const cos = Math.cos(azimuth * RADIANS);
    const sin = Math.sin(azimuth * RADIANS);
    return [0, 1, 2].map(
        (index) => axis[index] * vertical + (side[index] * cos + across[index] * sin) * horizontal,
    ) as Vec3;
}

// Down, the horizontal part of the mesh's X axis (Z when down lies along X),
// and the axis a quarter turn counterclockwise from it seen from above.
function frame(down: Vec3): [Vec3, Vec3, Vec3] {
    const axis = normalize(down);
    const reference: Vec3 = Math.abs(axis[0]) > 0.9 ? [0, 0, 1] : [1, 0, 0];
    const along = dot(reference, axis);
    const side = normalize([
        reference[0] - axis[0] * along,
        reference[1] - axis[1] * along,
        reference[2] - axis[2] * along,
    ]);
    const across: Vec3 = [
        side[1] * axis[2] - side[2] * axis[1],
        side[2] * axis[0] - side[0] * axis[2],
        side[0] * axis[1] - side[1] * axis[0],
    ];
    return [axis, side, across];
}

function dot(a: Vec3, b: Vec3) {
    return a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
}
