import process from 'node:process';

const [version] = process.argv.slice(2);
if (!version || process.env.ATTESTED_RELEASE_VERSION !== version) {
  throw new Error(`Missing build attestation for release ${version ?? '(unknown)'}`);
}
