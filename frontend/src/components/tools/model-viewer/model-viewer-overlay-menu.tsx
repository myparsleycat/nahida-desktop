import type {
  ViewerOverlayMenu,
  ViewerOverlayQuad,
  ViewerOverlaySlider,
  ViewerVariable,
} from "@shared/mod-viewer/types";
import type { CSSProperties, PointerEvent as ReactPointerEvent } from "react";
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
  const containerRef = useRef<HTMLDivElement>(null);
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
  const sliders = (page?.sliders ?? []).flatMap((slider) => {
    const mode =
      slider.modes.find((entry) => Number(activeState[entry.whenVariable]) === entry.whenValue) ??
      slider.modes[0];
    const handle = page?.quads[slider.handle];
    const track = slider.parts.find((part) => part.quad === slider.handle);
    return mode && handle && track ? [{ slider, mode, handle, track }] : [];
  });
  const sliderPlacement = new Map(
    sliders.flatMap(({ slider, mode }) => {
      const position = sliderPosition(Number(activeState[mode.variable]), mode);
      return slider.parts.map((part) => [
        part.quad,
        {
          x: part.startX + (part.endX - part.startX) * position,
          width: part.startWidth + (part.endWidth - part.startWidth) * position,
        },
      ]);
    }),
  );
  const menuX = (clientX: number) => {
    const bounds = containerRef.current?.getBoundingClientRect();
    return bounds && bounds.width > 0 ? ((clientX - bounds.left) / bounds.width) * menu.width : 0;
  };

  return (
    <div
      ref={containerRef}
      className="relative mx-auto mb-4 w-full select-none"
      style={{ maxWidth: menu.width, aspectRatio: `${menu.width} / ${menu.height}` }}
    >
      {page?.quads.map((quad, index) => {
        const state = quad.states?.find(
          (entry) => Number(activeState[entry.variable]) === entry.value,
        );
        const style = {
          ...place({ ...quad, ...sliderPlacement.get(index) }),
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
          ? (variables.find((variable) => variable.id === button.variable)?.label ?? button.label)
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
      {sliders.map(({ slider, mode, handle, track }) => {
        const position = sliderPosition(Number(activeState[mode.variable]), mode);
        const select = (next: number) => {
          const clamped = Math.min(Math.max(next, 0), 1);
          onSelect(mode.variable, mode.min + (mode.max - mode.min) * clamped);
        };
        return (
          <OverlaySliderHandle
            key={`${pageIndex}:${slider.handle}`}
            label={mode.label}
            value={Number(activeState[mode.variable])}
            min={mode.min}
            max={mode.max}
            style={place({ ...handle, ...sliderPlacement.get(slider.handle) })}
            position={position}
            startX={track.startX}
            endX={track.endX}
            menuX={menuX}
            onChange={select}
          />
        );
      })}
    </div>
  );
}

function sliderPosition(
  value: number,
  mode: Pick<ViewerOverlaySlider["modes"][number], "min" | "max">,
): number {
  const position = (value - mode.min) / (mode.max - mode.min);
  return Number.isFinite(position) ? Math.min(Math.max(position, 0), 1) : 0;
}

function OverlaySliderHandle({
  label,
  value,
  min,
  max,
  style,
  position,
  startX,
  endX,
  menuX,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  style: CSSProperties;
  position: number;
  startX: number;
  endX: number;
  menuX: (clientX: number) => number;
  onChange: (position: number) => void;
}) {
  const grabRef = useRef<number | null>(null);
  const toPosition = (event: ReactPointerEvent<HTMLDivElement>) =>
    (menuX(event.clientX) - (grabRef.current ?? 0) - startX) / (endX - startX);

  return (
    <div
      role="slider"
      tabIndex={0}
      title={label}
      aria-label={label}
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={value}
      className="absolute cursor-grab touch-none active:cursor-grabbing"
      style={style}
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId);
        grabRef.current = menuX(event.clientX) - (startX + (endX - startX) * position);
      }}
      onPointerMove={(event) => {
        if (grabRef.current !== null) onChange(toPosition(event));
      }}
      onPointerUp={() => {
        grabRef.current = null;
      }}
      onPointerCancel={() => {
        grabRef.current = null;
      }}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft") onChange(position - 0.01);
        if (event.key === "ArrowRight") onChange(position + 0.01);
      }}
    />
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
