import { useFrame, useThree } from "@react-three/fiber";
import {
  flowTrajectory,
  normalize,
  trajectoryPointAt,
  type Vec3,
} from "@shared/liquid-emitter/flow-trajectory";
import { useEffect, useMemo, useRef } from "react";
import { BufferGeometry, type Group, Line, LineBasicMaterial, Quaternion, Vector3 } from "three";

const ANCHOR_COLOR = "#ffc240";
// The mod's other emitters, shown for reference while one is edited.
const MUTED_COLOR = "#9a9a9a";
const PATH_COLOR = "#4de8ff";
const BEAD_COLOR = "#ffde14";
const ARROW_LENGTH = 0.18;
const BEAD_COUNT = 24;
// Tracers play at a tenth of real time so the path can be followed by eye.
const PREVIEW_SPEED = 0.1;

const overlayMaterial = { depthTest: false, depthWrite: false, transparent: true } as const;

export function LiquidEmitterOverlay({
  point,
  offset,
  direction,
  down,
  meshUnit,
  force = 1,
  nozzle = "spray",
  showTrajectory,
  muted = false,
}: {
  point: Vec3;
  offset: Vec3;
  direction: Vec3;
  /** The mesh's down axis, along which the previewed path falls. */
  down: Vec3;
  /** Meters per mesh unit; the overlay and the flow it previews are sized in meters. */
  meshUnit: number;
  /** The emitter's force, which the previewed path follows. */
  force?: number;
  /** The emitter's nozzle kind; an oozing one runs along the surface and has no path to preview. */
  nozzle?: string;
  showTrajectory: boolean;
  muted?: boolean;
}) {
  const color = muted ? MUTED_COLOR : ANCHOR_COLOR;
  const [pointMeters, offsetMeters] = useMemo(
    () => [toMeters(point, meshUnit), toMeters(offset, meshUnit)],
    [point, offset, meshUnit],
  );
  const origin = useMemo<Vec3>(
    () => [
      pointMeters[0] + offsetMeters[0],
      pointMeters[1] + offsetMeters[1],
      pointMeters[2] + offsetMeters[2],
    ],
    [pointMeters, offsetMeters],
  );
  const arrowRotation = useMemo(
    () =>
      new Quaternion().setFromUnitVectors(
        new Vector3(0, 1, 0),
        new Vector3(...normalize(direction)),
      ),
    [direction],
  );

  return (
    <group renderOrder={1000} scale={1 / meshUnit}>
      <mesh position={origin} renderOrder={1000}>
        <sphereGeometry args={[0.006, 16, 12]} />
        <meshBasicMaterial color={color} {...overlayMaterial} />
      </mesh>
      <group position={origin} quaternion={arrowRotation}>
        <mesh position={[0, ARROW_LENGTH * 0.4, 0]} renderOrder={1000}>
          <cylinderGeometry args={[0.0015, 0.0015, ARROW_LENGTH * 0.8, 8]} />
          <meshBasicMaterial color={color} {...overlayMaterial} />
        </mesh>
        <mesh position={[0, ARROW_LENGTH * 0.9, 0]} renderOrder={1000}>
          <coneGeometry args={[0.008, ARROW_LENGTH * 0.2, 12]} />
          <meshBasicMaterial color={color} {...overlayMaterial} />
        </mesh>
      </group>
      {showTrajectory && !muted && nozzle !== "ooze" ? (
        <FlowPreview
          point={pointMeters}
          offset={offsetMeters}
          direction={direction}
          down={down}
          force={force}
          nozzle={nozzle === "jet" ? "jet" : "spray"}
        />
      ) : null}
    </group>
  );
}

function toMeters(value: Vec3, meshUnit: number): Vec3 {
  return [value[0] * meshUnit, value[1] * meshUnit, value[2] * meshUnit];
}

function FlowPreview({
  point,
  offset,
  direction,
  down,
  force,
  nozzle,
}: {
  point: Vec3;
  offset: Vec3;
  direction: Vec3;
  down: Vec3;
  force: number;
  nozzle: "spray" | "jet";
}) {
  const trajectory = useMemo(
    () => flowTrajectory(point, offset, direction, down, force, nozzle),
    [point, offset, direction, down, force, nozzle],
  );
  const path = useMemo(() => {
    const line = new Line(
      new BufferGeometry().setFromPoints(trajectory.points.map((p) => new Vector3(...p))),
      new LineBasicMaterial({ color: PATH_COLOR, ...overlayMaterial }),
    );
    line.renderOrder = 1000;
    return line;
  }, [trajectory]);
  useEffect(
    () => () => {
      path.geometry.dispose();
      path.material.dispose();
    },
    [path],
  );

  const beadsRef = useRef<Group | null>(null);
  const clockRef = useRef(0);
  const invalidate = useThree((state) => state.invalidate);

  // The canvas renders on demand, so keep requesting frames while tracers move.
  useEffect(() => {
    const timer = window.setInterval(() => invalidate(), 1000 / 30);
    return () => window.clearInterval(timer);
  }, [invalidate]);

  useFrame((_, delta) => {
    clockRef.current =
      (clockRef.current + Math.min(delta, 0.05) * PREVIEW_SPEED) % trajectory.lifetime;
    beadsRef.current?.children.forEach((bead, index) => {
      const age =
        (clockRef.current + (index * trajectory.lifetime) / BEAD_COUNT) % trajectory.lifetime;
      bead.position.set(...trajectoryPointAt(trajectory, age));
    });
  });

  return (
    <>
      <primitive object={path} />
      <group ref={beadsRef}>
        {Array.from({ length: BEAD_COUNT }, (_, index) => (
          <mesh key={index} renderOrder={1000}>
            <sphereGeometry args={[0.0025, 8, 6]} />
            <meshBasicMaterial color={BEAD_COLOR} {...overlayMaterial} />
          </mesh>
        ))}
      </group>
    </>
  );
}
