import { Shell } from "@bindings/platform";
import type { BufferInfo, Capture, Layer } from "@bindings/tools/hash_inspector";
import { InspectorSection } from "@renderer/components/tools/inspector-section";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import {
  Section,
  SectionContent,
  SectionHeader,
  SectionTitle,
} from "@renderer/components/ui/section";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@renderer/components/ui/table";
import { Logger } from "@renderer/lib/logger";
import type { TFunction } from "i18next";
import { CopyIcon, ExternalLinkIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const K = "page.tools.hash_inspector";

export function CaptureDetail({ capture }: { capture: Capture }) {
  const { t } = useTranslation();
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const layers = capture.layers ?? [];

  if (capture.failureCode) {
    return (
      <Section>
        <SectionHeader>
          <SectionTitle>{t(`${K}.failure.title`)}</SectionTitle>
        </SectionHeader>
        <SectionContent layout="flow">
          <p className="text-xs text-destructive">
            {t(`${K}.failure.${capture.failureCode}`, { defaultValue: capture.failureCode })}
          </p>
          {capture.failureDetail ? (
            <p className="font-mono text-xs text-muted-foreground">{capture.failureDetail}</p>
          ) : null}
        </SectionContent>
      </Section>
    );
  }

  const visible = layers.find((layer) => layer.visible);
  const selected = layers.find((layer) => layer.recordId === selectedId) ?? visible ?? layers[0];

  return (
    <>
      <Section>
        <SectionHeader>
          <SectionTitle>
            {t(`${K}.visible_draw`)}
            <span className="ml-2 font-mono text-xs font-normal text-muted-foreground">
              {capture.cursorX}, {capture.cursorY} / {capture.windowWidth}×{capture.windowHeight}
            </span>
          </SectionTitle>
        </SectionHeader>
        <SectionContent layout="flow">
          {capture.hitsOverflow ? (
            <p className="text-xs text-amber-500">{t(`${K}.hits_overflow`)}</p>
          ) : null}
          {capture.deferredSkipped ? (
            <p className="text-xs text-muted-foreground">{t(`${K}.deferred_skipped`)}</p>
          ) : null}
          {visible ? (
            <VisibleDrawCard layer={visible} />
          ) : (
            <p className="text-xs text-muted-foreground">{t(`${K}.no_visible_draw`)}</p>
          )}
        </SectionContent>
      </Section>

      {layers.length ? (
        <Section>
          <SectionHeader>
            <SectionTitle>{t(`${K}.layers_count`, { count: layers.length })}</SectionTitle>
          </SectionHeader>
          <SectionContent layout="flow">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead />
                  <TableHead>{t(`${K}.mod`)}</TableHead>
                  <TableHead>{t(`${K}.match_hash`)}</TableHead>
                  <TableHead>{t(`${K}.draw`)}</TableHead>
                  <TableHead className="text-right">{t(`${K}.depth`)}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {layers.map((layer) => (
                  <TableRow
                    key={layer.recordId}
                    data-state={layer === selected ? "selected" : undefined}
                    className="cursor-pointer"
                    onClick={() => setSelectedId(layer.recordId)}
                  >
                    <TableCell>
                      <Badge variant={layer.visible ? "default" : "outline"}>
                        {layer.visible ? t(`${K}.visible`) : t(`${K}.hidden`)}
                      </Badge>
                    </TableCell>
                    <TableCell className="max-w-56 truncate text-xs">
                      {layerOwner(t, layer)}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {layer.ib?.matchHash || "-"}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {t(`${K}.origin.${layer.origin}`)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {layer.depth.toFixed(6)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {selected ? <LayerDetail captureId={capture.id} layer={selected} /> : null}
          </SectionContent>
        </Section>
      ) : null}
    </>
  );
}

function VisibleDrawCard({ layer }: { layer: Layer }) {
  const { t } = useTranslation();
  const shader = (stage: string) => layer.shaders?.find((item) => item.stage === stage)?.hash ?? "";
  const snippet = iniSnippet(layer);

  return (
    <div className="flex flex-col gap-3 rounded-md border border-border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium">{layerOwner(t, layer)}</span>
        <span className="text-xs text-muted-foreground">{t(`${K}.origin.${layer.origin}`)}</span>
        {snippet ? (
          <Button
            variant="outline"
            size="sm"
            className="ml-auto"
            onClick={() => copyText(snippet, t(`${K}.snippet_copied`), t)}
          >
            <CopyIcon />
            {t(`${K}.copy_snippet`)}
          </Button>
        ) : null}
      </div>

      {layer.commandList ? (
        <Group title={t(`${K}.command_list`)}>
          <IniRow
            section={layer.commandList.section}
            iniPath={layer.commandList.iniPath}
            mod={layer.commandList.mod}
          />
          <HashText value={layer.commandList.line} />
        </Group>
      ) : null}
      {layer.overrides?.length ? (
        <Group title={t(`${K}.matched_sections`)}>
          {layer.overrides.map((item) => (
            <IniRow
              key={`${item.iniPath}:${item.section}`}
              section={item.section}
              iniPath={item.iniPath}
              mod={item.mod}
            />
          ))}
        </Group>
      ) : null}

      <Facts
        items={[
          [
            `${t(`${K}.index_buffer`)} (${t(`${K}.match_hash`)})`,
            <HashText key="ib" value={layer.ib?.matchHash ?? ""} />,
          ],
          ["VB0", <HashText key="vb0" value={layer.vbs?.[0]?.matchHash ?? ""} />],
          ["VS", <HashText key="vs" value={shader("vs")} />],
          ["PS", <HashText key="ps" value={shader("ps")} />],
          [t(`${K}.draw`), `${layer.call} ${layer.count} @ ${layer.start}`],
        ]}
      />
    </div>
  );
}

function LayerDetail({ captureId, layer }: { captureId: string; layer: Layer }) {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-3 rounded-md border border-border p-3">
      <Facts
        items={[
          [t(`${K}.draw`), `${layer.call} ${layer.count} @ ${layer.start} (#${layer.drawNumber})`],
          [t(`${K}.depth`), layer.depth.toFixed(6)],
          [t(`${K}.primitives`), (layer.primitives ?? []).join(", ") || "-"],
          [t(`${K}.index_offset`), layer.indexOffset ?? "-"],
        ]}
      />

      {layer.commandList ? (
        <Group title={t(`${K}.command_list`)}>
          <IniRow
            section={layer.commandList.section}
            iniPath={layer.commandList.iniPath}
            mod={layer.commandList.mod}
          />
          <HashText value={layer.commandList.line} />
        </Group>
      ) : null}

      {layer.overrides?.length ? (
        <Group title={t(`${K}.matched_sections`)}>
          {layer.overrides.map((item) => (
            <IniRow
              key={`${item.iniPath}:${item.section}`}
              section={item.section}
              iniPath={item.iniPath}
              mod={item.mod}
            />
          ))}
        </Group>
      ) : null}

      <BufferTable
        title={t(`${K}.index_buffer`)}
        rows={layer.ib ? [layer.ib] : []}
        showSlot={false}
      />
      <BufferTable title={t(`${K}.vertex_buffers`)} rows={layer.vbs ?? []} showSlot />

      {layer.shaders?.length ? (
        <Group title={t(`${K}.shaders`)}>
          <Table>
            <TableBody>
              {layer.shaders.map((shader) => (
                <TableRow key={shader.stage}>
                  <TableCell className="w-12 uppercase">{shader.stage}</TableCell>
                  <TableCell>
                    <HashText value={shader.hash} />
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {shader.replaced ? t(`${K}.replaced`) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Group>
      ) : null}

      {layer.textures?.length ? (
        <Group title={t(`${K}.textures`)}>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t(`${K}.slot`)}</TableHead>
                <TableHead>{t(`${K}.hash`)}</TableHead>
                <TableHead>{t(`${K}.orig_hash`)}</TableHead>
                <TableHead>{t(`${K}.size`)}</TableHead>
                <TableHead>{t(`${K}.format`)}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {layer.textures.map((texture) => (
                <TableRow key={`${texture.stage}:${texture.slot}`}>
                  <TableCell className="font-mono">
                    {texture.stage}-t{texture.slot}
                  </TableCell>
                  <TableCell>
                    <HashText value={texture.hash} />
                  </TableCell>
                  <TableCell>
                    <HashText value={texture.origHash} />
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {texture.width}×{texture.height}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{texture.format}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Group>
      ) : null}

      {layer.renderTargets?.length || layer.depthTarget ? (
        <InspectorSection
          id={`hash-inspector:${captureId}:targets`}
          title={`${t(`${K}.render_targets`)} / ${t(`${K}.depth_target`)}`}
          panelClassName="pt-1"
        >
          <Table>
            <TableBody>
              {[
                ...(layer.renderTargets ?? []),
                ...(layer.depthTarget ? [layer.depthTarget] : []),
              ].map((surface, index) => (
                <TableRow key={`${surface.slot}:${index}`}>
                  <TableCell className="font-mono">
                    {surface === layer.depthTarget ? "ds" : `o${surface.slot}`}
                  </TableCell>
                  <TableCell>
                    <HashText value={surface.hash} />
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {surface.width}×{surface.height}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{surface.format}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </InspectorSection>
      ) : null}
    </div>
  );
}

function BufferTable({
  title,
  rows,
  showSlot,
}: {
  title: string;
  rows: BufferInfo[];
  showSlot: boolean;
}) {
  const { t } = useTranslation();
  if (!rows.length) return null;

  return (
    <Group title={title}>
      <Table>
        <TableHeader>
          <TableRow>
            {showSlot ? <TableHead>{t(`${K}.slot`)}</TableHead> : null}
            <TableHead>{t(`${K}.match_hash`)}</TableHead>
            <TableHead>{t(`${K}.hash`)}</TableHead>
            <TableHead>{t(`${K}.orig_hash`)}</TableHead>
            <TableHead>{t(`${K}.format`)}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={row.slot}>
              {showSlot ? <TableCell className="font-mono">vb{row.slot}</TableCell> : null}
              <TableCell>
                <HashText value={row.matchHash} />
              </TableCell>
              <TableCell>
                <HashText value={row.hash} />
              </TableCell>
              <TableCell>
                <HashText value={row.origHash} />
              </TableCell>
              <TableCell className="font-mono text-xs">{row.formatOrStride}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Group>
  );
}

function IniRow({ section, iniPath, mod }: { section: string; iniPath: string; mod: string }) {
  const { t } = useTranslation();

  return (
    <div className="flex min-w-0 items-center gap-2 text-xs">
      <span className="font-mono">[{section}]</span>
      <span className="min-w-0 truncate text-muted-foreground" title={iniPath}>
        {mod || iniPath}
      </span>
      {iniPath ? (
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={t(`${K}.open_ini`)}
          onClick={() => void Shell.OpenPath(iniPath)}
        >
          <ExternalLinkIcon />
        </Button>
      ) : null}
    </div>
  );
}

function HashText({ value }: { value: string }) {
  const { t } = useTranslation();
  if (!value) return <span className="text-xs text-muted-foreground">-</span>;

  return (
    <button
      type="button"
      className="rounded-sm font-mono text-xs hover:text-accent"
      title={t(`${K}.copy`)}
      onClick={() => copyText(value, t(`${K}.copied`), t)}
    >
      {value}
    </button>
  );
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted-foreground">{title}</span>
      {children}
    </div>
  );
}

function Facts({ items }: { items: [string, ReactNode][] }) {
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
      {items.map(([label, value]) => (
        <div key={label} className="contents">
          <dt className="text-muted-foreground">{label}</dt>
          <dd className="font-mono">{value}</dd>
        </div>
      ))}
    </dl>
  );
}

function copyText(value: string, done: string, t: TFunction) {
  navigator.clipboard.writeText(value).then(
    () => toast.success(done),
    (error: unknown) => {
      Logger.error(error, "HashInspector:copy");
      toast.error(t(`${K}.copy_failed`));
    },
  );
}

function layerOwner(t: TFunction, layer: Layer) {
  return layer.overrides?.[0]?.mod || layer.commandList?.mod || t(`${K}.not_a_mod`);
}

// iniSnippet starts a [TextureOverride] section that matches the draw by its index buffer. Only a game
// draw has a StartIndexLocation the mod can match; a command list draw is already the mod's own.
function iniSnippet(layer: Layer) {
  const hash = layer.ib?.matchHash;
  if (!hash) return "";
  const lines = [`[TextureOverride${hash}]`, `hash = ${hash}`];
  if (layer.origin === "game" && layer.call.startsWith("DrawIndexed")) {
    lines.push(`match_first_index = ${layer.start}`);
  }
  return lines.join("\n") + "\n";
}
