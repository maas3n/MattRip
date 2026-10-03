# Alpha 4 source and remux checks

`test-udf-source.sh <pinned-libudfread-source>` compares complete file bytes and
unaligned/backward reads against generated originals under ASan/UBSan. It checks
fd duplication, missing files, input bounds, cancellation and non-seekable input.
The same originals are wrapped in UDF 2.60 (pycdlib) and UDF 1.02 (genisoimage).
These are filesystem fixtures, not DVD-Video authoring/IFO fixtures.

`test-host-remux.sh <pinned-libudfread-source> <diagnostic-directory>` compiles the
**production JNI source** on Linux with the host libav libraries and invokes it
from Java. An original two-second PAL MPEG-2/AC-3 stream is split across two VOBs
and read through both backends. Fingerprints compare compressed packet hashes,
stream properties, common-clock PTS/DTS and chapters. The harness supplies cell
and chapter arrays directly; it does not claim to test real IFO navigation.
The host-only test library is never packaged in the Android APK.

The same harness also runs a second PAL fixture with two B-frames and sparse
video PES timestamps. It retains the initial clock anchor and replaces later
optional PTS/DTS fields with stuffing without modifying compressed payloads.
The fixture must expose missing video PTS to ordinary demuxing. Both folder and
ISO remuxes must succeed, including video-only selection; packet hashes and
presentation timestamps are checked against the fully timestamped original.
The timing tolerance is 1 ms for Matroska clock quantization, using one shared
A/V origin. This covers missing PTS, not DVD cell clock discontinuities.

Android DVD title discovery and title/cell/chapter planning are provided by the bundled libdvdnav/libdvdread path. Kotlin does not parse DVD IFO structures. The app logs the selected library-derived plan under `MattRipPlan`; successful `RemuxResult` also carries it as `planJson`.

## Remaining equivalence gates

These tests do not establish Linux/Windows release equivalence. Before claiming
that, add independently authored DVD-Video/UDF 1.02 fixtures and golden plans for
multiple titles, shared PGCs, fragmented files, multiple languages/subtitles and
cell clock discontinuities. Run actual Android JNI on both ABIs, compare against
the desktop dvdvideo engines, and test real SAF providers on Chromebooks.

Use a common timestamp origin across all streams, so an A/V offset cannot be
hidden by per-stream normalization. MPEG B-frame PTS may legitimately be
non-monotonic in demux order; check DTS and presentation order appropriately.
Only introduce measured, documented cross-muxer tolerances. Do not weaken the
exact folder/ISO test to accommodate a mismatch.

Interleaved multi-angle input currently fails closed: IFO cell spans alone do
not select VOBUs safely. No full parity or production-readiness claim is made.
