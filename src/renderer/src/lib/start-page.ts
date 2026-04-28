import { supportsWindowsDesktopFeatures } from "@shared/platform";

export const DRIVE_START_PAGES = ["/drive/drive/root", "/drive/share/root"] as const;

export function requiresAuthForStartPage(page: string | null | undefined) {
  return DRIVE_START_PAGES.includes(page as (typeof DRIVE_START_PAGES)[number]);
}

export function getFallbackStartPage(platform?: string | null) {
  return supportsWindowsDesktopFeatures(platform) ? "/mod" : "/transfer";
}

export function materializeStartPage(
  page: string | null | undefined,
  options: {
    isLoggedIn: boolean;
    sessionRootId?: string | null;
  },
) {
  if (!page) {
    return page;
  }

  if (page === "/drive/drive/root") {
    return options.isLoggedIn && options.sessionRootId
      ? `/drive/drive/${options.sessionRootId}`
      : page;
  }

  if (page === "/drive/share/root") {
    return options.isLoggedIn ? "/drive/share/share" : page;
  }

  return page;
}

export function resolveStartPage(
  page: string | null | undefined,
  options: {
    isLoggedIn: boolean;
    platform?: string | null;
    sessionRootId?: string | null;
  },
) {
  const fallback = getFallbackStartPage(options.platform);

  if (!page) {
    return fallback;
  }

  if (!options.isLoggedIn && requiresAuthForStartPage(page)) {
    return fallback;
  }

  if (!supportsWindowsDesktopFeatures(options.platform) && page === "/mod") {
    return fallback;
  }

  return materializeStartPage(page, options);
}
