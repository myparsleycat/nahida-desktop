import { Button } from "@renderer/components/ui/button";
import { DialogClose, DialogFooter } from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { Kbd } from "@renderer/components/ui/kbd";
import { formatKeyLabel, getBaseKey, getUsedModifiers } from "@shared/key-formatter";
import { useEffect, useRef, useState } from "react";
import { mapKeyboardEventToInternal } from "./utils";

interface KeyRecorderProps {
  defaultValue: string;
  otherKeys: string[];
  onSave: (value: string) => void;
}

export function KeyRecorder({ defaultValue, otherKeys, onSave }: KeyRecorderProps) {
  const [value, setValue] = useState(defaultValue);
  const containerRef = useRef<HTMLDivElement>(null);
  const footerRef = useRef<HTMLDivElement>(null);
  const manualRef = useRef<HTMLDivElement>(null);
  const saveButtonRef = useRef<HTMLButtonElement>(null);
  const enterTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const displayKeys = value
    .split(" ")
    .map((k) => formatKeyLabel(k))
    .filter((k): k is string => k !== null);

  useEffect(() => {
    containerRef.current?.focus();
    return () => {
      if (enterTimerRef.current) clearTimeout(enterTimerRef.current);
    };
  }, []);

  const recordKey = (e: React.KeyboardEvent | KeyboardEvent) => {
    const basicMapped = mapKeyboardEventToInternal(e);
    if (!basicMapped) return;

    const baseKey = getBaseKey(basicMapped);
    if (!baseKey) {
      setValue(basicMapped);
      return;
    }

    const conflicts = getUsedModifiers(baseKey, otherKeys);
    const finalMapped = mapKeyboardEventToInternal(e, conflicts);

    if (finalMapped) {
      setValue(finalMapped);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    e.preventDefault();
    e.stopPropagation();

    if (e.key === "Enter") {
      if (!e.repeat) {
        enterTimerRef.current = setTimeout(() => {
          saveButtonRef.current?.click();
          enterTimerRef.current = null;
        }, 500);
      }
      return;
    }

    if (enterTimerRef.current) {
      clearTimeout(enterTimerRef.current);
      enterTimerRef.current = null;
    }

    recordKey(e);
  };

  const handleKeyUp = (e: React.KeyboardEvent) => {
    e.preventDefault();
    e.stopPropagation();

    if (e.key === "Enter") {
      if (enterTimerRef.current) {
        clearTimeout(enterTimerRef.current);
        enterTimerRef.current = null;
        recordKey(e);
      }
    }
  };

  const handleBlur = (e: React.FocusEvent) => {
    if (
      footerRef.current?.contains(e.relatedTarget as Node) ||
      containerRef.current?.contains(e.relatedTarget as Node) ||
      manualRef.current?.contains(e.relatedTarget as Node)
    ) {
      return;
    }

    containerRef.current?.focus();
  };

  return (
    <div className="flex flex-col gap-4">
      {/* oxlint-disable-next-line jsx_a11y/no-static-element-interactions */}
      <div
        ref={containerRef}
        className="flex items-center justify-center p-6 border border-dashed rounded-md bg-muted/30 focus:bg-foreground/10 focus:border-solid outline-none transition-all cursor-pointer min-h-[100px]"
        tabIndex={0}
        onKeyDown={handleKeyDown}
        onKeyUp={handleKeyUp}
        onBlur={handleBlur}
      >
        {displayKeys.length > 0 ? (
          <div className="flex flex-wrap items-center justify-center gap-2">
            {displayKeys.map((label, idx) => (
              <Kbd key={idx.toString()} className="text-sm h-8 px-2 min-w-8 bg-background/50">
                {label}
              </Kbd>
            ))}
          </div>
        ) : (
          <span className="text-muted-foreground text-sm">Press any key combination...</span>
        )}
      </div>
      <div className="space-y-1" ref={manualRef}>
        <p className="text-[10px] text-muted-foreground uppercase font-bold ml-1">Manual Edit</p>
        <Input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          className="font-mono text-sm"
          placeholder="e.g. ctrl alt no_shift vk_up"
        />
      </div>
      <div ref={footerRef}>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <DialogClose asChild>
            <Button ref={saveButtonRef} onClick={() => onSave(value)}>
              Save
            </Button>
          </DialogClose>
        </DialogFooter>
      </div>
    </div>
  );
}
