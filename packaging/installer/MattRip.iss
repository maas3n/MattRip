#define MyAppName "MattRip"
#ifndef MyAppVersion
  #define MyAppVersion "0.0.0"
#endif
#define MyAppExeName "MattRip.exe"

[Setup]
AppId={{A861F62F-CA3B-4B12-9D83-6E6D9AE4E0C1}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
DefaultDirName={autopf}\MattRip
DefaultGroupName=MattRip
DisableProgramGroupPage=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
OutputDir=..\..\dist
#ifdef ThinSetup
OutputBaseFilename=MattRip-{#MyAppVersion}-Thin-Setup
#else
OutputBaseFilename=MattRip-{#MyAppVersion}-Setup
#endif
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
UninstallDisplayIcon={app}\{#MyAppExeName}
VersionInfoVersion={#MyAppVersion}.0
VersionInfoProductName=MattRip
VersionInfoDescription=MattRip DVD remuxer

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional shortcuts:"; Flags: unchecked

[Files]
Source: "..\..\MattRip.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\mattrip-cli.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\src\MattRip.exe.manifest"; DestDir: "{app}"; DestName: "MattRip.exe.manifest"; Flags: ignoreversion
Source: "..\..\src\MattRip.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\THIRD_PARTY.md"; DestDir: "{app}"; Flags: ignoreversion
#ifndef ThinSetup
Source: "bundled-tools\ffmpeg-2026-09-08\*"; DestDir: "{localappdata}\MattRip\tools\ffmpeg-2026-09-08"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "bundled-tools\mediainfo-26.05\MediaInfo.exe"; DestDir: "{localappdata}\MattRip\tools\mediainfo-26.05"; Flags: ignoreversion
#endif

[Icons]
Name: "{autoprograms}\MattRip"; Filename: "{app}\{#MyAppExeName}"; WorkingDir: "{app}"
Name: "{autodesktop}\MattRip"; Filename: "{app}\{#MyAppExeName}"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "Launch MattRip"; Flags: nowait postinstall skipifsilent