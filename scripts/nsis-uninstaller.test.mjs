import assert from 'node:assert/strict';
import { readdir, readFile } from 'node:fs/promises';
import test from 'node:test';

const project = await readFile(
  new URL('../build/windows/nsis/project.nsi', import.meta.url),
  'utf8',
);
test('keeps the NSIS running-app mutex in sync with the fairy single-instance ID', async () => {
  const appDirectory = new URL('../internal/app/', import.meta.url);
  const sources = await Promise.all([
    readFile(new URL('../main.go', import.meta.url), 'utf8'),
    ...(await readdir(appDirectory))
      .filter((file) => file.endsWith('.go') && !file.endsWith('_test.go'))
      .map((file) => readFile(new URL(file, appDirectory), 'utf8')),
  ]);
  const ids = sources.flatMap((source) =>
    [...source.matchAll(/fairy\.SingleInstance\(\s*"([^"]+)"/g)].map((match) => match[1]),
  );
  const mutex = project.match(/!define APP_SINGLE_INSTANCE_MUTEX "([^"]+)"/)?.[1];

  assert.equal(ids.length, 1, 'expected one fairy.SingleInstance call with a literal ID');
  // Fairy names the mutex Local\nahida.fairy.<ID>.
  assert.equal(mutex, `Local\\nahida.fairy.${ids[0]}`);
});

test('blocks uninstall while the app mutex is held', () => {
  const init = project.slice(
    project.indexOf('Function un.onInit'),
    project.indexOf('FunctionEnd', project.indexOf('Function un.onInit')),
  );

  // A null handle is the only way past the guard; any other result reaches a retry or an Abort.
  assert.match(
    init,
    /checkAppRunning:\s+System::Call 'kernel32::OpenMutexW\([^)]*\$\{APP_SINGLE_INSTANCE_MUTEX\}"\) p\.R0'\s+IntPtrCmp \$R0 0 appStopped\s/,
  );
  const held = init.slice(init.indexOf('IntPtrCmp $R0 0 appStopped'), init.indexOf('appStopped:'));
  assert.match(
    held,
    /IfSilent appRunningSilent\s+MessageBox MB_RETRYCANCEL[^\n]*IDRETRY checkAppRunning\s+Abort\s+appRunningSilent:\s+SetErrorLevel 2\s+Abort\s*$/,
  );
  assert.doesNotMatch(held, /Goto appStopped/);
});

test('preserves uninstall registration when executable removal fails', () => {
  const section = project.slice(
    project.indexOf('Section "uninstall"'),
    project.indexOf('SectionEnd', project.indexOf('Section "uninstall"')),
  );
  const clearErrors = section.indexOf('ClearErrors');
  const removeExecutable = section.indexOf('Delete "$INSTDIR\\${PRODUCT_EXECUTABLE}"');
  const checkErrors = section.indexOf('IfErrors uninstallFailed');
  const deleteRegistration = section.indexOf('!insertmacro nahida.deleteUninstaller');
  const failure = section.indexOf('uninstallFailed:');

  assert.ok(clearErrors >= 0 && clearErrors < removeExecutable);
  assert.ok(removeExecutable < checkErrors);
  assert.ok(checkErrors < deleteRegistration);
  assert.ok(deleteRegistration < failure);
  assert.doesNotMatch(section, /RMDir \/r "\$INSTDIR"/);
  assert.match(section.slice(failure), /SetErrorLevel 1/);
  assert.match(section.slice(failure), /Abort "Uninstallation stopped/);
});
