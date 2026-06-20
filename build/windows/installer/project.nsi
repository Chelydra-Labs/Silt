Unicode true

####
## Custom Silt NSIS installer template (#install-scope).
## Overrides Wails' default per-machine-only template with a MultiUser
## installer that lets the user CHOOSE: "Install for all users" (needs admin)
## or "Install for just me" (no admin), defaulting to per-user.
##
## This file is automatically used by `wails build --nsis` because it lives at
## build/windows/installer/project.nsi, which takes precedence over the Wails
## embedded template.
####

## Override the execution level BEFORE the tools include so wails_tools.nsh
## does not force "admin". "Highest" lets NSIS request elevation only when the
## user picks "all users"; per-user installs run without elevation.
!define REQUEST_EXECUTION_LEVEL "Highest"

## Include the wails tools (fills in INFO_*, PRODUCT_EXECUTABLE, macros, etc.)
!include "wails_tools.nsh"

# Version info
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"
VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!include "MUI.nsh"

##
## MultiUser configuration — the install-scope choice dialog.
## Defaults to CurrentUser (per-user, no admin). The user can switch to
## AllUsers (per-machine, admin) via the radio buttons on the install-mode
## page. See https://nsis.sourceforge.io/Docs/MultiUser/Readme.html
##
!define MULTIUSER_EXECUTIONLEVEL Highest
!define MULTIUSER_MUI
!define MULTIUSER_INSTALLMODE_DEFAULT_CURRENTUSER
!define MULTIUSER_INSTALLMODE_INSTDIR "${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!define MULTIUSER_INSTALLMODE_COMMANDLINE
!define MULTIUSER_INSTALLMODE_INSTDIR_REGISTRY_KEY   "Software\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!define MULTIUSER_INSTALLMODE_UNINSTALL_REGISTRY_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${UNINST_KEY_NAME}"
!define MULTIUSER_INSTALLMODE_INSTDIR_REGISTRY_VALUENAME "InstallDir"
!include "MultiUser.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

## Pages: Welcome → Install Mode (the choice) → Directory → Install → Finish
!insertmacro MUI_PAGE_WELCOME
!insertmacro MULTIUSER_PAGE_INSTALLMODE
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
## InstallDir is set dynamically by MultiUser.nsh based on the chosen scope:
##   CurrentUser → $LOCALAPPDATA\Programs\<Company>\<Product>
##   AllUsers    → $PROGRAMFILES64\<Company>\<Product>
ShowInstDetails show

Function .onInit
    ## Initialise MultiUser — this reads the registry for a prior install
    ## scope and sets $InstDir + $MultiUser.InstallMode accordingly.
    !insertmacro MULTIUSER_INIT

    ## Architecture guard (from wails_tools.nsh).
    !insertmacro wails.checkArchitecture
FunctionEnd

Section
    ## Set the shell context (start-menu / desktop shortcuts) to match the
    ## chosen install scope. The default wails.setShellContext macro checks
    ## REQUEST_EXECUTION_LEVEL, but with MultiUser the scope is runtime-chosen,
    ## so we drive it from $MultiUser.InstallMode instead.
    ${If} $MultiUser.InstallMode == "AllUsers"
        SetShellVarContext all
    ${Else}
        SetShellVarContext current
    ${EndIf}

    !insertmacro wails.webview2runtime

    ## If a prior version is installed at this $INSTDIR, silently run its
    ## uninstaller first for a clean upgrade (no leftover stale files). The
    ## MultiUser plugin has already detected the prior scope, so we read the
    ## uninstall string from the matching hive.
    SetRegView 64
    ${If} $MultiUser.InstallMode == "AllUsers"
        ReadRegStr $0 HKLM "${UNINST_KEY}" "UninstallString"
    ${Else}
        ReadRegStr $0 HKCU "${UNINST_KEY}" "UninstallString"
    ${EndIf}
    ${If} $0 != ""
        ExecWait '"$0" /S _?=$INSTDIR'
    ${EndIf}

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    ## Write uninstaller + registry entries. The Wails macro hardcodes HKLM,
    ## which fails for per-user installs. We write to the correct hive based
    ## on the chosen scope so upgrades + Add/Remove Programs work either way.
    WriteUninstaller "$INSTDIR\uninstall.exe"
    SetRegView 64
    ${If} $MultiUser.InstallMode == "AllUsers"
        WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
        WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
        WriteRegStr HKLM "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"
    ${Else}
        WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
        WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
        WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"
    ${EndIf}
    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0
    ${If} $MultiUser.InstallMode == "AllUsers"
        WriteRegDWORD HKLM "${UNINST_KEY}" "EstimatedSize" "$0"
    ${Else}
        WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
    ${EndIf}
SectionEnd

Section "uninstall"
    ${If} $MultiUser.InstallMode == "AllUsers"
        SetShellVarContext all
    ${Else}
        SetShellVarContext current
    ${EndIf}

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"
    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    ## Delete uninstaller + registry from the correct hive (NOT wails.deleteUninstaller
    ## which hardcodes HKLM).
    Delete "$INSTDIR\uninstall.exe"
    SetRegView 64
    ${If} $MultiUser.InstallMode == "AllUsers"
        DeleteRegKey HKLM "${UNINST_KEY}"
    ${Else}
        DeleteRegKey HKCU "${UNINST_KEY}"
    ${EndIf}
SectionEnd

Function un.onInit
    !insertmacro MULTIUSER_UNINIT
FunctionEnd
