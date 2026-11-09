; Installer hook for bowel on Windows.
;
; TUN mode needs an administrator: mihomo is bowel's child, and a process
; cannot open a TUN device with more rights than it has. macOS works around
; this with a setuid copy; Windows has no per-file privilege, so the app has
; to start elevated. This marks the executable RUNASADMIN, the same effect as
; an embedded requireAdministrator manifest, without touching the binary.
; ASCII only: NSIS reads an included file without a BOM as ANSI.

!macro customInstall
  WriteRegStr HKCU "Software\Microsoft\Windows NT\CurrentVersion\AppCompatFlags\Layers" "$INSTDIR\bowel.exe" "RUNASADMIN"
!macroend

!macro customUnInstall
  DeleteRegValue HKCU "Software\Microsoft\Windows NT\CurrentVersion\AppCompatFlags\Layers" "$INSTDIR\bowel.exe"
!macroend
