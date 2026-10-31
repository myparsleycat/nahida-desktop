import type {
  ViewerOverlayMenu,
  ViewerOverlayQuad,
  ViewerVariable,
} from "@shared/mod-viewer/types";
import type { CSSProperties } from "react";
import { useEffect, useRef, useState } from "react";

import type { VariableStateValue } from "./model-viewer-dialog-types";

import { modelViewerSourceToUrl } from "./model-viewer-session";

export function ModelViewerOverlayMenuPanel({
  menu,
  variables,
  activeState,
  onSelect,
}: {
  menu: ViewerOverlayMenu;
  variables: ViewerVariable[];
  activeState: Record<string, VariableStateValue>;
  onSelect: (variableId: string, value: VariableStateValue) => void;
}) {
  const [hovered, setHovered] = useState<number | null>(null);
  const hover = hovered === null ? undefined : menu.buttons[hovered]?.hover;
  const percent = (value: number, total: number) => `${(value / total) * 100}%`;
  const place = (rect: { x: number; y: number; width: number; height: number }) => ({
    left: percent(rect.x, menu.width),
    top: percent(rect.y, menu.height),
    width: percent(rect.width, menu.width),
    height: percent(rect.height, menu.height),
  });

  return (
    <div
      className="relative mx-auto mb-4 w-full select-none"
      style={{ maxWidth: menu.width, aspectRatio: `${menu.width} / ${menu.height}` }}
    >
      {menu.quads.map((quad, index) => {
        const state = quad.states?.find(
          (entry) => Number(activeState[entry.variable]) === entry.value,
        );
        const style = { ...place(quad), opacity: state?.opacity ?? quad.opacity };
        const url = modelViewerSourceToUrl(
          hover?.find((change) => change.quad === index)?.path ?? state?.path ?? quad.path,
        );
        return quad.flipbook ? (
          <FlipbookQuad key={index} flipbook={quad.flipbook} url={url} style={style} />
        ) : (
          <img
            key={index}
            src={url}
            alt=""
            draggable={false}
            className="pointer-events-none absolute"
            style={style}
          />
        );
      })}
      {menu.buttons.map((button, index) => {
        const label = variables.find((variable) => variable.id === button.variable)?.label;
        return (
          <button
            key={index}
            type="button"
            title={label}
            aria-label={label}
            className="absolute cursor-pointer"
            style={place(button)}
            onPointerEnter={() => setHovered(index)}
            onPointerLeave={() => setHovered(null)}
            onClick={() => {
              const current = Number(activeState[button.variable]);
              const next = button.transitions.find((entry) => entry.from === current);
              if (next) onSelect(button.variable, next.to);
            }}
          />
        );
      })}
    </div>
  );
}

function FlipbookQuad({
  flipbook,
  url,
  style,
}: {
  flipbook: NonNullable<ViewerOverlayQuad["flipbook"]>;
  url: string;
  style: CSSProperties;
}) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const element = ref.current;
    if (!element) return;

    const show = (frame: number) => {
      const column = frame % flipbook.columns;
      const row = Math.floor(frame / flipbook.columns);
      element.style.backgroundPosition = `${(column / Math.max(flipbook.columns - 1, 1)) * 100}% ${
        (row / Math.max(flipbook.rows - 1, 1)) * 100
      }%`;
    };
    show(flipbook.frameStart);
    const count = flipbook.frameEnd - flipbook.frameStart + 1;
    if (flipbook.fps <= 0 || count <= 1) return;

    let frame = 0;
    const timer = window.setInterval(() => {
      frame = (frame + 1) % count;
      show(flipbook.frameStart + frame);
    }, 1000 / flipbook.fps);
    return () => window.clearInterval(timer);
  }, [flipbook]);

  return (
    <div
      ref={ref}
      className="pointer-events-none absolute"
      style={{
        ...style,
        backgroundImage: `url("${url}")`,
        backgroundSize: `${flipbook.columns * 100}% ${flipbook.rows * 100}%`,
      }}
    />
  );
}
