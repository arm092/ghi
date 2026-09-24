from pathlib import Path
from zipfile import ZipFile, ZipInfo, ZIP_DEFLATED
root = Path(__file__).resolve().parent.parent
output = root/'dist/ghi_v0.2.1_macos_installer_kit.zip'
output.parent.mkdir(parents=True, exist_ok=True)
files = {
    'scripts/package-macos.sh': (root/'scripts/package-macos.sh').read_bytes(),
    'install/macos/preinstall': (root/'install/macos/preinstall').read_bytes(),
    'LICENSE': (root/'LICENSE').read_bytes(),
    'Build Installer.command': (root/'install/macos/Build Installer.command').read_bytes(),
    'README.txt': b'''Ghi 0.2.1 macOS Installer Build Kit

This is a build kit, not a prebuilt or verified installer.
Extract the ZIP, then run Build Installer.command on your Mac.
Alternatively, open Terminal in the extracted directory and run:

    bash scripts/package-macos.sh 0.2.1

Requirements: macOS, internet access and Apple Command Line Tools.
If the Apple tools are missing, install them using xcode-select --install.
The script downloads the released Intel and Apple silicon binaries,
checks SHA-256 hashes and builds a universal package using Apple tools.

Output: dist/ghi_v0.2.1_macos_universal.pkg

The package is unsigned unless Developer ID identities are configured.
Native installation has not been verified yet. Share the build output
and package with the coordinating task before publication.
The package needs administrator access and a signed-in desktop user.
It prepares Go automatically and refuses to overwrite a Homebrew install.

Source: https://github.com/arm092/ghi
'''
}
with ZipFile(output, 'w', ZIP_DEFLATED) as z:
    for name, data in files.items():
        info = ZipInfo('ghi-macos-installer/'+name)
        info.create_system = 3
        info.external_attr = ((0o100755 if name.endswith(('.sh', '.command', '/preinstall')) else 0o100644) << 16)
        info.compress_type = ZIP_DEFLATED
        z.writestr(info, data.replace(b'\r\n', b'\n'))
with ZipFile(output) as z:
    assert z.testzip() is None
import hashlib
digest = hashlib.sha256(output.read_bytes()).hexdigest()
Path(str(output)+'.sha256').write_text(digest+'  '+output.name+'\n', encoding='ascii')
print(output)
print(digest)
