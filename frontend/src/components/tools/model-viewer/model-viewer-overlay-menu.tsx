import type {
  ViewerOverlayMenu,
  ViewerOverlayQuad,
  ViewerOverlaySlider,
  ViewerVariable,
} from "@shared/mod-viewer/types";
import type { CSSProperties, PointerEvent as ReactPointerEvent } from "react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { VariableStateValue } from "./model-viewer-dialog-types";

import { modelViewerSourceToUrl } from "./model-viewer-session";

// Menus are authored against this window; see modelViewerOverlayWidth in the backend.
const REFERENCE_WIDTH = 1920;
const REFERENCE_HEIGHT = 1080;
const DRAG_THRESHOLD = 4;
const MIN_SIZE = 0.5;
const MAX_SIZE = 3;
const WHEEL_SIZE_RATE = 0.001;
const KEYBOARD_SIZE_STEP = 0.05;

type OverlayMenuPanelProps = {
  menu: ViewerOverlayMenu;
  variables: ViewerVariable[];
  activeState: Record<string, VariableStateValue>;
  onSelect: (variableId: string, value: VariableStateValue) => void;
  page: number;
  onPageChange: (page: number) => void;
};

/**
 * x and y are fractions of the viewer's size and size multiplies the size the menu
 * has in the game, so a resized viewer keeps the menu in proportion.
 */
export type ModelViewerOverlayPlacement = { x: number; y: number; size: number };

export function ModelViewerFloatingOverlayMenu({
  placement,
  onPlacementChange,
  ...panel
}: OverlayMenuPanelProps & {
  placement?: ModelViewerOverlayPlacement;
  onPlacementChange: (placement: ModelViewerOverlayPlacement) => void;
}) {
  const { t } = useTranslation();
  const menu = panel.menu;
  const layerRef = useRef<HTMLDivElement>(null);
  const [view, setView] = useState({ width: 0, height: 0 });
  const [draft, setDraft] = useState<ModelViewerOverlayPlacement | null>(null);
  const dragRef = useRef<{
    pointerId: number;
    startX: number;
    startY: number;
    startLeft: number;
    startTop: number;
    moved: boolean;
  } | null>(null);
  const resizeRef = useRef<{
    pointerId: number;
    startX: number;
    startY: number;
    startSize: number;
  } | null>(null);
  const suppressClickRef = useRef(false);
  const wheelRef = useRef<(event: WheelEvent) => void>(() => {});
  const pendingWheelRef = useRef<ModelViewerOverlayPlacement | null>(null);

  useEffect(() => {
    const layer = layerRef.current;
    if (!layer) return;

    const observer = new ResizeObserver(([entry]) => {
      setView({ width: entry.contentRect.width, height: entry.contentRect.height });
    });
    observer.observe(layer);
    return () => observer.disconnect();
  }, []);

  const scale = Math.min(view.width / REFERENCE_WIDTH, view.height / REFERENCE_HEIGHT);
  const baseWidth = menu.width * scale;
  const baseHeight = menu.height * scale;
  const clampSize = (next: number, roomWidth = view.width, roomHeight = view.height) => {
    const largest = Math.min(MAX_SIZE, roomWidth / baseWidth, roomHeight / baseHeight);
    return Math.max(Math.min(next, largest), Math.min(MIN_SIZE, largest));
  };
  const placedAt = (nextLeft: number, nextTop: number, nextSize: number) => ({
    x: Math.max(Math.min(nextLeft, view.width - baseWidth * nextSize), 0) / view.width,
    y: Math.max(Math.min(nextTop, view.height - baseHeight * nextSize), 0) / view.height,
    size: nextSize,
  });
  const shown = draft ??
    placement ?? { x: menu.x / REFERENCE_WIDTH, y: menu.y / REFERENCE_HEIGHT, size: 1 };
  const size = clampSize(shown.size);
  const { x, y } = placedAt(shown.x * view.width, shown.y * view.height, size);
  const left = x * view.width;
  const top = y * view.height;
  const width = baseWidth * size;
  const dragged = (
    drag: NonNullable<typeof dragRef.current>,
    event: { clientX: number; clientY: number },
  ) =>
    placedAt(
      drag.startLeft + event.clientX - drag.startX,
      drag.startTop + event.clientY - drag.startY,
      size,
    );
  // Follows the diagonal, since a flat menu would jump if the pointer's height set its size.
  const resized = (
    resize: NonNullable<typeof resizeRef.current>,
    event: { clientX: number; clientY: number },
  ) => {
    const grown =
      (event.clientX - resize.startX) * baseWidth + (event.clientY - resize.startY) * baseHeight;
    const next = resize.startSize + grown / (baseWidth * baseWidth + baseHeight * baseHeight);
    return placedAt(left, top, clampSize(next, view.width - left, view.height - top));
  };
  const cancelDrag = () => {
    dragRef.current = null;
    resizeRef.current = null;
    setDraft(null);
  };

  useLayoutEffect(() => {
    pendingWheelRef.current = null;
    wheelRef.current = (event) => {
      if (!event.ctrlKey) return;
      event.preventDefault();
      if (dragRef.current || resizeRef.current) return;

      // Several events can arrive before the next render, and each continues from the last.
      const pending = pendingWheelRef.current;
      const fromSize = pending?.size ?? size;
      const fromLeft = pending ? pending.x * view.width : left;
      const fromTop = pending ? pending.y * view.height : top;
      const next = clampSize(fromSize * Math.exp(-event.deltaY * WHEEL_SIZE_RATE));
      if (next === fromSize) return;

      // The point under the cursor stays there, so a button being aimed at does not run away.
      const bounds = layerRef.current?.getBoundingClientRect();
      const cursorX = event.clientX - (bounds?.left ?? 0);
      const cursorY = event.clientY - (bounds?.top ?? 0);
      const ratio = next / fromSize;
      pendingWheelRef.current = placedAt(
        cursorX - (cursorX - fromLeft) * ratio,
        cursorY - (cursorY - fromTop) * ratio,
        next,
      );
      onPlacementChange(pendingWheelRef.current);
    };
  });

  // React registers wheel listeners as passive, which could not stop the page zoom of Ctrl + wheel.
  useEffect(() => {
    const layer = layerRef.current;
    if (!layer) return;

    const onWheel = (event: WheelEvent) => wheelRef.current(event);
    layer.addEventListener("wheel", onWheel, { passive: false });
    return () => layer.removeEventListener("wheel", onWheel);
  }, []);

  return (
    <div
      ref={layerRef}
      className="pointer-events-none absolute inset-0 z-[5] overflow-hidden"
      data-testid="model-viewer-floating-overlay"
    >
      {width > 0 ? (
        <div
          className="group pointer-events-auto absolute cursor-grab touch-none active:cursor-grabbing"
          style={{ left, top, width }}
          onPointerDown={(event) => {
            suppressClickRef.current = false;
            if (event.button !== 0) return;
            dragRef.current = {
              pointerId: event.pointerId,
              startX: event.clientX,
              startY: event.clientY,
              startLeft: left,
              startTop: top,
              moved: false,
            };
          }}
          onPointerMove={(event) => {
            const drag = dragRef.current;
            if (drag?.pointerId !== event.pointerId) return;

            // Without capture yet, a release outside the menu never reaches pointerup.
            if ((event.buttons & 1) === 0) {
              cancelDrag();
              return;
            }
            if (!drag.moved) {
              const distance = Math.hypot(event.clientX - drag.startX, event.clientY - drag.startY);
              if (distance <= DRAG_THRESHOLD) return;

              // Capturing only now leaves a press that stays put to the button under it.
              drag.moved = true;
              event.currentTarget.setPointerCapture(event.pointerId);
            }
            setDraft(dragged(drag, event));
          }}
          onPointerUp={(event) => {
            const drag = dragRef.current;
            if (drag?.pointerId !== event.pointerId) return;
            dragRef.current = null;
            if (!drag.moved) return;
            suppressClickRef.current = true;
            onPlacementChange(dragged(drag, event));
            setDraft(null);
          }}
          onPointerCancel={cancelDrag}
          onLostPointerCapture={() => {
            if (dragRef.current || resizeRef.current) cancelDrag();
          }}
          onClickCapture={(event) => {
            if (!suppressClickRef.current) return;
            suppressClickRef.current = false;
            event.stopPropagation();
          }}
        >
          <ModelViewerOverlayMenuPanel {...panel} />
          <div
            role="slider"
            tabIndex={0}
            title={t("page.tools.model_viewer.overlay_resize")}
            aria-label={t("page.tools.model_viewer.overlay_resize")}
            aria-valuemin={MIN_SIZE * 100}
            aria-valuemax={MAX_SIZE * 100}
            aria-valuenow={Math.round(size * 100)}
            className="absolute right-0 bottom-0 flex size-4 cursor-nwse-resize touch-none items-end justify-end rounded-tl-sm bg-background/70 p-0.5 opacity-0 outline-none group-hover:opacity-100 focus-visible:opacity-100"
            onPointerDown={(event) => {
              event.stopPropagation();
              if (event.button !== 0) return;
              event.currentTarget.setPointerCapture(event.pointerId);
              resizeRef.current = {
                pointerId: event.pointerId,
                startX: event.clientX,
                startY: event.clientY,
                startSize: size,
              };
            }}
            onPointerMove={(event) => {
              const resize = resizeRef.current;
              if (resize?.pointerId === event.pointerId) setDraft(resized(resize, event));
            }}
            onPointerUp={(event) => {
              const resize = resizeRef.current;
              if (resize?.pointerId !== event.pointerId) return;
              resizeRef.current = null;
              onPlacementChange(resized(resize, event));
              setDraft(null);
            }}
            onKeyDown={(event) => {
              const step =
                event.key === "ArrowRight" || event.key === "ArrowUp"
                  ? KEYBOARD_SIZE_STEP
                  : event.key === "ArrowLeft" || event.key === "ArrowDown"
                    ? -KEYBOARD_SIZE_STEP
                    : 0;
              if (step === 0) return;
              event.preventDefault();
              onPlacementChange(
                placedAt(left, top, clampSize(size + step, view.width - left, view.height - top)),
              );
            }}
          >
            <div className="size-2 border-r-2 border-b-2 border-foreground/80" />
          </div>
        </div>
      ) : null}
    </div>
  );
}

export function ModelViewerOverlayMenuPanel({
  menu,
  variables,
  activeState,
  onSelect,
  page: pageIndex,
  onPageChange,
}: OverlayMenuPanelProps) {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
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
      className="relative w-full overflow-hidden select-none"
      style={{ aspectRatio: `${menu.width} / ${menu.height}` }}
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
            className="pointer-events-none absolute max-w-none"
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
                onPageChange(button.page);
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
        // In the viewer a press elsewhere on the menu starts moving the menu.
        event.stopPropagation();
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
