import type { Logger } from "@main/internal/logger";
import ky from "ky";
import ms from "ms";

type GitHubRelease = {
    tag_name?: unknown;
};

const NON_RELEASE_VERSION_NAMES = new Set(["main", "master"]);
const RELEASES_FETCH_COOLDOWN_MS = ms("1m");

const releasesCache: Partial<Record<string, string[]>> = {};
const releasesFetchedAt: Partial<Record<string, number>> = {};
const releasesFetchInFlight: Partial<Record<string, Promise<boolean>>> = {};

export async function updateXxmiLibsReleases(logger: Logger, provider = "SpectrumQT") {
    await fetchProviderReleases(logger, provider);
}

export async function getXxmiLibsReleases(logger: Logger, provider = "SpectrumQT") {
    if (!releasesCache[provider]) {
        await fetchProviderReleases(logger, provider);
    }

    return releasesCache[provider] ?? [];
}

async function fetchProviderReleases(logger: Logger, provider: string) {
    const inFlight = releasesFetchInFlight[provider];
    if (inFlight) {
        return inFlight;
    }

    const now = Date.now();
    const lastFetchedAt = releasesFetchedAt[provider] ?? 0;
    if (now - lastFetchedAt < RELEASES_FETCH_COOLDOWN_MS) {
        return true;
    }

    const fetchPromise = fetchProviderReleasesInternal(logger, provider);
    releasesFetchInFlight[provider] = fetchPromise;

    try {
        const success = await fetchPromise;
        if (success) {
            releasesFetchedAt[provider] = Date.now();
        }
        return success;
    } finally {
        delete releasesFetchInFlight[provider];
    }
}

async function fetchProviderReleasesInternal(logger: Logger, provider: string) {
    try {
        const url = `https://api.github.com/repos/${provider}/XXMI-Libs-Package/releases`;
        const resp = await ky.get(url, {
            headers: {
                "User-Agent":
                    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
            },
            throwHttpErrors: false,
        });

        if (!resp.ok) {
            logger.warn(
                `Failed to fetch releases for ${provider}: ${resp.status} ${resp.statusText}`,
                "XxmiLibsReleases:fetchProviderReleases",
            );
            return false;
        }

        const releases = (await resp.json()) as GitHubRelease[];
        releasesCache[provider] = releases
            .map((release) => release.tag_name)
            .filter(
                (tagName): tagName is string =>
                    typeof tagName === "string" &&
                    !NON_RELEASE_VERSION_NAMES.has(tagName.toLowerCase()),
            );
        return true;
    } catch (error) {
        logger.error(error, "XxmiLibsReleases:fetchProviderReleases");
        return false;
    }
}
