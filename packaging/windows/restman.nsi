; Built by scripts/package.sh from the staged Windows folder.
; Compile on Linux with: makensis restman.nsi
; Compile on Windows with: makensis restman.nsi

!include "MUI2.nsh"

Name "Restman"
OutFile "@OUTFILE@"
InstallDir "$PROGRAMFILES64\Restman"
RequestExecutionLevel admin
Unicode True

!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "Install"
  SetOutPath "$INSTDIR"
  File "Restman.exe"
  SetOutPath "$INSTDIR\frontend\dist"
  File /r "frontend\dist\*.*"
  CreateDirectory "$SMPROGRAMS"
  CreateShortcut "$SMPROGRAMS\Restman.lnk" "$INSTDIR\Restman.exe"
  CreateShortcut "$DESKTOP\Restman.lnk" "$INSTDIR\Restman.exe"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
SectionEnd

Section "Uninstall"
  Delete "$INSTDIR\Uninstall.exe"
  Delete "$INSTDIR\Restman.exe"
  RMDir /r "$INSTDIR\frontend"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\Restman.lnk"
  Delete "$DESKTOP\Restman.lnk"
SectionEnd
