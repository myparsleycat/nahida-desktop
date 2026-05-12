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
        window.api.invoke("ftm:getScripts").then((res) => setFixTools(res || []));
        window.api.invoke("ftm:getPresets").then((res) => setPresets(res || []));
    }, []);

    return { fixTools, presets };
}
