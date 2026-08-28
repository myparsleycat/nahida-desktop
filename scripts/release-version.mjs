const VERSION_PATTERN = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-beta\.([1-9]\d*))?$/;
const MAX_COMPONENT = 65_535;
const MAX_BETA = 65_534;

export function parseReleaseVersion(input) {
  const version = String(input ?? '').trim();
  const match = VERSION_PATTERN.exec(version);
  if (!match) {
    throw new Error(
      `Unsupported release version ${JSON.stringify(version)}; expected X.Y.Z or X.Y.Z-beta.N`,
    );
  }

  const [major, minor, patch] = match.slice(1, 4).map(Number);
  for (const [name, value] of Object.entries({ major, minor, patch })) {
    if (value > MAX_COMPONENT) {
      throw new Error(`${name} version component must be at most ${MAX_COMPONENT}`);
    }
  }

  const beta = match[4] === undefined ? null : Number(match[4]);
  if (beta !== null && beta > MAX_BETA) {
    throw new Error(`beta number must be between 1 and ${MAX_BETA}`);
  }

  return {
    version,
    major,
    minor,
    patch,
    beta,
    prerelease: beta !== null,
    numericVersion: `${major}.${minor}.${patch}.${beta ?? MAX_COMPONENT}`,
  };
}

export function createWindowsVersionInfo(baseInfo, input) {
  const release = parseReleaseVersion(input);
  const info = structuredClone(baseInfo);
  info.fixed = {
    ...info.fixed,
    file_version: release.numericVersion,
    product_version: release.numericVersion,
  };
  if (release.prerelease) {
    info.fixed.flags = 'Prerelease';
  } else {
    delete info.fixed.flags;
  }

  info.info ??= {};
  info.info['0000'] ??= {};
  info.info['0000'].FileVersion = release.version;
  info.info['0000'].ProductVersion = release.version;
  return { release, info };
}

export function createNSISVersionInclude(input) {
  const release = parseReleaseVersion(input);
  return [
    `!define NAHIDA_SEMVER "${release.version}"`,
    `!define NAHIDA_NUMERIC_VERSION "${release.numericVersion}"`,
    '',
  ].join('\n');
}
