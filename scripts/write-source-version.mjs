import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

import { parseReleaseVersion } from './release-version.mjs';

const RESOURCE_FILES = {
  appResources: 'build/windows/nahida-desktop.winres.json',
  helperResources: 'build/windows/elevated-helper.winres.json',
};

const SOURCE_FILES = {
  ...RESOURCE_FILES,
  manifest: 'build/windows/nahida-desktop.exe.manifest',
  platform: 'internal/platform/shell.go',
};

export async function writeSourceVersion(root, input) {
  const release = parseReleaseVersion(input);
  const entries = Object.fromEntries(
    Object.entries(SOURCE_FILES).map(([name, file]) => [name, path.join(root, file)]),
  );
  const sources = Object.fromEntries(
    await Promise.all(
      Object.entries(entries).map(async ([name, file]) => [name, await readFile(file, 'utf8')]),
    ),
  );

  const updated = {
    ...Object.fromEntries(
      Object.entries(RESOURCE_FILES).map(([name, file]) => [
        name,
        updateResourceVersion(sources[name], release, file),
      ]),
    ),
    manifest: replaceExactlyOnce(
      sources.manifest,
      /(<assemblyIdentity\s+type="win32"\s+name="com\.nahida\.desktop"\s+version=")([^"]+)(")/,
      `$1${release.numericVersion}$3`,
      SOURCE_FILES.manifest,
    ),
    platform: replaceExactlyOnce(
      sources.platform,
      /(var AppVersion = ")[^"]+("\r?\n)/,
      `$1${release.version}$2`,
      SOURCE_FILES.platform,
    ),
  };

  await Promise.all(
    Object.entries(updated).map(([name, content]) => writeFile(entries[name], content)),
  );

  process.stdout.write(`Updated source version to ${release.version}\n`);
  return release;
}

function updateResourceVersion(source, release, file) {
  const config = JSON.parse(source);
  const infos = Object.values(config.RT_VERSION ?? {}).flatMap((languages) =>
    Object.values(languages),
  );
  if (infos.length !== 1) {
    throw new Error(`Expected exactly one version field in ${file}; found ${infos.length}`);
  }

  const [info] = infos;
  info.fixed.file_version = release.numericVersion;
  if (Object.hasOwn(info.fixed, 'product_version')) {
    info.fixed.product_version = release.numericVersion;
  }
  info.info['0000'].ProductVersion = release.version;
  if (Object.hasOwn(info.info['0000'], 'FileVersion')) {
    info.info['0000'].FileVersion = release.version;
  }
  return `${JSON.stringify(config, null, 2)}\n`;
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
