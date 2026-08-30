import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

import { parseReleaseVersion } from './release-version.mjs';

const SOURCE_FILES = {
  config: 'build/config.yml',
  info: 'build/windows/info.json',
  manifest: 'build/windows/wails.exe.manifest',
  nsisTools: 'build/windows/nsis/wails_tools.nsh',
  platform: 'internal/platform/shell.go',
};

export async function writeSourceVersion(root, input) {
  const release = parseReleaseVersion(input);
  const entries = Object.fromEntries(
    Object.entries(SOURCE_FILES).map(([name, file]) => [name, path.join(root, file)]),
  );
  const [config, infoSource, manifest, nsisTools, platform] = await Promise.all([
    readFile(entries.config, 'utf8'),
    readFile(entries.info, 'utf8'),
    readFile(entries.manifest, 'utf8'),
    readFile(entries.nsisTools, 'utf8'),
    readFile(entries.platform, 'utf8'),
  ]);
  const info = JSON.parse(infoSource);
  info.fixed.file_version = release.numericVersion;
  if (Object.hasOwn(info.fixed, 'product_version')) {
    info.fixed.product_version = release.numericVersion;
  }
  info.info['0000'].ProductVersion = release.version;
  if (Object.hasOwn(info.info['0000'], 'FileVersion')) {
    info.info['0000'].FileVersion = release.version;
  }

  const updated = {
    config: replaceExactlyOnce(
      config,
      /^(\s*version:\s*)"[^"]+"(\s*# The application version\s*)$/m,
      `$1"${release.version}"$2`,
      SOURCE_FILES.config,
    ),
    info: `${JSON.stringify(info, null, 2)}\n`,
    manifest: replaceExactlyOnce(
      manifest,
      /(<assemblyIdentity\s+type="win32"\s+name="com\.nahida\.desktop"\s+version=")([^"]+)(")/,
      `$1${release.numericVersion}$3`,
      SOURCE_FILES.manifest,
    ),
    nsisTools: replaceExactlyOnce(
      nsisTools,
      /(!define INFO_PRODUCTVERSION ")[^"]+("\r?\n)/,
      `$1${release.version}$2`,
      SOURCE_FILES.nsisTools,
    ),
    platform: replaceExactlyOnce(
      platform,
      /(var AppVersion = ")[^"]+("\r?\n)/,
      `$1${release.version}$2`,
      SOURCE_FILES.platform,
    ),
  };

  await Promise.all([
    writeFile(entries.config, updated.config),
    writeFile(entries.info, updated.info),
    writeFile(entries.manifest, updated.manifest),
    writeFile(entries.nsisTools, updated.nsisTools),
    writeFile(entries.platform, updated.platform),
  ]);

  process.stdout.write(`Updated source version to ${release.version}\n`);
  return release;
}

function replaceExactlyOnce(source, pattern, replacement, file) {
  const matches = source.match(new RegExp(pattern.source, `${pattern.flags}g`));
  if (matches?.length !== 1) {
    throw new Error(`Expected exactly one version field in ${file}; found ${matches?.length ?? 0}`);
  }
  return source.replace(pattern, replacement);
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : '';
if (invokedPath === fileURLToPath(import.meta.url)) {
  const version = process.argv.slice(2).find((argument) => argument !== '--');
  await writeSourceVersion(process.cwd(), version);
}
