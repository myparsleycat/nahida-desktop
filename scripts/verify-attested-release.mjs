import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import process from 'node:process';

const executable = 'nahida-desktop-windows-amd64.exe';
const bundlePath = 'bin/nahida-desktop-windows-amd64.sigstore.json';
const versionSubject = 'nahida-desktop-release-version';

const [version] = process.argv.slice(2);
if (!version || process.env.ATTESTED_RELEASE_VERSION !== version) {
  throw new Error(`Missing build attestation for release ${version ?? '(unknown)'}`);
}

// The self-updater parses the bundle as a single JSON object and rejects any release whose executable digest and
// version it does not attest, so a mismatch here would publish a release that no installed copy can update to.
const bundle = JSON.parse((await readFile(bundlePath, 'utf8')).trim());
const statement = JSON.parse(Buffer.from(bundle.dsseEnvelope.payload, 'base64').toString('utf8'));
const attested = (name) => statement.subject.find((subject) => subject.name === name)?.digest?.sha256;
const actual = createHash('sha256').update(await readFile(`bin/${executable}`)).digest('hex');
if (attested(executable) !== actual) {
  throw new Error(`${bundlePath} does not attest bin/${executable} (sha256 ${actual})`);
}
if (attested(versionSubject) !== createHash('sha256').update(version).digest('hex')) {
  throw new Error(`${bundlePath} does not attest release version ${version}`);
}
