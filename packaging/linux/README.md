# MattRip on Debian / Ubuntu

The Linux port provides two executables from the same source tree:

- `mattrip` — desktop GUI (Fyne)
- `mattrip-cli` — command-line interface

## Runtime tool policy

### Single-file standalone

The standalone build embeds private Wayland, X11, keyboard, and OpenGL dispatch
libraries, their dependencies, and copyright notices alongside its multimedia
tools. The launcher extracts them into its user cache and uses them through a
process-local `LD_LIBRARY_PATH`. Users do not need to install `libwayland-client0`
to start this build, and no administrator access is needed for extraction.

The host must still provide compatible glibc (the release build targets Ubuntu
24.04) and a graphical session. On X11/WSLg, the launcher tests a real OpenGL
context using the host driver first. If that fails, it retries with a private
Mesa llvmpipe software renderer. This affects GUI drawing, not media remuxing.
The fallback includes Mesa's dynamically loaded vendor and DRI libraries and
their dependencies. Hardware GPU drivers and the system ELF loader are not
copied from the build runner. A missing or unreachable display still needs to
be fixed in the desktop/WSLg environment; a renderer cannot supply a display.

`--standalone-self-test` resolves the GUI and every private shared library with
the host ELF loader before exercising the embedded command-line tools. CI also
runs this check in a clean Ubuntu container without GUI packages, then opens
the actual Fyne window with `--graphics-self-test` through an external Xvfb
display. It verifies software fallback, failure with the private renderer stack
removed, and preference for a working host driver. Library source
package names and exact versions are recorded in the extracted
`licenses/library-packages.json`. The build downloads the exact corresponding
source packages into `MattRip-VERSION-Linux-Library-Sources.tar.gz`. Build hosts
need Debian/Ubuntu source repositories (`deb-src`) enabled; CI enables these
before building. This is a build prerequisite, not an end-user requirement.

### Other Linux packages

MattRip always checks the user's existing tools first:

1. Locate `ffmpeg` and `ffprobe` on `PATH`.
2. Verify that FFmpeg exposes the `dvdvideo` demuxer.
3. Use those system tools when the check passes.
4. If FFmpeg is missing or unsuitable, download the pinned BtbN Linux amd64 build and verify its SHA-256 before using it from the user's cache.
5. Use system `mediainfo` when available. MediaInfo is optional and is recommended by the `.deb` package.

Run `mattrip-cli tools` to inspect what MattRip sees on a machine.

## Build from source on Debian / Ubuntu

Fyne requires Go, a C compiler, and the Linux graphics development headers. Install the build prerequisites with:

```bash
sudo apt update
sudo apt install golang-go gcc libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev git dpkg-dev
```

Then build both binaries:

```bash
cd src
go mod download
go build -o mattrip .
go build -tags cli -o mattrip-cli .
```

To create the `.deb`, portable binary tarball, source tarball, and checksums from the repository root:

```bash
bash packaging/linux/build-linux-release.sh 1.3.0-dev1
```

Install the generated `.deb` with `apt` so recommended distro tools are installed automatically when available:

```bash
sudo apt install ./dist/linux-release/mattrip_1.3.0~dev1_amd64.deb
```

MattRip does not bypass DVD copy protection such as CSS.
