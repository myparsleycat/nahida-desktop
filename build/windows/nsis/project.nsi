Unicode true

####
## Per-user installer for Nahida Desktop. Product definitions and install macros live in nahida.nsh.
##
## task package builds the application, prepares the WebView2 bootstrapper and runs makensis.
####
!include "nahida.nsh"
!include "..\..\..\bin\release-version.nsh"

# Keep this in sync with the fairy.SingleInstance ID in the application. Fairy
# creates the mutex Local\nahida.fairy.<ID> before the application starts its services.
!define APP_SINGLE_INSTANCE_MUTEX "Local\nahida.fairy.com.nahida.desktop"

!define MODEL_VIEWER_KEY "Software\Classes\Directory\shell\nahida.live.ModelViewer"

!macro nahida.registerModelViewer
    SetRegView 64
    WriteRegStr HKCU "${MODEL_VIEWER_KEY}" "" "모델 뷰어로 보기"
    WriteRegStr HKCU "${MODEL_VIEWER_KEY}" "Icon" '$\"$INSTDIR\${PRODUCT_EXECUTABLE}$\",0'
    WriteRegStr HKCU "${MODEL_VIEWER_KEY}" "MultiSelectModel" "Single"
    WriteRegStr HKCU "${MODEL_VIEWER_KEY}\command" "" '$\"$INSTDIR\${PRODUCT_EXECUTABLE}$\" --model-viewer $\"%1$\"'
    System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
!macroend

!macro nahida.unregisterModelViewer
    SetRegView 64
    DeleteRegKey HKCU "${MODEL_VIEWER_KEY}"
    System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
!macroend

# The version information for this two must consist of 4 parts
VIProductVersion "${NAHIDA_NUMERIC_VERSION}"
VIFileVersion    "${NAHIDA_NUMERIC_VERSION}"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${NAHIDA_SEMVER}"
VIAddVersionKey "FileVersion"     "${NAHIDA_SEMVER}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uninstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\bin\${INFO_PROJECTNAME}-windows-${ARCH}-installer.exe" # Name of the installer's file.
InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
ShowInstDetails show # This will always show the installation details.

Function .onInit
   !insertmacro nahida.checkArchitecture
FunctionEnd

Function un.onInit
    checkAppRunning:
        System::Call 'kernel32::OpenMutexW(i 0x00100000, i 0, w "${APP_SINGLE_INSTANCE_MUTEX}") p.R0'
        IntPtrCmp $R0 0 appStopped

        System::Call 'kernel32::CloseHandle(p $R0)'
        IfSilent appRunningSilent
        MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "${INFO_PRODUCTNAME} is still running.$\r$\n$\r$\nQuit it from the system tray, then click Retry." /SD IDCANCEL IDRETRY checkAppRunning
        Abort

    appRunningSilent:
        SetErrorLevel 2
        Abort

    appStopped:
FunctionEnd

Section
    SetShellVarContext current

    !insertmacro nahida.ensureWebView2
    SetOutPath $INSTDIR

    !insertmacro nahida.files
    # The elevated helper is embedded into the application binary and extracted
    # under the shared per-user app data directory at runtime, so the installer
    # no longer ships a sibling copy that in-place updates could never refresh.
    # The unsigned per-user install still lets an ordinary user replace the
    # extracted binary before it is launched elevated; that is an accepted
    # limitation, not a closed defense.

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro nahida.registerProtocol
    !insertmacro nahida.registerModelViewer

    !insertmacro nahida.writeUninstaller
SectionEnd

Section "uninstall"
    SetShellVarContext current

    # The uninstaller runs from a temporary copy. Ensure its working directory
    # is also outside $INSTDIR before removing installed files.
    SetOutPath "$TEMP"
    ClearErrors
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    # Remove the helper left next to the app by installers that predate the
    # embedded-helper build.
    Delete "$INSTDIR\nahida-elevated-helper.exe"
    IfErrors uninstallFailed

    # Remove the WebView2 data directory left by releases built on Wails.
    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"
    ClearErrors # WebView2 data cleanup is best effort and must not leak an error flag.

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro nahida.unregisterProtocol
    !insertmacro nahida.unregisterModelViewer
    # The application registers its notification identity at startup.
    DeleteRegKey HKCU "Software\Classes\AppUserModelId\com.nahida"

    !insertmacro nahida.deleteUninstaller
    RMDir "$INSTDIR"
    Goto uninstallDone

    uninstallFailed:
        SetErrorLevel 1
        IfSilent uninstallAbort
        MessageBox MB_OK|MB_ICONSTOP "${INFO_PRODUCTNAME} could not be completely removed.$\r$\n$\r$\nClose any program using files in $INSTDIR, then run the uninstaller again." /SD IDOK

    uninstallAbort:
        Abort "Uninstallation stopped because ${PRODUCT_EXECUTABLE} could not be removed."

    uninstallDone:
SectionEnd
