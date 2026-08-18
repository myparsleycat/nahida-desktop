import type { Logger } from "@main/internal/logger";
import ky from "ky";
import ms from "ms";

type GitHubRelease = {
    tag_name?: unknown;
};

const NON_RELEASE_VERSION_NAMES = new Set(["main", "master"]);
const RELEASES_FETCH_COOLDOWN_MS = ms("1m");

const releasesCache = new Map<string, string[]>();
const releasesFetchedAt = new Map<string, number>();
const releasesFetchInFlight = new Map<string, Promise<boolean>>();

export async function updateXxmiLibsReleases(logger: Logger, provider = "SpectrumQT") {
    const success = await fetchProviderReleases(logger, provider);
    if (!success) {
        throw new Error(`Failed to fetch XXMI libs releases for ${provider}`);
    }
}

export async function getXxmiLibsReleases(logger: Logger, provider = "SpectrumQT") {
    if (!releasesCache.has(provider)) {
        const success = await fetchProviderReleases(logger, provider);
        if (!success) {
            throw new Error(`Failed to fetch XXMI libs releases for ${provider}`);
        }
    }

    return releasesCache.get(provider) ?? [];
}

async function fetchProviderReleases(logger: Logger, provider: string) {
    const inFlight = releasesFetchInFlight.get(provider);
    if (inFlight) {
        return inFlight;
    }

    const now = Date.now();
    const lastFetchedAt = releasesFetchedAt.get(provider) ?? 0;
    if (now - lastFetchedAt < RELEASES_FETCH_COOLDOWN_MS) {
        return true;
    }

    const fetchPromise = fetchProviderReleasesInternal(logger, provider);
    releasesFetchInFlight.set(provider, fetchPromise);

    try {
        const success = await fetchPromise;
        if (success) {
            releasesFetchedAt.set(provider, Date.now());
        }
        return success;
    } finally {
        releasesFetchInFlight.delete(provider);
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
        releasesCache.set(
            provider,
            releases
                .map((release) => release.tag_name)
                .filter(
                    (tagName): tagName is string =>
                        typeof tagName === "string" &&
                        !NON_RELEASE_VERSION_NAMES.has(tagName.toLowerCase()),
                ),
        );
        return true;
    } catch (error) {
        logger.error(error, "XxmiLibsReleases:fetchProviderReleases");
        return false;
    }
}
