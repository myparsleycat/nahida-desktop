import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const project = await readFile(
  new URL('../build/windows/nsis/project.nsi', import.meta.url),
  'utf8',
);
const app = await readFile(new URL('../internal/app/app.go', import.meta.url), 'utf8');

test('keeps the NSIS running-app mutex in sync with the Wails single-instance ID', () => {
  const uniqueID = app.match(/UniqueID:\s*"([^"]+)"/)?.[1];
  const mutex = project.match(/!define APP_SINGLE_INSTANCE_MUTEX "([^"]+)"/)?.[1];

  assert.ok(uniqueID, 'Wails single-instance UniqueID is missing');
  assert.equal(mutex, `wails-app-${uniqueID}-sim`);
});

test('blocks uninstall while the app mutex is held', () => {
  const init = project.slice(
    project.indexOf('Function un.onInit'),
    project.indexOf('FunctionEnd', project.indexOf('Function un.onInit')),
  );

  assert.match(init, /OpenMutexW/);
  assert.match(init, /MB_RETRYCANCEL/);
  assert.match(init, /IfSilent appRunningSilent/);
  assert.match(init, /SetErrorLevel 2\s+Abort/);
});

test('preserves uninstall registration when executable removal fails', () => {
  const section = project.slice(
    project.indexOf('Section "uninstall"'),
    project.indexOf('SectionEnd', project.indexOf('Section "uninstall"')),
  );
  const clearErrors = section.indexOf('ClearErrors');
  const removeExecutable = section.indexOf('Delete "$INSTDIR\\${PRODUCT_EXECUTABLE}"');
  const checkErrors = section.indexOf('IfErrors uninstallFailed');
  const deleteRegistration = section.indexOf('!insertmacro wails.deleteUninstaller');
  const failure = section.indexOf('uninstallFailed:');

  assert.ok(clearErrors >= 0 && clearErrors < removeExecutable);
  assert.ok(removeExecutable < checkErrors);
  assert.ok(checkErrors < deleteRegistration);
  assert.ok(deleteRegistration < failure);
  assert.doesNotMatch(section, /RMDir \/r "\$INSTDIR"/);
  assert.match(section.slice(failure), /SetErrorLevel 1/);
  assert.match(section.slice(failure), /Abort "Uninstallation stopped/);
});
