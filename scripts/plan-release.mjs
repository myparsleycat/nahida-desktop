import { appendFile } from 'node:fs/promises';
import process from 'node:process';
import semanticRelease from 'semantic-release';

import { parseReleaseVersion } from './release-version.mjs';

const result = await semanticRelease({ dryRun: true });
if (result?.nextRelease) {
  const { version } = parseReleaseVersion(result.nextRelease.version);
  await appendFile(process.env.GITHUB_OUTPUT, `version=${version}\n`, 'utf8');
}
