import { OrbitControls } from "@react-three/drei";
import { Canvas, useThree } from "@react-three/fiber";
import {
  applyMultiRegionDeform,
  composeDisplayWeights,
  writeWeightColors,
  type ActiveRegionDeform,
} from "@shared/body-shape";
import { useEffect, useMemo, useRef } from "react";
import {
  BufferAttribute,
  BufferGeometry,
  Color,
  DoubleSide,
  Mesh,
  MeshStandardMaterial,
} from "three";

export type BodyShapeViewportProps = {
  originalPositions: Float32Array;
  previewPositions: Float32Array;
  regions: ActiveRegionDeform[];
  indices?: Uint32Array;
  showOriginal: boolean;
  showWeights: boolean;
  /** Bumped when region selection or amounts change. */
  weightVersion: number;
};

export function BodyShapeViewport(props: BodyShapeViewportProps) {
  return (
    <div className="h-full min-h-[320px] w-full rounded-md border border-border bg-background">
      <Canvas
        camera={{ position: [0, 1.2, 2.4], fov: 45, near: 0.01, far: 500 }}
        dpr={Math.min(window.devicePixelRatio, 2)}
        gl={{ antialias: true, alpha: true }}
      >
        <color attach="background" args={["#12141a"]} />
        <ambientLight intensity={0.55} />
        <hemisphereLight intensity={0.7} groundColor="#3a3f4a" position={[0, 1, 0]} />
        <directionalLight intensity={1.4} position={[4, 6, 5]} />
        <directionalLight intensity={0.5} position={[-4, 2, -3]} />
        <BodyShapeMesh {...props} />
        <OrbitControls makeDefault enableDamping dampingFactor={0.12} />
      </Canvas>
    </div>
  );
}

function BodyShapeMesh({
  originalPositions,
  previewPositions,
  regions,
  indices,
  showOriginal,
  showWeights,
  weightVersion,
}: BodyShapeViewportProps) {
  const meshRef = useRef<Mesh>(null);
  const colorsRef = useRef(new Float32Array(Math.floor(originalPositions.length / 3) * 3));
  const { camera } = useThree();

  const geometry = useMemo(() => {
    const geo = new BufferGeometry();
    const positionAttr = new BufferAttribute(previewPositions, 3);
    positionAttr.setUsage(35048);
    geo.setAttribute("position", positionAttr);

    const colorAttr = new BufferAttribute(colorsRef.current, 3);
    colorAttr.setUsage(35048);
    geo.setAttribute("color", colorAttr);

    if (indices && indices.length > 0) {
      geo.setIndex(new BufferAttribute(indices, 1));
    }
    geo.computeVertexNormals();
    geo.computeBoundingSphere();
    return geo;
  }, [indices, originalPositions, previewPositions]);

  useEffect(() => {
    const vertexCount = Math.floor(originalPositions.length / 3);
    if (colorsRef.current.length !== vertexCount * 3) {
      colorsRef.current = new Float32Array(vertexCount * 3);
      geometry.setAttribute("color", new BufferAttribute(colorsRef.current, 3));
    }

    if (showOriginal || regions.length === 0) {
      previewPositions.set(originalPositions);
    } else {
      applyMultiRegionDeform({
        originalPositions,
        previewPositions,
        regions,
      });
    }

    const positionAttr = geometry.getAttribute("position") as BufferAttribute;
    positionAttr.needsUpdate = true;

    const displayWeights = composeDisplayWeights(vertexCount, showOriginal ? [] : regions);
    writeWeightColors(displayWeights, colorsRef.current);
    const colorAttr = geometry.getAttribute("color") as BufferAttribute;
    colorAttr.needsUpdate = true;

    geometry.computeVertexNormals();
    geometry.computeBoundingSphere();
  }, [geometry, originalPositions, previewPositions, regions, showOriginal, weightVersion]);

  const material = useMemo(() => {
    return new MeshStandardMaterial({
      vertexColors: showWeights,
      color: showWeights ? new Color("#ffffff") : new Color("#9aa4b2"),
      roughness: 0.55,
      metalness: 0.05,
      side: DoubleSide,
      flatShading: false,
    });
  }, [showWeights]);

  useEffect(() => {
    return () => {
      geometry.dispose();
      material.dispose();
    };
  }, [geometry, material]);

  useEffect(() => {
    if (!geometry.boundingSphere) return;
    const radius = Math.max(geometry.boundingSphere.radius, 0.1);
    camera.position.set(radius * 1.6, radius * 1.1, radius * 2.2);
    camera.lookAt(geometry.boundingSphere.center);
    camera.updateProjectionMatrix();
  }, [camera, geometry, originalPositions]);

  return <mesh ref={meshRef} geometry={geometry} material={material} />;
}
