# Android DVD clock reset and demux progress investigation

The reported device error was a Matroska write failure while submitting MPEG2
video with PTS 125, DTS 5, and time base 1/1000. Those values alone do not identify
the rejected packet: the interleaver can fail while flushing an earlier packet.
The original DVD has not been supplied. Mathias subsequently confirmed that
remux, batch mux, Advanced Merger and CLI mux all work in v1.4.17. Treat that
as the user-confirmed working baseline; the repeated-cell fixture below is a
separate defect and does not establish the cause of his v1.4.18 regression.

An independently authored DVD repeats a VOB in one title. The production
libdvdnav/libdvdread title planner returns both playback spans. The old JNI
remuxer concatenates the MPEG program streams without handling their clock
reset, and Matroska rejects non-monotonic DTS. Both v1.4.17 and v1.4.18's remux
code fail this reproduction. This establishes a defect, not proof that the
reported device failure was introduced by v1.4.18.

## Change

- Read NAV PCI/DSI with libdvdread and identify continuous clock segments.
  No MattRip IFO parser is introduced.
- Probe the complete title for track discovery, then isolate MPEG parser and
  GENPTS lookahead at clock boundaries. Preserve one common origin and apply
  NAV-derived offsets equally to video, audio, and subtitles. Do not clamp each
  stream independently or discard packets to hide timestamp errors.
- Keep stream identity across segments using DVD stream IDs and codec IDs.
- Report processed packet positions, rather than the read-ahead/probe cursor.
- Forward staging progress to the DVD tab and report native extraction progress.
  MKV staging reports percentage or copied MiB when its size is unknown.

## Regression coverage

The authored fixtures include continuous chapters and a repeated clock domain,
MPEG2 B-frames, AC3, DVD subtitles, and chapters. The production JNI tests cover
folder and UDF ISO input, elementary and VOB exports, exact packet payloads,
presentation timing, and intermediate extraction progress. NAV duration includes
the encoder's audio tail, so the repeated clock interval can be slightly longer
than the nominal six seconds. Tests require the same offset for every stream.
Android instrumentation additionally checks intermediate staging/extraction
statuses through TabMediaEngine and SAF. Existing sparse-PTS/B-frame,
folder/ISO parity, track-selection, and cancellation tests remain in place.

Windows and Linux already use FFmpeg's dvdvideo demuxer, which resets its MPEG
subdemuxer at NAV discontinuities and applies a common offset. The local FFmpeg
9.0.1 dvdvideo remux of the repeated-cell fixture produces the same interval.
Their media implementation therefore does not need the Android source adapter.
The PR's Windows and Linux workflows run the shared desktop behavior suites.

No public release is part of this change. Validation on the user's failing DVD
is still required before claiming that exact case is resolved.

## Input-policy audit and correction (October 1)

Mathias requires the full analyzeduration=100000000 microseconds (100 seconds),
probesize=100000000 bytes, and GENPTS combination for original DVD inputs,
including DVD audio selected alongside MKV video/subtitles in Advanced Merger.
The 100M probe overrides must stay with the DVD input.

I incorrectly interpreted the original instruction as requiring removal of
existing GENPTS handling from all ordinary inputs. Mathias explicitly corrected
that interpretation: "I DID NOT TELL YOU TO REMOVE GENPTS". That removal is
reverted. Existing GENPTS remains in the desktop ordinary-media helper and the
Android ordinary-media reader. Only the large probe overrides are DVD-specific.
The regression tests continue to cover both orders of mixed DVD/MKV inputs.

The mistaken removal caused the existing desktop TestMergerRawVideo/m2v and
/vob tests to fail with "Can't write packet with unknown timestamp". This was
also reproduced directly with FFmpeg 9.0.1; GENPTS alone restores those cases.
The tests were not weakened or removed.

In v1.4.17 Android demux stages the chosen DVD title into MKV using nativeRemux,
then extracts the chosen tracks from that MKV and copies the exports to SAF.
The DVD stage already had both 100M limits and GENPTS in v1.4.17 and v1.4.18.
Those options were not removed between these releases. The production native
code difference was the DVD subtitle canvas metadata addition; the demux export
reader itself did not change between those two tags. This comparison does not
yet identify the original-device failure.

FFmpeg's concat-demuxer safe=0 accepts filenames rejected by safe=1. It does not
repair timestamps or suppress corruption errors; neither dvdvideo nor the
concat: protocol uses that private option. GENPTS fills missing presentation
timestamps where decoding timestamps are available; it does not promise to
repair all existing timestamp discontinuities.
Source: https://ffmpeg.org/ffmpeg-formats.html (Format Options, concat, dvdvideo).


## Direct DVD demux (October 2)

DVD-tab demux now passes the libdvdnav/libdvdread title plan directly to a
native DVD reader and the elementary/VOB output writers. It no longer calls
remuxTitleToFile, creates source.mkv, or requires a successful Matroska write.
The reader uses all three DVD input settings during discovery and each clock
segment: 100M analyzeduration, 100M probesize, and GENPTS. A common timestamp
origin/offset preserves relative A/V/subtitle timing across clock resets.
Original DVD stream indexes are used directly for selection and filenames.
IFO language/palette, subtitle canvas, and selected-title chapters are retained.

Android still temporarily stores the exported files for SAF copying and the
small IFO metadata needed by libdvdnav. MKV demux retains its input copy.
Windows and Linux already demux directly through dvdvideo with all three DVD
options; their extraction implementation does not need this Android adapter.
Advanced Merger's internal DVD-to-MKV staging remains separate from Demux.

Native tests exercise folder/ISO, elementary/video-only VOB, selected audio,
chapters disabled, cancellation, and repeated DVD clocks. Payloads and subtitle
timing are compared against independently remuxed reference files. The Android
SAF instrumentation additionally checks extraction progress and absence of a
source.mkv cache file. This removes the Matroska prerequisite from DVD demux;
it does not establish the cause of the separately reported remux failure.

MKV demux remains supported through the same button. Its dedicated input reader
uses the container timestamps without explicitly adding GENPTS or either 100M
probe override, on Android and desktop. Existing raw-media merger GENPTS and
all DVD GENPTS handling remain intact. Android SAF tests select video/audio/SRT
and chapters from an MKV through TabMediaEngine as well as testing DVD sources.
