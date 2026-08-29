import { getFixToolPresets, getFixToolScripts } from "@renderer/wails/fix-tools";
import { useEffect, useState } from "react";

type FixTool = {
    id: string;
    name: string;
    type: string;
    size: number;
};

type Preset = {
    id: string;
    name: string;
};

export function useModContextMenuData() {
    const [fixTools, setFixTools] = useState<FixTool[]>([]);
    const [presets, setPresets] = useState<Preset[]>([]);

    useEffect(() => {
        void getFixToolScripts().then((res) => setFixTools(res || []));
        void getFixToolPresets().then((res) => setPresets(res || []));
    }, []);

    return { fixTools, presets };
}
