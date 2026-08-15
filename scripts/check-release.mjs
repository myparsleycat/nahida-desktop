import fs from "node:fs";

import semanticRelease from "semantic-release";

async function checkRelease() {
    const result = await semanticRelease({
        dryRun: true,
        plugins: ["@semantic-release/commit-analyzer"],
        branches: ["main"],
    });

    const shouldRelease = Boolean(result && result.nextRelease);
    const nextVersion = shouldRelease ? result.nextRelease.version : "";
    const releaseType = shouldRelease ? result.nextRelease.type : "";

    console.log(`[check-release] Should release: ${shouldRelease}`);
    if (shouldRelease) {
        console.log(`[check-release] Next version: ${nextVersion} (${releaseType})`);
    } else {
        console.log("[check-release] No release required for recent commits.");
    }

    const githubOutput = process.env.GITHUB_OUTPUT;
    if (githubOutput) {
        fs.appendFileSync(githubOutput, `should_release=${shouldRelease}\n`);
        fs.appendFileSync(githubOutput, `next_version=${nextVersion}\n`);
        fs.appendFileSync(githubOutput, `release_type=${releaseType}\n`);
    }
}

checkRelease().catch((err) => {
    console.error("[check-release] Failed to determine release status:", err);
    process.exit(1);
});
