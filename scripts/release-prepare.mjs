import { spawnSync } from 'node:child_process';
import process from 'node:process';

import { parseReleaseVersion } from './release-version.mjs';
import { writeSourceVersion } from './write-source-version.mjs';

const version = process.argv.slice(2).find((argument) => argument !== '--');
const release = parseReleaseVersion(version);
await writeSourceVersion(process.cwd(), release.version);
const result = spawnSync(
  'task',
  ['package', `APP_VERSION=${release.version}`, 'ARCH=amd64'],
  { stdio: 'inherit', shell: process.platform === 'win32' },
);

if (result.error) {
  throw result.error;
}
if (result.status !== 0) {
  process.exit(result.status ?? 1);
}
