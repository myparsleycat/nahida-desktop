import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';

import {
  createNSISVersionInclude,
  createReleaseResourceConfig,
  parseReleaseVersion,
} from './release-version.mjs';

// go-winres configs of the binaries that carry the application version.
const RELEASE_RESOURCE_CONFIGS = ['nahida-desktop.winres.json', 'elevated-helper.winres.json'];

const [version, outputDirectory = 'bin'] = process.argv.slice(2);
if (!version) {
  throw new Error('APP_VERSION is required');
}

const release = parseReleaseVersion(version);
const root = process.cwd();
const source = path.join(root, 'build/windows');
const output = path.resolve(root, outputDirectory);
const configs = await Promise.all(
  RELEASE_RESOURCE_CONFIGS.map(async (file) => {
    const base = JSON.parse(await readFile(path.join(source, file), 'utf8'));
    return [file, createReleaseResourceConfig(base, release.version, { from: source, to: output })];
  }),
);

await mkdir(output, { recursive: true });
await Promise.all([
  ...configs.map(([file, config]) =>
    writeFile(path.join(output, file), `${JSON.stringify(config, null, 2)}\n`),
  ),
  writeFile(path.join(output, 'release-version.nsh'), createNSISVersionInclude(release.version)),
]);

process.stdout.write(
  `Prepared Windows metadata for ${release.version} (${release.numericVersion})\n`,
);
