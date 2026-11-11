# Product definitions and install macros for project.nsi. The installer is per-user and amd64 only.

!include "x64.nsh"
!include "WinVer.nsh"
!include "FileFunc.nsh"

!define INFO_PROJECTNAME "nahida-desktop"
!define INFO_COMPANYNAME "nahida.live"
!define INFO_PRODUCTNAME "Nahida Desktop"
!define INFO_COPYRIGHT "© 2026, nahida.live"
!define PRODUCT_EXECUTABLE "${INFO_PROJECTNAME}.exe"

# internal/platform reads DisplayVersion from this key; keep the name stable across releases.
!define UNINST_KEY_NAME "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${UNINST_KEY_NAME}"

!define PROTOCOL_SCHEME "nahida"
!define PROTOCOL_DESCRIPTION "Nahida Desktop deep link"

!ifndef APP_BINARY
    !error "Pass the application executable with -DAPP_BINARY=<path>."
!endif
!ifndef WEBVIEW2_BOOTSTRAPPER
    !error "Pass the WebView2 bootstrapper with -DWEBVIEW2_BOOTSTRAPPER=<path>."
!endif
!define ARCH "amd64"

RequestExecutionLevel user

!macro nahida.checkArchitecture
    ${If} ${AtLeastWin10}
        ${If} ${IsNativeAMD64}
            Goto archOk
        ${EndIf}

        IfSilent 0 archNotSilent
            SetErrorLevel 65
            Abort
        archNotSilent:
            MessageBox MB_OK "This product can't be installed on the current Windows architecture. Supports: ${ARCH}"
            Quit
    ${Else}
        IfSilent 0 winNotSilent
            SetErrorLevel 64
            Abort
        winNotSilent:
            MessageBox MB_OK "This product is only supported on Windows 10 (Server 2016) and later."
            Quit
    ${EndIf}

    archOk:
!macroend

!macro nahida.files
    File "/oname=${PRODUCT_EXECUTABLE}" "${APP_BINARY}"
!macroend

!macro nahida.ensureWebView2
    SetRegView 64
    ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    ${If} $0 != ""
    ${AndIf} $0 != "0.0.0.0"
        Goto webView2Done
    ${EndIf}
    ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    ${If} $0 != ""
    ${AndIf} $0 != "0.0.0.0"
        Goto webView2Done
    ${EndIf}

    DetailPrint "Installing WebView2 Runtime for GameBanana sign-in."
    InitPluginsDir
    SetOutPath "$PLUGINSDIR"
    File "/oname=MicrosoftEdgeWebview2Setup.exe" "${WEBVIEW2_BOOTSTRAPPER}"
    ClearErrors
    ExecWait '"$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install' $0
    ${If} ${Errors}
    ${OrIf} $0 != 0
        DetailPrint "WebView2 Runtime installation failed. GameBanana automatic sign-in requires WebView2."
    ${EndIf}

    webView2Done:
    ClearErrors
!macroend

!macro nahida.writeUninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"

    SetRegView 64
    WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${NAHIDA_SEMVER}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"

    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0
    WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
!macroend

!macro nahida.deleteUninstaller
    Delete "$INSTDIR\uninstall.exe"

    SetRegView 64
    DeleteRegKey HKCU "${UNINST_KEY}"
!macroend

!macro nahida.registerProtocol
    DeleteRegKey HKCU "Software\Classes\${PROTOCOL_SCHEME}"
    WriteRegStr HKCU "Software\Classes\${PROTOCOL_SCHEME}" "" "${PROTOCOL_DESCRIPTION}"
    WriteRegStr HKCU "Software\Classes\${PROTOCOL_SCHEME}" "URL Protocol" ""
    WriteRegStr HKCU "Software\Classes\${PROTOCOL_SCHEME}\DefaultIcon" "" "$INSTDIR\${PRODUCT_EXECUTABLE},0"
    WriteRegStr HKCU "Software\Classes\${PROTOCOL_SCHEME}\shell" "" ""
    WriteRegStr HKCU "Software\Classes\${PROTOCOL_SCHEME}\shell\open" "" ""
    WriteRegStr HKCU "Software\Classes\${PROTOCOL_SCHEME}\shell\open\command" "" "$INSTDIR\${PRODUCT_EXECUTABLE} $\"%1$\""
!macroend

!macro nahida.unregisterProtocol
    DeleteRegKey HKCU "Software\Classes\${PROTOCOL_SCHEME}"
!macroend
