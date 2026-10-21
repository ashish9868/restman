; Built by scripts/package.sh from the staged Windows folder.

Unicode True

!include "MUI2.nsh"
!include "FileFunc.nsh"
!include "x64.nsh"
!insertmacro GetSize

!define MUI_ICON "restman.ico"
!define MUI_UNICON "restman.ico"
!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TITLE "Install Restman"
!define MUI_WELCOMEPAGE_TEXT "Setup installs Restman in Program Files and adds Start menu and desktop shortcuts.$\r$\n$\r$\nCollections stay on this computer.$\r$\n$\r$\nPowered by D2i Technology."
!define MUI_DIRECTORYPAGE_TEXT_TOP "Restman will be installed in this folder."
!define MUI_FINISHPAGE_RUN "$INSTDIR\Restman.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Launch Restman"
!define MUI_FINISHPAGE_NOREBOOTSUPPORT

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Name "Restman"
OutFile "@OUTFILE@"
InstallDir "$PROGRAMFILES64\Restman"
InstallDirRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman" "InstallLocation"
RequestExecutionLevel admin
BrandingText "D2i Technology"

VIProductVersion "@VERSION4@"
VIAddVersionKey /LANG=1033 "ProductName" "Restman"
VIAddVersionKey /LANG=1033 "CompanyName" "D2i Technology"
VIAddVersionKey /LANG=1033 "FileDescription" "Restman Setup"
VIAddVersionKey /LANG=1033 "FileVersion" "@VERSION@"
VIAddVersionKey /LANG=1033 "ProductVersion" "@VERSION@"
VIAddVersionKey /LANG=1033 "LegalCopyright" "D2i Technology"

Function .onInit
  SetShellVarContext all
  ${If} ${RunningX64}
    SetRegView 64
  ${EndIf}
FunctionEnd

Function un.onInit
  SetShellVarContext all
  ${If} ${RunningX64}
    SetRegView 64
  ${EndIf}
FunctionEnd

Section "Install"
  SetOutPath "$INSTDIR"
  File "Restman.exe"
  SetOutPath "$INSTDIR\frontend\dist"
  File /r "frontend\dist\*.*"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateDirectory "$SMPROGRAMS\Restman"
  CreateShortcut "$SMPROGRAMS\Restman\Restman.lnk" "$INSTDIR\Restman.exe" "" "$INSTDIR\Restman.exe" 0
  CreateShortcut "$SMPROGRAMS\Restman\Uninstall.lnk" "$INSTDIR\Uninstall.exe"
  CreateShortcut "$DESKTOP\Restman.lnk" "$INSTDIR\Restman.exe" "" "$INSTDIR\Restman.exe" 0
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman" "DisplayName" "Restman"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman" "DisplayVersion" "@VERSION@"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman" "Publisher" "D2i Technology"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman" "URLInfoAbout" "https://d2itechnology.com/"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman" "DisplayIcon" "$INSTDIR\Restman.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman" "UninstallString" "$\"$INSTDIR\Uninstall.exe$\""
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman" "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman" "EstimatedSize" $0
SectionEnd

Section "Uninstall"
  Delete "$INSTDIR\Uninstall.exe"
  Delete "$INSTDIR\Restman.exe"
  RMDir /r "$INSTDIR\frontend"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\Restman\Restman.lnk"
  Delete "$SMPROGRAMS\Restman\Uninstall.lnk"
  RMDir "$SMPROGRAMS\Restman"
  Delete "$DESKTOP\Restman.lnk"
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Restman"
SectionEnd
