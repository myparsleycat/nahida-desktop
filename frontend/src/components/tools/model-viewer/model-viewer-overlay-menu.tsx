import type {
  ViewerOverlayMenu,
  ViewerOverlayQuad,
  ViewerVariable,
} from "@shared/mod-viewer/types";
import type { CSSProperties } from "react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

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
  const { t } = useTranslation();
  const [pageIndex, setPageIndex] = useState(0);
  const [hovered, setHovered] = useState<number | null>(null);
  const page = menu.pages[pageIndex] ?? menu.pages[0];
  const hover = hovered === null ? undefined : page?.buttons[hovered]?.hover;
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
      {page?.quads.map((quad, index) => {
        const state = quad.states?.find(
          (entry) => Number(activeState[entry.variable]) === entry.value,
        );
        const style = {
          ...place(quad),
          opacity: state?.opacity ?? quad.opacity,
          transform:
            quad.flipX || quad.flipY
              ? `scale(${quad.flipX ? -1 : 1}, ${quad.flipY ? -1 : 1})`
              : undefined,
        };
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
      {page?.buttons.map((button, index) => {
        const label = button.variable
          ? variables.find((variable) => variable.id === button.variable)?.label
          : t("page.tools.model_viewer.overlay_switch_page");
        return (
          <button
            key={`${pageIndex}:${index}`}
            type="button"
            title={label}
            aria-label={label}
            className="absolute cursor-pointer"
            style={place(button)}
            onPointerEnter={() => setHovered(index)}
            onPointerLeave={() => setHovered(null)}
            onClick={() => {
              if (button.variable) {
                const current = Number(activeState[button.variable]);
                const next = button.transitions.find((entry) => entry.from === current);
                if (next) onSelect(button.variable, next.to);
              }
              if (button.page !== undefined) {
                setPageIndex(button.page);
                setHovered(null);
              }
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
