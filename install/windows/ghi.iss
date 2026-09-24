#ifndef ReleaseVersion
  #error ReleaseVersion is required
#endif
#ifndef BundleRoot
  #error BundleRoot is required
#endif
#ifndef RepoRoot
  #error RepoRoot is required
#endif
#ifndef OutputRoot
  #error OutputRoot is required
#endif

[Setup]
AppId=am.ghi.compiler
AppName=Ghi
AppVersion={#ReleaseVersion}
AppPublisher=Ghi
AppPublisherURL=https://github.com/arm092/ghi
AppSupportURL=https://github.com/arm092/ghi/issues
DefaultDirName={localappdata}\Ghi
PrivilegesRequired=lowest
ArchitecturesAllowed=x64os or arm64
ArchitecturesInstallIn64BitMode=x64os or arm64
DisableProgramGroupPage=yes
LicenseFile={#RepoRoot}\LICENSE
OutputDir={#OutputRoot}
OutputBaseFilename=ghi_v{#ReleaseVersion}_windows_setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ChangesEnvironment=yes
UninstallDisplayName=Ghi and Mojave
CloseApplications=yes

[Files]
Source: "{#BundleRoot}\windows-amd64\ghi.exe"; DestDir: "{app}\bin"; Check: not IsArm64; Flags: ignoreversion
Source: "{#BundleRoot}\windows-amd64\mojave.exe"; DestDir: "{app}\bin"; Check: not IsArm64; Flags: ignoreversion
Source: "{#BundleRoot}\windows-arm64\ghi.exe"; DestDir: "{app}\bin"; Check: IsArm64; Flags: ignoreversion
Source: "{#BundleRoot}\windows-arm64\mojave.exe"; DestDir: "{app}\bin"; Check: IsArm64; Flags: ignoreversion
Source: "{#BundleRoot}\windows-amd64\ghi.exe"; DestName: "ghi-amd64.exe"; Flags: dontcopy
Source: "{#BundleRoot}\windows-arm64\ghi.exe"; DestName: "ghi-arm64.exe"; Flags: dontcopy
Source: "{#RepoRoot}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#RepoRoot}\THIRD_PARTY_NOTICES"; DestDir: "{app}"; Flags: ignoreversion

[Code]
const
  OwnerKey = 'Software\Ghi\Installer';

function SamePath(Entry, Target: String): Boolean;
begin
  Entry := Trim(Entry);
  if (Length(Entry) > 1) and (Entry[1] = '"') and (Entry[Length(Entry)] = '"') then
    Entry := Copy(Entry, 2, Length(Entry) - 2);
  Result := CompareText(RemoveBackslashUnlessRoot(Entry), RemoveBackslashUnlessRoot(Target)) = 0;
end;

function FilterPath(Value, Target: String; var Found: Boolean): String;
var
  I: Integer;
  Entry: String;
  Kept: Boolean;
begin
  Result := '';
  Found := False;
  Kept := False;
  repeat
    I := Pos(';', Value);
    if I = 0 then Entry := Value
    else begin Entry := Copy(Value, 1, I - 1); Delete(Value, 1, I); end;
    if SamePath(Entry, Target) then Found := True
    else begin
      if Kept then Result := Result + ';';
      Result := Result + Entry;
      Kept := True;
    end;
  until I = 0;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Binary: String;
  ExitCode: Integer;
begin
  Result := '';
  if IsArm64 then Binary := 'ghi-arm64.exe' else Binary := 'ghi-amd64.exe';
  ExtractTemporaryFile(Binary);
  WizardForm.StatusLabel.Caption := 'Checking Go; downloading a compatible version if needed...';
  if not Exec(ExpandConstant('{tmp}\') + Binary, 'setup', '', SW_HIDE, ewWaitUntilTerminated, ExitCode) then
    Result := 'Could not start Go setup. Retry the installer.'
  else if ExitCode <> 0 then
    Result := 'Go setup failed. Check your internet connection and retry. Existing Ghi files have not been replaced.';
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  Value, Target, Ignored: String;
  Found: Boolean;
begin
  if CurStep = ssPostInstall then begin
    Target := ExpandConstant('{app}\bin');
    RegQueryStringValue(HKCU, 'Environment', 'Path', Value);
    Ignored := FilterPath(Value, Target, Found);
    if not Found then begin
      if Value <> '' then Value := Value + ';';
      if not RegWriteExpandStringValue(HKCU, 'Environment', 'Path', Value + Target) then
        RaiseException('Could not update your user PATH.');
      RegWriteStringValue(HKCU, OwnerKey, 'AddedPath', Target);
    end;
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Value, Target, Updated: String;
  Found: Boolean;
begin
  if CurUninstallStep = usUninstall then begin
    if RegQueryStringValue(HKCU, OwnerKey, 'AddedPath', Target) and
       SamePath(Target, ExpandConstant('{app}\bin')) then begin
      if RegQueryStringValue(HKCU, 'Environment', 'Path', Value) then begin
        Updated := FilterPath(Value, Target, Found);
        if Found then RegWriteExpandStringValue(HKCU, 'Environment', 'Path', Updated);
      end;
      RegDeleteValue(HKCU, OwnerKey, 'AddedPath');
      RegDeleteKeyIfEmpty(HKCU, OwnerKey);
    end;
  end;
end;
