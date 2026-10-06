#!/usr/bin/env python3
"""Audit the actual ELF bytes in every APK/AAB ABI, including unexpected libraries."""
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import zipfile

EXPECTED = {'libavutil.so', 'libavcodec.so', 'libavformat.so', 'libudfread.so', 'libmattmux_jni.so', 'libmediainfo_jni.so'}
ABIS = {'arm64-v8a', 'x86_64'}

for filename in sys.argv[1:]:
    with zipfile.ZipFile(filename) as archive, tempfile.TemporaryDirectory() as tmp:
        prefix = 'base/' if Path(filename).suffix == '.aab' else ''
        libraries = [name for name in archive.namelist() if name.startswith(prefix + 'lib/') and name.endswith('.so')]
        found = {}
        for name in libraries:
            parts = name[len(prefix):].split('/')
            if len(parts) != 3:
                raise SystemExit(f'{filename}: unexpected library path {name}')
            _, abi, lib = parts
            found.setdefault(abi, set()).add(lib)
            # Never extract archive paths. Inspect bytes in one private regular file.
            elf = Path(tmp) / 'library.so'
            elf.write_bytes(archive.read(name))
            program = subprocess.check_output(['readelf', '-lW', str(elf)], text=True)
            loads = [line.split()[-1] for line in program.splitlines() if line.lstrip().startswith('LOAD ')]
            if not loads or any(int(value, 16) < 16384 for value in loads):
                raise SystemExit(f'{filename}: ELF LOAD alignment below 16 KB: {name}')
            dynamic = subprocess.check_output(['readelf', '-dW', str(elf)], text=True)
            needed = '\n'.join(line for line in dynamic.splitlines() if '(NEEDED)' in line)
            if lib == 'libmattmux_jni.so':
                symbols = subprocess.check_output(['readelf', '-Ws', str(elf)], text=True)
                if 'Java_io_github_maas3n_mattmux_AndroidNativeRemuxEngine_nativeScanDvdNav' not in symbols:
                    raise SystemExit(f'{filename}: DVDNav JNI scanner entry point missing: {name}')
            if re.search(r'dvdcss|x264|x265|xvid|libav\w+\.so\.', needed, re.I):
                raise SystemExit(f'{filename}: forbidden or versioned dependency: {name}\n{needed}')
        if set(found) != ABIS or any(found[abi] != EXPECTED for abi in ABIS):
            raise SystemExit(f'{filename}: unexpected native package contents: {found}')
        build_info_path = prefix + 'assets/ffmpeg/ffmpeg-build-info.txt'
        build_info = archive.read(build_info_path).decode('utf-8', errors='strict')
        css_enabled = 'CSS support: libdvdcss 1.6.0' in build_info
        notices = ['COPYING.LGPLv2.1', 'LIBUDFREAD_COPYING.txt', 'DVDREAD_COPYING.txt', 'DVDNAV_COPYING.txt',
                   'ffmpeg-build-info.txt', 'MediaInfoLib-License.html', 'ZenLib-License.txt', 'NDK-NOTICE.txt']
        if css_enabled:
            notices.append('LIBDVDCSS_COPYING.txt')
        for notice in notices:
            if not archive.read(prefix + 'assets/ffmpeg/' + notice):
                raise SystemExit(f'{filename}: missing/empty notice {notice}')
        css_label = 'CSS-capable' if css_enabled else 'CSS-free'
        print(f'{filename}: both ABIs, all native ELFs, 16 KB alignment, dependency and notice audit PASS ({css_label})')
