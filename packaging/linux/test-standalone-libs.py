#!/usr/bin/env python3
"""Regression: resolve private GUI dependencies without default library search.

Run on Ubuntu: python3 packaging/linux/test-standalone-libs.py
"""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("bundle", HERE / "bundle-standalone-libs.py")
bundle = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bundle)


class StandaloneLibraries(unittest.TestCase):
    def test_wayland_without_host_gui_packages(self):
        with tempfile.TemporaryDirectory(prefix="mattrip-library-test-") as tmp:
            root = Path(tmp)
            payload = root / "payload"
            payload.mkdir()
            source = root / "probe.c"
            source.write_text('extern void wl_display_disconnect(void *);\n'
                              'void (*volatile probe)(void *) = wl_display_disconnect;\n'
                              'int main(void) { return probe == 0; }\n')
            wayland = next(path for name, path in bundle.dependencies("/usr/lib/x86_64-linux-gnu/libwayland-cursor.so.0").items()
                           if name == "libwayland-client.so.0")
            bundle.bundle(payload)
            # NODEFLIB and --inhibit-cache prevent the probe from falling back
            # to host Wayland even when this development machine has it installed.
            subprocess.run(["gcc", str(source), wayland, "-Wl,-z,nodefaultlib",
                            "-o", str(payload / "mattrip-bin")], check=True)
            for name in ("libc.so.6", "ld-linux-x86-64.so.2"):
                target = root / "lib64" / name
                target.parent.mkdir(exist_ok=True)
                shutil.copyfile(Path("/lib/x86_64-linux-gnu") / name, target)
                target.chmod(0o755)
            loader = [str(root / "lib64/ld-linux-x86-64.so.2"), "--inhibit-cache",
                      "--library-path", str(payload / "lib") + ":" + str(root / "lib64")]
            subprocess.run(loader + [str(payload / "mattrip-bin")], check=True)
            for lib in sorted((payload / "lib").iterdir()):
                result = subprocess.run(loader + ["--list", str(lib)],
                                        check=True, capture_output=True, text=True)
                for line in result.stdout.splitlines():
                    if " => " in line:
                        name, resolved = line.strip().split(" => ", 1)
                        if name not in bundle.HOST_LIBS:
                            self.assertTrue(resolved.startswith(str(payload / "lib") + "/"), line)
            (payload / "lib/libwayland-client.so.0").unlink()
            failure = subprocess.run(loader + [str(payload / "mattrip-bin")], capture_output=True, text=True)
            self.assertNotEqual(failure.returncode, 0)
            self.assertIn("libwayland-client.so.0", failure.stderr)

    def test_missing_dependency_fails_packaging(self):
        with tempfile.TemporaryDirectory(prefix="mattrip-missing-lib-") as tmp:
            root = Path(tmp)
            (root / "lib.c").write_text("int fixture(void) { return 0; }\n")
            (root / "main.c").write_text("extern int fixture(void); int main(void) { return fixture(); }\n")
            lib = root / "libmattrip_fixture.so"
            subprocess.run(["gcc", "-shared", "-fPIC", str(root / "lib.c"),
                            "-Wl,-soname,libmattrip_fixture.so", "-o", str(lib)], check=True)
            app = root / "app"
            subprocess.run(["gcc", str(root / "main.c"), str(lib),
                            "-Wl,-rpath," + tmp, "-o", str(app)], check=True)
            lib.unlink()
            with self.assertRaisesRegex(RuntimeError, "Unresolved build dependency"):
                bundle.dependencies(app)


if __name__ == "__main__":
    unittest.main()
