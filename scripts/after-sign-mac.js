const { execSync } = require("child_process");
const path = require("path");

/**
 * electron-builder afterSign hook
 *
 * Fixes "different Team IDs" crash on macOS when building without an Apple Developer certificate.
 * The Electron Framework ships pre-signed with Electron's Team ID, but the app itself
 * gets ad-hoc signed (no Team ID). macOS 15+ enforces that all components in a bundle
 * share the same Team ID, so we re-sign everything with ad-hoc identity ("-") to unify them.
 */
exports.default = async function afterSign(context) {
    const { electronPlatformName, appOutDir, packager } = context;

    if (electronPlatformName !== "darwin") {
        return;
    }

    const appName = packager.appInfo.productFilename;
    const appPath = path.join(appOutDir, `${appName}.app`);

    // If a real signing identity is present (i.e. paid Apple Developer account),
    // electron-builder already handles signing correctly — skip ad-hoc re-signing.
    const identity = packager.platformSpecificBuildOptions.identity;
    if (identity && identity !== "-" && identity !== null) {
        console.log(
            `[after-sign-mac] Real identity detected ("${identity}"), skipping ad-hoc re-sign.`,
        );
        return;
    }

    console.log(`[after-sign-mac] Ad-hoc re-signing app to fix Team ID mismatch: ${appPath}`);

    try {
        // --deep  : recursively signs all nested code (frameworks, helpers, dylibs)
        // --force : overrides any existing signature (including Electron's official one)
        // --sign - : ad-hoc identity — no certificate required
        execSync(`codesign --deep --force --sign - "${appPath}"`, { stdio: "inherit" });
        console.log("[after-sign-mac] Ad-hoc re-signing completed successfully.");
    } catch (err) {
        console.error("[after-sign-mac] Re-signing failed:", err.message);
        throw err;
    }
};
