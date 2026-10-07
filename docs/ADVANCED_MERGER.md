# Advanced Merger

Advanced Merger combines selected video, audio, subtitle, attachment/data, and optional chapter data into a new Matroska (`.mkv`) file without transcoding.

The feature is available from the **ADVANCED** tab. It is separate from the **REMUX/DEMUX** workflow.

## Inputs

- **MEDIA(All streams included)** accepts FFmpeg-supported containers and elementary media files plus DVD VIDEO_TS folders and DVD ISO images. Every probed stream in the selected source is added to **Select Streams**: video, audio, subtitle, attachment/data/other streams, plus one selectable embedded chapter-set row when the source contains chapters. DVD folder/ISO inputs select the longest readable DVD title through the shared DVD source engine.
- **DVD DRIVE** (Windows/Linux) discovers a physical optical drive through the desktop DVDSource layer, scans it with FFmpeg `dvdvideo`/libdvdread/libdvdnav, chooses the longest readable title, and exposes that title's streams and chapters.
- **ADD AUDIO(Only audio streams will be included)** accepts containers and elementary audio files understood by FFmpeg. Only audio streams are added to **Select Streams**.
- **ADD SUBTITLE(Only subtitle streams will be Included)** accepts containers and subtitle files understood by FFmpeg. Only subtitle streams are added to **Select Streams**.
- **ADD CHAPTER .txt FILE(FFMETADATA1 Format)** accepts one chapter override source. A valid `FFMETADATA1` text file is the primary format shown by the UI; existing MKV chapter-override input remains accepted for compatibility.

MEDIA intentionally differs from the filtered Audio and Subtitle buttons. For example, adding an MKV through **MEDIA(All streams included)** can expose its video, audio, subtitle, attachment/data streams and embedded chapter set at the same time. Adding that same MKV through **ADD AUDIO(Only audio streams will be included)** exposes only its audio tracks. On Windows, MEDIA asks whether to open media files/DVD ISO or a DVD/VIDEO_TS folder. On Linux, the MEDIA browser can add the currently browsed DVD/VIDEO_TS folder directly. On Android/ChromeOS, MEDIA offers the file picker for media/DVD ISO or the SAF tree picker for a DVD/VIDEO_TS folder.

Raw/elementary inputs are supported when the bundled FFmpeg runtime can demux them, including common H.264, MPEG-2/VOB, AAC, AC-3, MP3, DTS, SRT, WebVTT, SUP and similar formats.

For VobSub on Android/ChromeOS, select the matching `.idx` and `.sub` files together so MattRip can stage the sidecar pair before probing.

## Stream and chapter selection

Every discovered stream or chapter set is initially selected. Clear any checkbox you do not want in the output.

Embedded chapters are represented as chapter-set checkboxes because chapters are container metadata rather than packet streams. When no dedicated chapter override is chosen, select at most one movie chapter set. If more than one movie chapter set is selected, MattRip asks you to choose only one.

A chapter source chosen with **ADD CHAPTER .txt FILE(FFMETADATA1 Format)** overrides selected embedded movie chapters. The dedicated picker intentionally accepts MKV or `FFMETADATA1`; movie inputs themselves may carry embedded chapters in other FFmpeg-supported containers such as MP4.

At least one non-chapter stream must remain selected before muxing.

## Output and metadata behavior

Choose the output folder, enter an `.mkv` filename, and press **MUX**.

MattRip maps the exact selected input stream indexes and uses stream copy (`-c copy` on desktop, equivalent native libav packet copying on Android/ChromeOS). It does not intentionally re-encode video or audio. On Windows and Linux, each DVD input (VIDEO_TS, ISO, or physical drive) is opened with `-analyzeduration 100M -probesize 100M -fflags +genpts`. In mixed jobs those 100M probe overrides are scoped only to DVD inputs. Ordinary MKV/MP4/raw inputs keep their existing media-input handling instead of inheriting the DVD-specific 100M probe limits.

Desktop muxing preserves global metadata from the first media input and explicitly copies metadata for each selected stream. This includes stream language/title metadata and attachment filenames where present. Chapter titles are preserved when chapters are copied from either an embedded movie chapter set or the dedicated chapter source.

Android/ChromeOS performs the equivalent selection through the native merger path, including attachment/data streams, container metadata, embedded chapters, and MKV/FFMETADATA1 chapter overrides.

Existing output files are not overwritten. Cancel stops the active probe/copy/mux operation and partial operation-owned output is not promoted to the requested final filename.

## Android / ChromeOS temporary storage

Android's Storage Access Framework does not guarantee a normal seekable filesystem path for every selected document. Ordinary media inputs are copied to private temporary storage. DVD folder/ISO inputs are scanned directly for streams and chapters; adding a DVD does not create an intermediate MKV.

**DEMUX** reads the original DVD title through the existing native DVD reader. Only the extracted outputs need temporary space before they are copied to the chosen destination. **MUX** prepares a temporary MKV for each selected DVD source when the operation starts, including a DVD selected only for chapters. Original DVD stream selections are mapped to the staged MKV indexes before merging. Windows/Linux already read DVD sources directly for both operations and need no preparation change.

For MUX, the device needs enough free temporary space for the selected input copies, any staged DVD titles, and the in-progress output. Temporary merger data is cleaned up when the panel is destroyed or an operation completes. The tab container also applies current status- and navigation-bar insets so the DVD and Advanced Merger interfaces remain clear of system UI across phones, navigation modes, rotation, and resizable ChromeOS windows.

## Example mapping

A selection equivalent to this FFmpeg command can be built through the UI:

```text
ffmpeg -i input2.mkv -i input.mp4 -i input.mkv -i input.ac3 -i input.srt \
  -map 2:v -map 1:a -map 3:a -map 2:s:0 -map 4:s \
  -map_chapters 0 -c copy output.mkv
```

MattRip constructs its maps from the individual stream checkboxes rather than mapping every stream of a category automatically. The chapter source is resolved separately from packet-stream maps so embedded chapters or a dedicated chapter override can be selected without treating chapters as ordinary streams.
