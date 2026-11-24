import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const config = JSON.parse(await readFile(new URL('../.releaserc.json', import.meta.url), 'utf8'));

test('uses stable main and beta v3 release branches', () => {
  assert.deepEqual(config.branches, [
    'main',
    { name: 'v3', channel: 'beta', prerelease: 'beta' },
  ]);
  assert.equal(config.tagFormat, 'v${version}');
});

test('publishes only the four documented draft assets', () => {
  const github = config.plugins.find(
    (plugin) => Array.isArray(plugin) && plugin[0] === '@semantic-release/github',
  );
  assert.ok(github);
  assert.equal(github[1].draftRelease, true);
  assert.deepEqual(github[1].assets, [
    { path: 'bin/nahida-desktop-windows-amd64.exe' },
    { path: 'bin/nahida-desktop-windows-amd64-installer.exe' },
    { path: 'bin/SHA256SUMS' },
    { path: 'bin/nahida-desktop-windows-amd64.sigstore.json' },
  ]);
});

test('attests release binaries before semantic-release can push the tag', async () => {
  const workflow = await readFile(new URL('../.github/workflows/release.yml', import.meta.url), 'utf8');
  const attest = workflow.search(/^\s+uses: actions\/attest@/m);
  const publish = workflow.search(/^\s+run: pnpm exec semantic-release\s*$/m);
  assert.ok(attest !== -1 && publish > attest, 'the release must remain retryable if attestation fails');
  const bundle = workflow.indexOf('BUNDLE_PATH: ${{ steps.attest-release.outputs.bundle-path }}');
  assert.ok(
    bundle > attest && publish > bundle,
    'the attestation bundle must be staged before semantic-release uploads assets',
  );
});

test('commits source versions without publishing to npm', () => {
  const names = config.plugins.map((plugin) => (Array.isArray(plugin) ? plugin[0] : plugin));
  assert.deepEqual(names, [
    '@semantic-release/commit-analyzer',
    '@semantic-release/release-notes-generator',
    '@semantic-release/exec',
    '@semantic-release/git',
    '@semantic-release/github',
  ]);
  assert.ok(!names.includes('@semantic-release/npm'));

  const git = config.plugins.find(
    (plugin) => Array.isArray(plugin) && plugin[0] === '@semantic-release/git',
  );
  assert.deepEqual(git[1], {
    assets: [
      'build/windows/nahida-desktop.winres.json',
      'build/windows/elevated-helper.winres.json',
      'build/windows/nahida-desktop.exe.manifest',
      'internal/platform/shell.go',
    ],
    message: 'chore(release): set version ${nextRelease.version} [skip ci]',
  });
});
