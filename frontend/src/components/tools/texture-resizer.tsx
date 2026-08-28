import { TextureResizerWorkspace } from "@renderer/components/tools/texture-resizer-workspace";
import { ScrollArea } from "@renderer/components/ui/scroll-area";

export default function TextureResizer() {
  return (
    <ScrollArea className="h-full">
      <div className="flex h-full min-h-0 flex-col p-4">
        <TextureResizerWorkspace mode="folder" />
      </div>
    </ScrollArea>
  );
}
