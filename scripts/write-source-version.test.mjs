import assert from 'node:assert/strict';
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { writeSourceVersion } from './write-source-version.mjs';

const root = path.resolve(import.meta.dirname, '..');
const sourceFiles = [
  'build/windows/nahida-desktop.winres.json',
  'build/windows/elevated-helper.winres.json',
  'build/windows/nahida-desktop.exe.manifest',
  'internal/platform/shell.go',
];

test('updates every tracked application version source', async (t) => {
  const fixture = await createFixture(t);

  await writeSourceVersion(fixture, '3.1.0-beta.7');

  const [appResources, helperResources, manifest, platform] = await Promise.all(
    sourceFiles.map((file) => readFile(path.join(fixture, file), 'utf8')),
  );

  for (const resources of [appResources, helperResources]) {
    const info = JSON.parse(resources).RT_VERSION['#1']['0000'];
    assert.equal(info.fixed.file_version, '3.1.0.7');
    assert.equal(info.info['0000'].ProductVersion, '3.1.0-beta.7');
  }
  assert.match(manifest, /name="com\.nahida\.desktop" version="3\.1\.0\.7"/);
  assert.match(platform, /var AppVersion = "3\.1\.0-beta\.7"/);
});

test('does not write any source when a version marker is missing', async (t) => {
  const fixture = await createFixture(t);
  const platformPath = path.join(fixture, 'internal/platform/shell.go');
  await writeFile(
    platformPath,
    (await readFile(platformPath, 'utf8')).replace('var AppVersion =', 'var MissingVersion ='),
  );
  const before = await Promise.all(
    sourceFiles.map((file) => readFile(path.join(fixture, file), 'utf8')),
  );

  await assert.rejects(
    writeSourceVersion(fixture, '3.2.0'),
    /Expected exactly one version field in internal\/platform\/shell\.go; found 0/,
  );

  const after = await Promise.all(
    sourceFiles.map((file) => readFile(path.join(fixture, file), 'utf8')),
  );
  assert.deepEqual(after, before);
});

async function createFixture(t) {
  const fixture = await mkdtemp(path.join(tmpdir(), 'nahida-source-version-'));
  t.after(() => rm(fixture, { recursive: true, force: true }));

  for (const file of sourceFiles) {
    await mkdir(path.dirname(path.join(fixture, file)), { recursive: true });
    await cp(path.join(root, file), path.join(fixture, file));
  }
  return fixture;
}
