import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';

import { createNSISVersionInclude, createWindowsVersionInfo } from './release-version.mjs';

const [version, outputDirectory = 'bin'] = process.argv.slice(2);
if (!version) {
  throw new Error('APP_VERSION is required');
}

const root = process.cwd();
const output = path.resolve(root, outputDirectory);
const base = JSON.parse(await readFile(path.join(root, 'build/windows/info.json'), 'utf8'));
const { release, info } = createWindowsVersionInfo(base, version);

await mkdir(output, { recursive: true });
await Promise.all([
  writeFile(path.join(output, 'windows-info.json'), `${JSON.stringify(info, null, '\t')}\n`),
  writeFile(path.join(output, 'release-version.nsh'), createNSISVersionInclude(release.version)),
]);

process.stdout.write(
  `Prepared Windows metadata for ${release.version} (${release.numericVersion})\n`,
);
