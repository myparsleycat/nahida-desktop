import assert from 'node:assert/strict';
import test from 'node:test';

import {
  createNSISVersionInclude,
  createWindowsVersionInfo,
  parseReleaseVersion,
} from './release-version.mjs';

const baseInfo = {
  fixed: { file_version: '3.0.0' },
  info: { '0000': { ProductName: 'Nahida Desktop' } },
};

test('maps beta SemVer to a Windows prerelease version', () => {
  const { release, info } = createWindowsVersionInfo(baseInfo, '3.0.0-beta.42');
  assert.equal(release.numericVersion, '3.0.0.42');
  assert.equal(info.fixed.file_version, '3.0.0.42');
  assert.equal(info.fixed.product_version, '3.0.0.42');
  assert.equal(info.fixed.flags, 'Prerelease');
  assert.equal(info.info['0000'].FileVersion, '3.0.0-beta.42');
  assert.equal(info.info['0000'].ProductVersion, '3.0.0-beta.42');
});

test('maps stable SemVer above every beta in the Windows numeric version', () => {
  const { release, info } = createWindowsVersionInfo(baseInfo, '3.0.0');
  assert.equal(release.numericVersion, '3.0.0.65535');
  assert.equal(info.fixed.file_version, '3.0.0.65535');
  assert.equal(info.fixed.product_version, '3.0.0.65535');
  assert.equal(info.fixed.flags, undefined);
  assert.equal(info.info['0000'].ProductVersion, '3.0.0');
});

test('writes the same SemVer and numeric version for NSIS', () => {
  assert.equal(
    createNSISVersionInclude('3.0.0-beta.7'),
    '!define NAHIDA_SEMVER "3.0.0-beta.7"\n' +
      '!define NAHIDA_NUMERIC_VERSION "3.0.0.7"\n',
  );
});

test('accepts the documented beta bounds', () => {
  assert.equal(parseReleaseVersion('3.0.0-beta.1').beta, 1);
  assert.equal(parseReleaseVersion('3.0.0-beta.65534').beta, 65_534);
});

test('rejects unsupported or unsafe release versions', () => {
  for (const version of [
    '',
    'v3.0.0',
    '3.0',
    '3.0.0-beta.0',
    '3.0.0-beta.01',
    '3.0.0-beta.65535',
    '3.0.0-alpha.1',
    '3.0.0-rc.1',
    '3.0.0+build.1',
    '65536.0.0',
  ]) {
    assert.throws(() => parseReleaseVersion(version), undefined, version);
  }
});
