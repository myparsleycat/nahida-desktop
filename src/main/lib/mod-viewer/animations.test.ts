import assert from "node:assert/strict";

import { describe, it } from "vitest";

import { animationFrameValues, extractPresentAnimations } from "./animations";
import { parseIniText } from "./ini";

describe("extractPresentAnimations range limits", () => {
    it("keeps ordinary time clips and their frame domains bounded", () => {
        const clips = extractPresentAnimations(
            parseIniText(
                `[Constants]
global $fps = 30
global $frame = 0
[Present]
$frame = time * $fps
[TextureOverrideBody]
if $frame == 0
    vb0 = ResourcePos
elif $frame == 1
    vb0 = ResourcePos1
endif
`,
                "mod.ini",
            ),
            { fps: "30", frame: "0" },
            [],
        );
        assert.equal(clips.length, 1);
        assert.deepEqual(animationFrameValues(clips[0]), ["0", "1"]);
    });

    it("rejects clips whose frame span exceeds the value limit", () => {
        const clips = extractPresentAnimations(
            parseIniText(
                `[Constants]
global $fps = 30
global $frame = 0
[Present]
$frame = time * $fps
[TextureOverrideBody]
if $frame == 0
    vb0 = ResourcePos
elif $frame == 100000
    vb0 = ResourcePos1
endif
`,
                "mod.ini",
            ),
            { fps: "30", frame: "0" },
            [],
        );
        assert.deepEqual(clips, []);
    });
});
