import { Button } from "@renderer/components/ui/button";
import { ButtonGroup } from "@renderer/components/ui/button-group";
import { DialogFooter } from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { Logger } from "@renderer/lib/logger";
import { drawCoveredImage, MENU_MAKER_CROP_PREVIEW_SIZE } from "@shared/menu-maker/crop";
import {
  acceptsIconResult,
  clearIconifyCache,
  deleteIconifyPrefix,
  downloadIconifyCollection,
  getFavoriteIconifyPrefixes,
  getIconifyCacheStats,
  sanitizeIconifySVG,
  searchCachedIconifyIcons,
  searchIconifyIcons,
  searchLucideIcons,
  toggleFavoriteIconifyPrefix,
  type IconSearchResult,
  type IconifyCacheStats,
} from "@shared/menu-maker/icons";
import { type MenuMakerSlot } from "@shared/menu-maker/types";
import { Loader2Icon, SearchIcon, StarIcon, XIcon } from "lucide-react";
import { DynamicIcon, iconNames } from "lucide-react/dynamic";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { Modal } from "./menu-maker-dialogs";

export function IconPicker({
  currentToken,
  onClose,
  onPick,
  t,
}: {
  currentToken: string;
  onClose: () => void;
  onPick: (icon: MenuMakerSlot["icon"]) => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<IconSearchResult[]>(() => searchLucideIcons(""));
  const [online, setOnline] = useState(false);
  const [collectionPrefix, setCollectionPrefix] = useState("");
  const [cache, setCache] = useState<IconifyCacheStats>({ count: 0, bytes: 0, prefixes: [] });
  const [favorites, setFavorites] = useState(getFavoriteIconifyPrefixes);
  const token = useRef(currentToken);
  useEffect(() => {
    token.current = currentToken;
  }, [currentToken]);
  useEffect(() => {
    void Promise.all([searchCachedIconifyIcons(""), getIconifyCacheStats()]).then(
      ([cached, stats]) => {
        setResults([...searchLucideIcons(query), ...cached]);
        setCache(stats);
      },
    );
  }, []);
  const searchOnline = async () => {
    const requestToken = token.current;
    setOnline(true);
    try {
      const iconify = await searchIconifyIcons(query);
      if (acceptsIconResult(token.current, requestToken)) {
        setResults([...searchLucideIcons(query), ...iconify]);
        setCache(await getIconifyCacheStats());
      }
    } catch (error) {
      Logger.capture("menu-maker:icon-search", error);
      setResults([...searchLucideIcons(query), ...(await searchCachedIconifyIcons(query))]);
      toast.error(t("page.tools.menu_maker.icon_offline"));
    } finally {
      setOnline(false);
    }
  };
  return (
    <Modal title={t("page.tools.menu_maker.icon_picker")} onClose={onClose}>
      <div className="flex gap-2">
        <Input
          autoFocus
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            void searchCachedIconifyIcons(event.target.value).then((cached) =>
              setResults([...searchLucideIcons(event.target.value), ...cached]),
            );
          }}
          placeholder={t("page.tools.menu_maker.icon_search")}
        />
        <Button variant="outline" disabled={!query || online} onClick={() => void searchOnline()}>
          {online ? <Loader2Icon className="animate-spin" /> : <SearchIcon />}
          {t("page.tools.menu_maker.online_search")}
        </Button>
      </div>
      <p className="mt-2 text-xs text-muted-foreground">
        {t("page.tools.menu_maker.online_policy")}
      </p>
      <div className="mt-2 flex gap-2">
        <Input
          className="h-8"
          value={collectionPrefix}
          onChange={(event) => setCollectionPrefix(event.target.value)}
          placeholder={t("page.tools.menu_maker.collection_prefix")}
        />
        <Button
          size="sm"
          variant="outline"
          disabled={!collectionPrefix || online}
          onClick={() => {
            setOnline(true);
            void downloadIconifyCollection(collectionPrefix)
              .then(async (count) => {
                setCache(await getIconifyCacheStats());
                setResults([
                  ...searchLucideIcons(query),
                  ...(await searchCachedIconifyIcons(query)),
                ]);
                toast.success(t("page.tools.menu_maker.collection_cached", { count }));
              })
              .catch(() => toast.error(t("page.tools.menu_maker.icon_offline")))
              .finally(() => setOnline(false));
          }}
        >
          {t("page.tools.menu_maker.download_collection")}
        </Button>
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-1 text-[11px] text-muted-foreground">
        <span>
          {t("page.tools.menu_maker.icon_cache", {
            count: cache.count,
            size: formatBytes(cache.bytes),
          })}
        </span>
        {cache.prefixes.map((prefix) => (
          <ButtonGroup key={prefix}>
            <Button
              type="button"
              size="xs"
              variant="outline"
              onClick={() => setFavorites(toggleFavoriteIconifyPrefix(prefix))}
            >
              <StarIcon className={favorites.includes(prefix) ? "fill-current" : undefined} />
              {prefix}
            </Button>
            <Button
              type="button"
              size="icon-xs"
              variant="outline"
              onClick={() =>
                void deleteIconifyPrefix(prefix).then(async () => {
                  setCache(await getIconifyCacheStats());
                  setResults([
                    ...searchLucideIcons(query),
                    ...(await searchCachedIconifyIcons(query)),
                  ]);
                })
              }
            >
              <XIcon />
            </Button>
          </ButtonGroup>
        ))}
        {cache.count > 0 && (
          <Button
            type="button"
            variant="link"
            size="xs"
            className="ml-auto h-auto px-0"
            onClick={() =>
              void clearIconifyCache().then(() => {
                setCache({ count: 0, bytes: 0, prefixes: [] });
                setResults(searchLucideIcons(query));
              })
            }
          >
            {t("page.tools.menu_maker.clear_cache")}
          </Button>
        )}
      </div>
      <div className="mt-3 grid max-h-[55vh] grid-cols-6 gap-2 overflow-auto">
        {results.map((result) => (
          <Button
            key={`${result.source}:${result.name}`}
            type="button"
            variant="outline"
            title={result.name}
            className="aspect-square size-auto"
            onClick={() =>
              result.source === "lucide"
                ? onPick({ kind: "lucide", name: result.name, color: "#ff4fb3" })
                : result.svg &&
                  sanitizeIconifySVG(result.svg) &&
                  onPick({
                    kind: "iconify",
                    name: result.name,
                    color: "#ff4fb3",
                    svg: sanitizeIconifySVG(result.svg)!,
                  })
            }
          >
            {result.source === "lucide" &&
            iconNames.includes(result.name as (typeof iconNames)[number]) ? (
              <DynamicIcon name={result.name as (typeof iconNames)[number]} />
            ) : result.svg ? (
              <span className="size-6" dangerouslySetInnerHTML={{ __html: result.svg }} />
            ) : null}
          </Button>
        ))}
      </div>
    </Modal>
  );
}

export function CropDialog({
  source,
  size,
  onClose,
  onConfirm,
  t,
}: {
  source: string;
  size: number;
  onClose: () => void;
  onConfirm: (dataUrl: string) => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const [zoom, setZoom] = useState(1);
  const [image, setImage] = useState<HTMLImageElement>();
  const drag = useRef<{ x: number; y: number; ox: number; oy: number } | undefined>(undefined);
  const previewRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const next = new Image();
    next.src = source;
    let cancelled = false;
    void next.decode().then(
      () => {
        if (!cancelled) setImage(next);
      },
      (error: unknown) => {
        if (!cancelled) Logger.error({ error }, "MenuMakerPage:cropDecode");
      },
    );
    return () => {
      cancelled = true;
    };
  }, [source]);

  useEffect(() => {
    const canvas = previewRef.current;
    const context = canvas?.getContext("2d");
    if (!canvas || !context || !image?.naturalWidth) return;
    drawCoveredImage(
      context,
      image,
      image.naturalWidth,
      image.naturalHeight,
      MENU_MAKER_CROP_PREVIEW_SIZE,
      zoom,
      offset,
    );
  }, [image, offset, zoom]);

  const confirm = () => {
    if (!image?.naturalWidth) return;
    const canvas = document.createElement("canvas");
    canvas.width = size;
    canvas.height = size;
    const context = canvas.getContext("2d");
    if (!context) return;
    drawCoveredImage(context, image, image.naturalWidth, image.naturalHeight, size, zoom, offset);
    onConfirm(canvas.toDataURL("image/png"));
  };

  return (
    <Modal title={t("page.tools.menu_maker.crop")} onClose={onClose}>
      <canvas
        ref={previewRef}
        width={MENU_MAKER_CROP_PREVIEW_SIZE}
        height={MENU_MAKER_CROP_PREVIEW_SIZE}
        className="mx-auto size-64 touch-none rounded border border-border bg-black"
        onWheel={(event) => {
          event.preventDefault();
          setZoom((value) => Math.max(1, Math.min(4, value - event.deltaY * 0.001)));
        }}
        onPointerDown={(event) => {
          drag.current = { x: event.clientX, y: event.clientY, ox: offset.x, oy: offset.y };
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onPointerMove={(event) => {
          if (!drag.current) return;
          const bounds = event.currentTarget.getBoundingClientRect();
          setOffset({
            x:
              drag.current.ox +
              ((event.clientX - drag.current.x) * event.currentTarget.width) / bounds.width,
            y:
              drag.current.oy +
              ((event.clientY - drag.current.y) * event.currentTarget.height) / bounds.height,
          });
        }}
        onPointerUp={() => {
          drag.current = undefined;
        }}
      />
      <p className="mt-2 text-center text-xs text-muted-foreground">
        {t("page.tools.menu_maker.crop_hint")}
      </p>
      <DialogFooter className="mt-4">
        <Button variant="outline" onClick={onClose}>
          {t("g.cancel")}
        </Button>
        <Button disabled={!image?.naturalWidth} onClick={confirm}>
          {t("g.confirm")}
        </Button>
      </DialogFooter>
    </Modal>
  );
}

function formatBytes(bytes: number): string {
  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KiB`;
}
