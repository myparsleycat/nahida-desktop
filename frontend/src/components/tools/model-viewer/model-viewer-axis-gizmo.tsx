import { cn } from "@renderer/lib/utils";
import {
  forwardRef,
  Fragment,
  useCallback,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
} from "react";
import { useTranslation } from "react-i18next";
import { Quaternion, Vector3 } from "three";

export type ModelViewerAxisView = {
  id: "+x" | "-x" | "+y" | "-y" | "+z" | "-z";
  phi: number;
  theta: number;
};

export type ModelViewerAxisGizmoHandle = {
  update: (cameraQuaternion: Quaternion) => void;
};

// OrbitControls cannot hold an exact pole, so top and bottom views stop just short of it.
const POLE_PHI = 0.001;
const GIZMO_SIZE = 88;
const AXIS_RADIUS = 30;

const AXES = [
  {
    color: "#ff3352",
    direction: new Vector3(1, 0, 0),
    label: "X",
    positive: { id: "+x", phi: Math.PI / 2, theta: Math.PI / 2 },
    negative: { id: "-x", phi: Math.PI / 2, theta: -Math.PI / 2 },
  },
  {
    color: "#8bdc00",
    direction: new Vector3(0, 1, 0),
    label: "Y",
    positive: { id: "+y", phi: POLE_PHI, theta: 0 },
    negative: { id: "-y", phi: Math.PI - POLE_PHI, theta: 0 },
  },
  {
    color: "#2890ff",
    direction: new Vector3(0, 0, 1),
    label: "Z",
    positive: { id: "+z", phi: Math.PI / 2, theta: 0 },
    negative: { id: "-z", phi: Math.PI / 2, theta: Math.PI },
  },
] as const satisfies ReadonlyArray<{
  color: string;
  direction: Vector3;
  label: string;
  positive: ModelViewerAxisView;
  negative: ModelViewerAxisView;
}>;

export const ModelViewerAxisGizmo = forwardRef<
  ModelViewerAxisGizmoHandle,
  { onSelect: (view: ModelViewerAxisView) => void }
>(function ModelViewerAxisGizmo({ onSelect }, ref) {
  const { t } = useTranslation();
  const linesRef = useRef<Array<HTMLDivElement | null>>([]);
  const positiveHeadsRef = useRef<Array<HTMLButtonElement | null>>([]);
  const negativeHeadsRef = useRef<Array<HTMLButtonElement | null>>([]);

  const update = useCallback((cameraQuaternion: Quaternion) => {
    const toCamera = cameraQuaternion.clone().invert();
    AXES.forEach((axis, index) => {
      const projected = axis.direction.clone().applyQuaternion(toCamera);
      const x = projected.x * AXIS_RADIUS;
      const y = -projected.y * AXIS_RADIUS;
      const positiveHead = positiveHeadsRef.current[index];
      const negativeHead = negativeHeadsRef.current[index];
      const line = linesRef.current[index];

      if (positiveHead) {
        positiveHead.style.transform = `translate(${x}px, ${y}px)`;
        positiveHead.style.zIndex = depthToZIndex(projected.z);
      }
      if (negativeHead) {
        negativeHead.style.transform = `translate(${-x}px, ${-y}px)`;
        negativeHead.style.zIndex = depthToZIndex(-projected.z);
      }
      if (line) {
        line.style.width = `${Math.hypot(x, y)}px`;
        line.style.transform = `rotate(${Math.atan2(y, x)}rad)`;
        line.style.zIndex = depthToZIndex(projected.z / 2);
      }
    });
  }, []);

  useImperativeHandle(ref, () => ({ update }), [update]);

  // The first camera frame arrives after mount; show the default orientation until then.
  useLayoutEffect(() => {
    update(new Quaternion());
  }, [update]);

  return (
    <div
      className="absolute top-3 right-3 z-[5] rounded-full transition-colors hover:bg-foreground/15"
      style={{ height: GIZMO_SIZE, width: GIZMO_SIZE }}
    >
      {AXES.map((axis, index) => (
        <Fragment key={axis.label}>
          <div
            ref={(element) => {
              linesRef.current[index] = element;
            }}
            aria-hidden="true"
            className="absolute top-1/2 left-1/2 -mt-px h-0.5 origin-left"
            style={{ backgroundColor: axis.color }}
          />
          <AxisHead
            ref={(element) => {
              positiveHeadsRef.current[index] = element;
            }}
            color={axis.color}
            label={t("page.tools.model_viewer.axis_view", { axis: `+${axis.label}` })}
            onClick={() => onSelect(axis.positive)}
          >
            {axis.label}
          </AxisHead>
          <AxisHead
            ref={(element) => {
              negativeHeadsRef.current[index] = element;
            }}
            color={axis.color}
            label={t("page.tools.model_viewer.axis_view", { axis: `-${axis.label}` })}
            negative
            onClick={() => onSelect(axis.negative)}
          />
        </Fragment>
      ))}
    </div>
  );
});

const AxisHead = forwardRef<
  HTMLButtonElement,
  {
    children?: string;
    color: string;
    label: string;
    negative?: boolean;
    onClick: () => void;
  }
>(function AxisHead({ children, color, label, negative, onClick }, ref) {
  return (
    <button
      ref={ref}
      type="button"
      aria-label={label}
      className="absolute top-1/2 left-1/2 -mt-2.5 -ml-2.5 flex size-5 cursor-pointer items-center justify-center rounded-full"
      onClick={onClick}
    >
      <span
        className={cn(
          "flex items-center justify-center rounded-full transition-transform hover:scale-125",
          negative
            ? "size-3.5 border-2 opacity-70"
            : "size-5 text-[11px] leading-none font-bold text-black",
        )}
        style={
          negative
            ? { backgroundColor: `${color}55`, borderColor: color }
            : { backgroundColor: color }
        }
      >
        {children}
      </span>
    </button>
  );
});

function depthToZIndex(depth: number) {
  return String(Math.round((depth + 1) * 100));
}
