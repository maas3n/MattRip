#include <jni.h>
#include <errno.h>
#include <limits.h>
#include "udf_source.h"
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/avutil.h>
#include <libavutil/dict.h>
#include <libavutil/error.h>
#include <libavutil/mathematics.h>
#include <libavutil/channel_layout.h>
#include <libavutil/mem.h>
#ifdef MATTMUX_DVDNAV
#include <dvdnav/dvdnav.h>
#include <dvdread/dvd_reader.h>
#include <dvdread/ifo_read.h>
#include <dvdread/ifo_types.h>
#include <dvdread/nav_read.h>
#include <android/log.h>
#endif
#include <libavutil/timestamp.h>
#ifdef MATTMUX_DVDCSS
#include <dvdcss/dvdcss.h>
#endif

#define IO_BUFFER_SIZE (64 * 1024)
#define DVD_SECTOR_SIZE 2048LL

typedef struct { JNIEnv *env; jobject engine; jmethodID method; } CancelContext;

static int is_cancelled(void *opaque)
{
    CancelContext *ctx = opaque;
    if (!ctx || !ctx->method) return 0;
    jboolean result = (*ctx->env)->CallBooleanMethod(ctx->env, ctx->engine, ctx->method);
    return (*ctx->env)->ExceptionCheck(ctx->env) || result;
}

typedef struct {
    int64_t source_start;
    int64_t length;
    int64_t stream_start;
} SourceSpan;

typedef struct {
    CancelContext *cancel;
    DvdUdfSource *udf; /* borrowed for this JNI call */
    UDFFILE *udf_files[9];
    int *fds;
    int fd_count;
    int64_t *file_starts;
    int64_t total_source_size;
    SourceSpan *spans;
    int span_count;
    int64_t stream_size;
    int64_t window_start, window_end; /* one continuous DVD clock domain */
    int64_t pos;
#ifdef MATTMUX_DVDCSS
    dvdcss_t dvdcss;
    dvdcss_stream_cb dvdcss_callbacks;
    int64_t dvdcss_stream_pos;
    int64_t cached_source_sector;
    unsigned char cached_sector[DVDCSS_BLOCK_SIZE];
    int dvdcss_key_ready;
#endif
} SourceContext;

typedef struct {
    int fd;
    CancelContext *cancel;
    int64_t pos;
} OutputContext;

static void format_version(char *out, size_t out_size, unsigned version)
{
    snprintf(out, out_size, "%u.%u.%u",
        (version >> 16) & 0xff,
        (version >> 8) & 0xff,
        version & 0xff);
}

static void ff_error(char *out, size_t out_size, const char *step, int err)
{
    char detail[AV_ERROR_MAX_STRING_SIZE] = {0};
    av_strerror(err, detail, sizeof(detail));
    snprintf(out, out_size, "%s: %s", step, detail);
}

static int find_span(const SourceContext *ctx, int64_t stream_pos)
{
    for (int i = 0; i < ctx->span_count; ++i) {
        const int64_t start = ctx->spans[i].stream_start;
        const int64_t end = start + ctx->spans[i].length;
        if (stream_pos >= start && stream_pos < end) return i;
    }
    return -1;
}

static int find_file(const SourceContext *ctx, int64_t source_pos)
{
    for (int i = 0; i < ctx->fd_count; ++i) {
        const int64_t start = ctx->file_starts[i];
        const int64_t end = (i + 1 < ctx->fd_count) ? ctx->file_starts[i + 1] : ctx->total_source_size;
        if (source_pos >= start && source_pos < end) return i;
    }
    return -1;
}

static ssize_t source_raw_read_at(SourceContext *ctx, int64_t source_pos, uint8_t *buf, size_t wanted)
{
    if (!ctx || source_pos < 0 || source_pos > ctx->total_source_size) return -1;
    size_t done = 0;
    while (done < wanted && source_pos < ctx->total_source_size) {
        if (is_cancelled(ctx->cancel)) return -1;
        int file_index = find_file(ctx, source_pos);
        if (file_index < 0) return -1;
        int64_t file_start = ctx->file_starts[file_index];
        int64_t file_end = (file_index + 1 < ctx->fd_count) ? ctx->file_starts[file_index + 1] : ctx->total_source_size;
        int64_t available = file_end - source_pos;
        size_t chunk = wanted - done;
        if ((int64_t)chunk > available) chunk = (size_t)available;
        if (!chunk) break;

        ssize_t n;
        if (ctx->udf) {
            UDFFILE *file = ctx->udf_files[file_index];
            int64_t offset = source_pos - file_start;
            if (udfread_file_seek(file, offset, SEEK_SET) != offset) return -1;
            n = udfread_file_read(file, buf + done, chunk);
        } else {
            do {
                n = pread(ctx->fds[file_index], buf + done, chunk, (off_t)(source_pos - file_start));
            } while (n < 0 && errno == EINTR && !is_cancelled(ctx->cancel));
        }
        if (n <= 0) return done ? (ssize_t)done : -1;
        done += (size_t)n;
        source_pos += n;
    }
    return (ssize_t)done;
}

#ifdef MATTMUX_DVDCSS
static int mattmux_dvdcss_stream_seek(void *opaque, uint64_t block)
{
    SourceContext *ctx = (SourceContext *)opaque;
    if (!ctx || block > INT_MAX ||
        block > (uint64_t)(ctx->total_source_size / DVDCSS_BLOCK_SIZE)) return -1;
    ctx->dvdcss_stream_pos = (int64_t)block * DVDCSS_BLOCK_SIZE;
    return (int)block;
}

static int mattmux_dvdcss_stream_read(void *opaque, void *buffer, int blocks)
{
    SourceContext *ctx = (SourceContext *)opaque;
    if (!ctx || !buffer || blocks < 0 ||
        blocks > INT_MAX / DVDCSS_BLOCK_SIZE) return -1;
    size_t wanted = (size_t)blocks * DVDCSS_BLOCK_SIZE;
    ssize_t n = source_raw_read_at(ctx, ctx->dvdcss_stream_pos, buffer, wanted);
    if (n < 0 || n % DVDCSS_BLOCK_SIZE != 0) return -1;
    ctx->dvdcss_stream_pos += n;
    return (int)(n / DVDCSS_BLOCK_SIZE);
}

static int source_sector_is_scrambled(const unsigned char *sector)
{
    return sector[0] == 0x00 && sector[1] == 0x00 && sector[2] == 0x01 &&
           sector[3] == 0xba && (sector[0x14] & 0x30) != 0;
}

static int source_open_dvdcss(SourceContext *ctx)
{
    if (ctx->dvdcss) return 0;
    ctx->dvdcss_callbacks.pf_seek = mattmux_dvdcss_stream_seek;
    ctx->dvdcss_callbacks.pf_read = mattmux_dvdcss_stream_read;
    ctx->dvdcss_callbacks.pf_readv = NULL;
    ctx->dvdcss_stream_pos = 0;
    ctx->dvdcss = dvdcss_open_stream(ctx, &ctx->dvdcss_callbacks);
    return ctx->dvdcss ? 0 : AVERROR(EIO);
}

static int source_load_sector(SourceContext *ctx, int64_t sector)
{
    if (sector < 0 || sector > INT_MAX ||
        sector > ctx->total_source_size / DVDCSS_BLOCK_SIZE - 1) return AVERROR(EIO);
    if (ctx->cached_source_sector == sector) return 0;

    int64_t byte_pos = sector * (int64_t)DVDCSS_BLOCK_SIZE;
    if (source_raw_read_at(ctx, byte_pos, ctx->cached_sector, DVDCSS_BLOCK_SIZE) != DVDCSS_BLOCK_SIZE)
        return is_cancelled(ctx->cancel) ? AVERROR_EXIT : AVERROR(EIO);

    if (source_sector_is_scrambled(ctx->cached_sector)) {
        int ret = source_open_dvdcss(ctx);
        if (ret < 0) return ret;
        int seek_flags = ctx->dvdcss_key_ready ? DVDCSS_NOFLAGS : DVDCSS_SEEK_KEY;
        if (dvdcss_seek(ctx->dvdcss, (int)sector, seek_flags) < 0) return AVERROR(EIO);
        ctx->dvdcss_key_ready = 1;
        if (dvdcss_read(ctx->dvdcss, ctx->cached_sector, 1, DVDCSS_READ_DECRYPT) != 1)
            return AVERROR(EIO);
    }

    ctx->cached_source_sector = sector;
    return 0;
}

static int source_read_css_aware(SourceContext *ctx, int64_t source_pos, uint8_t *buf, int wanted)
{
    int done = 0;
    while (done < wanted) {
        int64_t sector = source_pos / DVDCSS_BLOCK_SIZE;
        int offset = (int)(source_pos % DVDCSS_BLOCK_SIZE);
        int ret = source_load_sector(ctx, sector);
        if (ret < 0) return done ? done : ret;
        int chunk = DVDCSS_BLOCK_SIZE - offset;
        if (chunk > wanted - done) chunk = wanted - done;
        memcpy(buf + done, ctx->cached_sector + offset, (size_t)chunk);
        source_pos += chunk;
        done += chunk;
    }
    return done;
}
#endif

static int source_read(void *opaque, uint8_t *buf, int buf_size)
{
    SourceContext *ctx = (SourceContext *)opaque;
    if (is_cancelled(ctx->cancel)) return AVERROR_EXIT;
    if (ctx->pos >= ctx->window_end) return AVERROR_EOF;

    int span_index = find_span(ctx, ctx->pos);
    if (span_index < 0) return AVERROR(EIO);
    SourceSpan *span = &ctx->spans[span_index];
    int64_t in_span = ctx->pos - span->stream_start;
    int64_t source_pos = span->source_start + in_span;
    int64_t remaining_span = span->length - in_span;
    int64_t wanted = buf_size;
    if (wanted > ctx->window_end - ctx->pos) wanted = ctx->window_end - ctx->pos;
    if (wanted > remaining_span) wanted = remaining_span;
    if (wanted <= 0 || wanted > INT_MAX) return AVERROR(EIO);

    int n;
#ifdef MATTMUX_DVDCSS
    n = source_read_css_aware(ctx, source_pos, buf, (int)wanted);
#else
    ssize_t raw = source_raw_read_at(ctx, source_pos, buf, (size_t)wanted);
    n = raw < 0 || raw > INT_MAX ? -1 : (int)raw;
#endif
    if (n <= 0) return is_cancelled(ctx->cancel) ? AVERROR_EXIT : AVERROR(EIO);
    ctx->pos += n;
    return n;
}

static int64_t source_seek(void *opaque, int64_t offset, int whence)
{
    SourceContext *ctx = (SourceContext *)opaque;
    if (whence == AVSEEK_SIZE) return ctx->window_end - ctx->window_start;
    int base_whence = whence & ~AVSEEK_FORCE;
    int64_t next;
    switch (base_whence) {
        case SEEK_SET: next = ctx->window_start + offset; break;
        case SEEK_CUR: next = ctx->pos + offset; break;
        case SEEK_END: next = ctx->window_end + offset; break;
        default: return AVERROR(EINVAL);
    }
    if (next < ctx->window_start || next > ctx->window_end) return AVERROR(EINVAL);
    ctx->pos = next;
    return next - ctx->window_start;
}

#if LIBAVFORMAT_VERSION_MAJOR >= 61
static int output_write(void *opaque, const uint8_t *buf, int buf_size)
#else
static int output_write(void *opaque, uint8_t *buf, int buf_size)
#endif
{
    OutputContext *ctx = (OutputContext *)opaque;
    if (is_cancelled(ctx->cancel)) return AVERROR_EXIT;
    int written = 0;
    while (written < buf_size) {
        ssize_t n = pwrite(ctx->fd, buf + written, (size_t)(buf_size - written), (off_t)(ctx->pos + written));
        if (n < 0 && errno == EINTR) continue;
        if (n < 0) return AVERROR(errno);
        if (n == 0) return AVERROR(EIO);
        written += (int)n;
    }
    ctx->pos += written;
    return written;
}

static int64_t output_seek(void *opaque, int64_t offset, int whence)
{
    OutputContext *ctx = (OutputContext *)opaque;
    if (whence == AVSEEK_SIZE) {
        struct stat st;
        return fstat(ctx->fd, &st) == 0 ? st.st_size : AVERROR(errno);
    }
    int base_whence = whence & ~AVSEEK_FORCE;
    int64_t next;
    switch (base_whence) {
        case SEEK_SET: next = offset; break;
        case SEEK_CUR: next = ctx->pos + offset; break;
        case SEEK_END: {
            struct stat st;
            if (fstat(ctx->fd, &st) != 0) return AVERROR(errno);
            next = st.st_size + offset;
            break;
        }
        default: return AVERROR(EINVAL);
    }
    if (next < 0) return AVERROR(EINVAL);
    ctx->pos = next;
    return next;
}

static int init_source(JNIEnv *env, jintArray fd_array, jlongArray starts_array, jlongArray ends_array,
                       SourceContext *ctx, DvdUdfSource *udf, int title_set, CancelContext *cancel, char *error, size_t error_size)
{
    memset(ctx, 0, sizeof(*ctx));
#ifdef MATTMUX_DVDCSS
    ctx->cached_source_sector = -1;
#endif
    ctx->cancel = cancel;
    ctx->udf = udf;
    jsize fd_count = (*env)->GetArrayLength(env, fd_array);
    if (udf) {
        udf->cancelled = is_cancelled;
        udf->cancel_opaque = cancel;
        fd_count = 0;
        int gap = 0;
        for (int part = 1; part <= 9; ++part) {
            UDFFILE *file = dvd_udf_file(udf, title_set, part, 0);
            if (!file) { gap = 1; continue; }
            if (gap) {
                udfread_file_close(file);
                snprintf(error, error_size, "ISO title has a missing VOB part");
                return AVERROR_INVALIDDATA;
            }
            ctx->udf_files[fd_count++] = file;
        }
    }
    jsize span_count = (*env)->GetArrayLength(env, starts_array);
    if (fd_count <= 0 || span_count <= 0 || (*env)->GetArrayLength(env, ends_array) != span_count) {
        snprintf(error, error_size, "Invalid native DVD input arrays");
        return AVERROR(EINVAL);
    }

    ctx->fds = av_malloc_array((size_t)fd_count, sizeof(*ctx->fds));
    ctx->file_starts = av_malloc_array((size_t)fd_count, sizeof(*ctx->file_starts));
    ctx->spans = av_malloc_array((size_t)span_count, sizeof(*ctx->spans));
    if (!ctx->fds || !ctx->file_starts || !ctx->spans) return AVERROR(ENOMEM);
    ctx->fd_count = fd_count;
    ctx->span_count = span_count;

    jint *fds = (*env)->GetIntArrayElements(env, fd_array, NULL);
    jlong *starts = (*env)->GetLongArrayElements(env, starts_array, NULL);
    jlong *ends = (*env)->GetLongArrayElements(env, ends_array, NULL);
    if ((!fds && !udf) || !starts || !ends) {
        if (fds) (*env)->ReleaseIntArrayElements(env, fd_array, fds, JNI_ABORT);
        if (starts) (*env)->ReleaseLongArrayElements(env, starts_array, starts, JNI_ABORT);
        if (ends) (*env)->ReleaseLongArrayElements(env, ends_array, ends, JNI_ABORT);
        return AVERROR(ENOMEM);
    }

    int ret = 0;
    int64_t source_total = 0;
    for (int i = 0; i < fd_count; ++i) {
        struct stat st;
        ctx->fds[i] = udf ? -1 : fds[i];
        ctx->file_starts[i] = source_total;
        int64_t size = udf ? udfread_file_size(ctx->udf_files[i]) :
            (fstat(fds[i], &st) == 0 ? st.st_size : -1);
        unsigned char probe;
        if (size <= 0 || size % DVD_SECTOR_SIZE || size > INT64_MAX - source_total ||
            (!udf && pread(fds[i], &probe, 1, 0) != 1)) {
            snprintf(error, error_size, "DVD VOB descriptor %d is not seekable", i + 1);
            ret = AVERROR(EIO);
            goto done;
        }
        source_total += size;
    }
    ctx->total_source_size = source_total;

    int64_t stream_total = 0;
    for (int i = 0; i < span_count; ++i) {
        if (starts[i] < 0 || ends[i] <= starts[i] || ends[i] > source_total / DVD_SECTOR_SIZE) {
            snprintf(error, error_size, "DVD cell %d points outside title VOB data", i + 1);
            ret = AVERROR_INVALIDDATA;
            goto done;
        }
        int64_t start = starts[i] * DVD_SECTOR_SIZE;
        int64_t end = ends[i] * DVD_SECTOR_SIZE;
        if (start < 0 || end <= start || end > source_total || end - start > INT64_MAX - stream_total) {
            snprintf(error, error_size, "DVD cell %d points outside title VOB data", i + 1);
            ret = AVERROR_INVALIDDATA;
            goto done;
        }
        ctx->spans[i].source_start = start;
        ctx->spans[i].length = end - start;
        ctx->spans[i].stream_start = stream_total;
        stream_total += end - start;
    }
    ctx->stream_size = stream_total;
    ctx->window_end = stream_total;

done:
    if (fds) (*env)->ReleaseIntArrayElements(env, fd_array, fds, JNI_ABORT);
    (*env)->ReleaseLongArrayElements(env, starts_array, starts, JNI_ABORT);
    (*env)->ReleaseLongArrayElements(env, ends_array, ends, JNI_ABORT);
    return ret;
}

static void free_source(SourceContext *ctx)
{
#ifdef MATTMUX_DVDCSS
    if (ctx->dvdcss) {
        dvdcss_close(ctx->dvdcss);
        ctx->dvdcss = NULL;
    }
#endif
    for (int i = 0; i < 9; ++i) if (ctx->udf_files[i]) udfread_file_close(ctx->udf_files[i]);
    if (ctx->udf) { ctx->udf->cancelled = NULL; ctx->udf->cancel_opaque = NULL; }
    av_freep(&ctx->fds);
    av_freep(&ctx->file_starts);
    av_freep(&ctx->spans);
}

static int add_chapters(JNIEnv *env, AVFormatContext *out, jlongArray starts_array, jlongArray ends_array)
{
    jsize count = (*env)->GetArrayLength(env, starts_array);
    if ((*env)->GetArrayLength(env, ends_array) != count) return AVERROR(EINVAL);
    if (count == 0) return 0;
    jlong *starts = (*env)->GetLongArrayElements(env, starts_array, NULL);
    jlong *ends = (*env)->GetLongArrayElements(env, ends_array, NULL);
    if (!starts || !ends) {
        if (starts) (*env)->ReleaseLongArrayElements(env, starts_array, starts, JNI_ABORT);
        if (ends) (*env)->ReleaseLongArrayElements(env, ends_array, ends, JNI_ABORT);
        return AVERROR(ENOMEM);
    }

    AVChapter **chapters = av_calloc((size_t)count, sizeof(*chapters));
    if (!chapters) {
        (*env)->ReleaseLongArrayElements(env, starts_array, starts, JNI_ABORT);
        (*env)->ReleaseLongArrayElements(env, ends_array, ends, JNI_ABORT);
        return AVERROR(ENOMEM);
    }
    int ret = 0;
    for (int i = 0; i < count; ++i) {
        if (starts[i] < 0 || ends[i] <= starts[i]) { ret = AVERROR(EINVAL); break; }
        chapters[i] = av_mallocz(sizeof(*chapters[i]));
        if (!chapters[i]) { ret = AVERROR(ENOMEM); break; }
        chapters[i]->id = i;
        chapters[i]->time_base = (AVRational){1, 1000};
        chapters[i]->start = starts[i];
        chapters[i]->end = ends[i];
        char title[32];
        snprintf(title, sizeof(title), "Chapter %02d", i + 1);
        av_dict_set(&chapters[i]->metadata, "title", title, 0);
    }
    if (ret >= 0) {
        out->chapters = chapters;
        out->nb_chapters = count;
        chapters = NULL;
    }
    if (chapters) {
        for (int i = 0; i < count; ++i) {
            if (chapters[i]) {
                av_dict_free(&chapters[i]->metadata);
                av_free(chapters[i]);
            }
        }
        av_free(chapters);
    }
    (*env)->ReleaseLongArrayElements(env, starts_array, starts, JNI_ABORT);
    (*env)->ReleaseLongArrayElements(env, ends_array, ends, JNI_ABORT);
    return ret;
}

static void report_progress(JNIEnv *env, jobject thiz, jmethodID method, int percent)
{
    if (!method) return;
    (*env)->CallVoidMethod(env, thiz, method, percent);
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
}

JNIEXPORT jstring JNICALL
Java_io_github_maas3n_mattmux_AndroidNativeRemuxEngine_nativeVersionSummary(JNIEnv *env, jobject thiz)
{
    (void)thiz;
    char avformat[32], avcodec[32], avutil[32], summary[192];
    format_version(avformat, sizeof(avformat), avformat_version());
    format_version(avcodec, sizeof(avcodec), avcodec_version());
    format_version(avutil, sizeof(avutil), avutil_version());
    snprintf(summary, sizeof(summary), "FFmpeg %s (libavformat %s, libavcodec %s, libavutil %s)",
        av_version_info(), avformat, avcodec, avutil);
    return (*env)->NewStringUTF(env, summary);
}

static void throw_io(JNIEnv *env, const char *message)
{
    jclass cls = (*env)->FindClass(env, "java/io/IOException");
    if (cls) (*env)->ThrowNew(env, cls, message);
}

JNIEXPORT jlong JNICALL
Java_io_github_maas3n_mattmux_AndroidNativeRemuxEngine_nativeOpenIso(JNIEnv *env, jobject thiz, jint fd)
{
    (void)thiz;
    char error[256] = "Could not allocate UDF reader";
    DvdUdfSource *source = dvd_udf_open(fd, error, sizeof(error));
    if (!source) throw_io(env, error);
    return (jlong)(intptr_t)source;
}

JNIEXPORT void JNICALL
Java_io_github_maas3n_mattmux_AndroidNativeRemuxEngine_nativeCloseIso(JNIEnv *env, jobject thiz, jlong handle)
{
    (void)env; (void)thiz;
    dvd_udf_close((DvdUdfSource *)(intptr_t)handle);
}

JNIEXPORT jbyteArray JNICALL
Java_io_github_maas3n_mattmux_AndroidNativeRemuxEngine_nativeReadIsoIfo(JNIEnv *env, jobject thiz, jlong handle, jint title_set)
{
    (void)thiz;
    UDFFILE *file = dvd_udf_file((DvdUdfSource *)(intptr_t)handle, title_set, 0, 1);
    if (!file) return NULL;
    int64_t size = udfread_file_size(file);
    jbyteArray result = NULL;
    uint8_t *bytes = NULL;
    if (size <= 0 || size > 64 * 1024 * 1024) { throw_io(env, "Invalid ISO IFO size"); goto done; }
    bytes = malloc((size_t)size);
    if (!bytes) { throw_io(env, "Could not allocate IFO buffer"); goto done; }
    int64_t done_bytes = 0;
    while (done_bytes < size) {
        ssize_t n = udfread_file_read(file, bytes + done_bytes, (size_t)(size - done_bytes));
        if (n <= 0) { throw_io(env, "Truncated or unreadable IFO in ISO"); goto done; }
        done_bytes += n;
    }
    result = (*env)->NewByteArray(env, (jsize)size);
    if (result) (*env)->SetByteArrayRegion(env, result, 0, (jsize)size, (jbyte *)bytes);
done:
    free(bytes);
    udfread_file_close(file);
    return result;
}

static const char *track_type_name(enum AVMediaType type)
{
    switch (type) {
        case AVMEDIA_TYPE_VIDEO: return "video";
        case AVMEDIA_TYPE_AUDIO: return "audio";
        case AVMEDIA_TYPE_SUBTITLE: return "subtitle";
        default: return "other";
    }
}

static void metadata_field(const AVDictionary *metadata, const char *key, char *out, size_t out_size)
{
    AVDictionaryEntry *entry = av_dict_get(metadata, key, NULL, 0);
    const char *source = (entry && entry->value && entry->value[0]) ? entry->value : "-";
    size_t j = 0;
    for (size_t i = 0; source[i] && j + 1 < out_size; ++i) {
        unsigned char c = (unsigned char)source[i];
        out[j++] = (c == '\t' || c == '\r' || c == '\n') ? ' ' : (char)c;
    }
    out[j] = '\0';
}

static int apply_dvd_ifo_metadata(JNIEnv *env, AVFormatContext *input,
                                  jobjectArray language_records, jintArray palette_array)
{
    if (language_records) {
        jsize count = (*env)->GetArrayLength(env, language_records);
        for (jsize i = 0; i < count; ++i) {
            jstring record = (jstring)(*env)->GetObjectArrayElement(env, language_records, i);
            if (!record) continue;
            const char *value = (*env)->GetStringUTFChars(env, record, NULL);
            if (!value) { (*env)->DeleteLocalRef(env, record); return AVERROR_EXTERNAL; }
            char *end = NULL;
            long stream_id = strtol(value, &end, 10);
            if (end && *end == '\t' && end[1] && stream_id >= 0 && stream_id <= INT_MAX) {
                for (unsigned s = 0; s < input->nb_streams; ++s) {
                    if (input->streams[s]->id == stream_id) {
                        av_dict_set(&input->streams[s]->metadata, "language", end + 1, 0);
                        break;
                    }
                }
            }
            (*env)->ReleaseStringUTFChars(env, record, value);
            (*env)->DeleteLocalRef(env, record);
        }
    }

    if (palette_array && (*env)->GetArrayLength(env, palette_array) == 16) {
        int width = 0, height = 0;
        for (unsigned s = 0; s < input->nb_streams; ++s) {
            AVCodecParameters *par = input->streams[s]->codecpar;
            if (par->codec_type == AVMEDIA_TYPE_VIDEO && par->width > 0 && par->height > 0) {
                width = par->width;
                height = par->height;
                break;
            }
        }
        jint *colors = (*env)->GetIntArrayElements(env, palette_array, NULL);
        if (!colors) return AVERROR(ENOMEM);
        char palette[192];
        size_t used = 0;
        // MPEG-PS does not carry VobSub canvas extradata. Preserve the DVD
        // picture dimensions with its IFO palette when staging into Matroska.
        int n = width > 0 && height > 0
            ? snprintf(palette, sizeof(palette), "size: %dx%d\npalette: ", width, height)
            : snprintf(palette, sizeof(palette), "palette: ");
        if (n < 0 || (size_t)n >= sizeof(palette)) { (*env)->ReleaseIntArrayElements(env, palette_array, colors, JNI_ABORT); return AVERROR(EINVAL); }
        used = (size_t)n;
        for (int i = 0; i < 16; ++i) {
            n = snprintf(palette + used, sizeof(palette) - used, "%06x%s",
                         ((unsigned)colors[i]) & 0xffffffU, i == 15 ? "\n" : ", ");
            if (n < 0 || (size_t)n >= sizeof(palette) - used) { (*env)->ReleaseIntArrayElements(env, palette_array, colors, JNI_ABORT); return AVERROR(EINVAL); }
            used += (size_t)n;
        }
        (*env)->ReleaseIntArrayElements(env, palette_array, colors, JNI_ABORT);

        for (unsigned s = 0; s < input->nb_streams; ++s) {
            AVStream *stream = input->streams[s];
            if (stream->codecpar->codec_id != AV_CODEC_ID_DVD_SUBTITLE) continue;
            uint8_t *extra = av_mallocz(used + AV_INPUT_BUFFER_PADDING_SIZE);
            if (!extra) return AVERROR(ENOMEM);
            memcpy(extra, palette, used);
            av_freep(&stream->codecpar->extradata);
            stream->codecpar->extradata = extra;
            stream->codecpar->extradata_size = (int)used;
        }
    }
    return 0;
}

JNIEXPORT jobjectArray JNICALL
Java_io_github_maas3n_mattmux_AndroidNativeRemuxEngine_nativeProbeTracks(
    JNIEnv *env, jobject thiz, jintArray fd_array, jlongArray starts_array, jlongArray ends_array,
    jlong iso_handle, jint title_set, jobjectArray language_records, jintArray palette_array)
{
    jclass engine_class = (*env)->GetObjectClass(env, thiz);
    CancelContext cancel = {env, thiz, (*env)->GetMethodID(env, engine_class, "isNativeCancelled", "()Z")};
    if (!cancel.method) return NULL;
    char error[512] = {0};
    int ret = 0;
    SourceContext source;
    AVIOContext *input_io = NULL;
    AVFormatContext *input = NULL;
    jobjectArray result = NULL;

    ret = init_source(env, fd_array, starts_array, ends_array, &source,
        (DvdUdfSource *)(intptr_t)iso_handle, title_set, &cancel, error, sizeof(error));
    if (ret < 0) goto cleanup_probe;

    uint8_t *input_buffer = av_malloc(IO_BUFFER_SIZE);
    if (!input_buffer) { ret = AVERROR(ENOMEM); goto cleanup_probe; }
    input_io = avio_alloc_context(input_buffer, IO_BUFFER_SIZE, 0, &source, source_read, NULL, source_seek);
    if (!input_io) { av_free(input_buffer); ret = AVERROR(ENOMEM); goto cleanup_probe; }
    input = avformat_alloc_context();
    if (!input) { ret = AVERROR(ENOMEM); goto cleanup_probe; }
    input->pb = input_io;
    input->probesize = 100000000;
    input->max_analyze_duration = 100000000;
    input->interrupt_callback = (AVIOInterruptCB){is_cancelled, &cancel};
    input->flags |= AVFMT_FLAG_CUSTOM_IO | AVFMT_FLAG_GENPTS;
    ret = avformat_open_input(&input, NULL, NULL, NULL);
    if (ret < 0) { ff_error(error, sizeof(error), "Could not open selected DVD program stream", ret); goto cleanup_probe; }
    ret = avformat_find_stream_info(input, NULL);
    if (ret < 0) { ff_error(error, sizeof(error), "Could not probe DVD streams", ret); goto cleanup_probe; }

    ret = apply_dvd_ifo_metadata(env, input, language_records, palette_array);
    if (ret < 0) { ff_error(error, sizeof(error), "Could not apply DVD IFO metadata", ret); goto cleanup_probe; }

    int count = 0;
    for (unsigned i = 0; i < input->nb_streams; ++i) {
        enum AVMediaType type = input->streams[i]->codecpar->codec_type;
        if (type == AVMEDIA_TYPE_VIDEO || type == AVMEDIA_TYPE_AUDIO || type == AVMEDIA_TYPE_SUBTITLE) count++;
    }
    jclass string_class = (*env)->FindClass(env, "java/lang/String");
    if (!string_class) { ret = AVERROR_EXTERNAL; goto cleanup_probe; }
    result = (*env)->NewObjectArray(env, count, string_class, NULL);
    if (!result) { ret = AVERROR(ENOMEM); goto cleanup_probe; }

    int row = 0;
    for (unsigned i = 0; i < input->nb_streams; ++i) {
        AVStream *stream = input->streams[i];
        AVCodecParameters *par = stream->codecpar;
        enum AVMediaType type = par->codec_type;
        if (type != AVMEDIA_TYPE_VIDEO && type != AVMEDIA_TYPE_AUDIO && type != AVMEDIA_TYPE_SUBTITLE) continue;
        char language[128], title[256], layout[128] = "-", record[1024];
        metadata_field(stream->metadata, "language", language, sizeof(language));
        metadata_field(stream->metadata, "title", title, sizeof(title));
        int channels = 0;
        if (type == AVMEDIA_TYPE_AUDIO) {
            channels = par->ch_layout.nb_channels;
            if (channels > 0 && av_channel_layout_describe(&par->ch_layout, layout, sizeof(layout)) < 0) strcpy(layout, "-");
        }
        snprintf(record, sizeof(record), "%u\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s",
            i, track_type_name(type), avcodec_get_name(par->codec_id), language, title,
            par->width, par->height, channels, layout);
        jstring value = (*env)->NewStringUTF(env, record);
        if (!value) { ret = AVERROR(ENOMEM); goto cleanup_probe; }
        (*env)->SetObjectArrayElement(env, result, row++, value);
        (*env)->DeleteLocalRef(env, value);
        if ((*env)->ExceptionCheck(env)) { ret = AVERROR_EXTERNAL; goto cleanup_probe; }
    }

cleanup_probe:
    if (input) {
        if (input->iformat) avformat_close_input(&input);
        else avformat_free_context(input);
    }
    if (input_io) { av_freep(&input_io->buffer); avio_context_free(&input_io); }
    free_source(&source);
    if (ret < 0) {
        if (!(*env)->ExceptionCheck(env)) {
            if (error[0] == '\0') ff_error(error, sizeof(error), "Native metadata probe failed", ret);
            throw_io(env, error);
        }
        return NULL;
    }
    return result;
}

/* NAV timing is decoded by libdvdread, never by an IFO parser in MattMux.
 * A fresh MPEG demuxer at each discontinuity prevents GENPTS / frame parsers
 * from looking across clock resets. All streams receive the same clock offset.
 */
typedef struct { int64_t start, end, offset; } DvdClockSegment;

static int dvd_clock_segments(SourceContext *source, DvdClockSegment **result, int *count)
{
    *result = av_malloc(sizeof(**result));
    if (!*result) return AVERROR(ENOMEM);
    (*result)[0] = (DvdClockSegment){0, source->stream_size, 0};
    *count = 1;
#ifdef MATTMUX_DVDNAV
    int seen = 0;
    int64_t previous_end = 0, offset = 0;
    for (int span = 0; span < source->span_count; ++span) {
        int64_t position = source->spans[span].stream_start;
        int64_t end = position + source->spans[span].length;
        while (position < end) {
            uint8_t nav[2048];
            source->pos = position;
            int got = 0;
            while (got < (int)sizeof(nav)) {
                int n = source_read(source, nav + got, sizeof(nav) - got);
                if (n < 0) return n;
                got += n;
            }
            /* Raw program streams used by host tests have no authored NAV.
             * Only an entirely non-NAV source retains the legacy single clock. */
            if (memcmp(nav + 38, "\x00\x00\x01\xbf", 4) || nav[44] != 0 ||
                memcmp(nav + 1024, "\x00\x00\x01\xbf", 4) || nav[1030] != 1) {
                if (!seen && !span) { source->pos = 0; return 0; }
                return AVERROR_INVALIDDATA;
            }
            pci_t pci; dsi_t dsi;
            navRead_PCI(&pci, nav + 45);
            navRead_DSI(&dsi, nav + 1031);
            int64_t start_ptm = pci.pci_gi.vobu_s_ptm, end_ptm = pci.pci_gi.vobu_e_ptm;
            if (end_ptm < start_ptm) return AVERROR_INVALIDDATA;
            if (seen && previous_end != start_ptm) {
                offset += previous_end - start_ptm;
                DvdClockSegment *grown = av_realloc_array(*result, *count + 1, sizeof(**result));
                if (!grown) return AVERROR(ENOMEM);
                *result = grown;
                grown[*count - 1].end = position;
                grown[(*count)++] = (DvdClockSegment){position, source->stream_size, offset};
            }
            previous_end = end_ptm;
            seen = 1;
            int64_t next = position + ((int64_t)dsi.dsi_gi.vobu_ea + 1) * DVD_SECTOR_SIZE;
            if (next <= position || next > end) return AVERROR_INVALIDDATA;
            position = next;
        }
    }
#endif
    source->pos = 0;
    return 0;
}

static int open_clock_segment(SourceContext *source, DvdClockSegment segment,
                              AVFormatContext **input, AVIOContext **io)
{
    source->window_start = source->pos = segment.start;
    source->window_end = segment.end;
    uint8_t *buffer = av_malloc(IO_BUFFER_SIZE);
    if (!buffer) return AVERROR(ENOMEM);
    *io = avio_alloc_context(buffer, IO_BUFFER_SIZE, 0, source, source_read, NULL, source_seek);
    if (!*io) { av_free(buffer); return AVERROR(ENOMEM); }
    *input = avformat_alloc_context();
    if (!*input) return AVERROR(ENOMEM);
    (*input)->pb = *io;
    (*input)->probesize = 100000000;
    (*input)->max_analyze_duration = 100000000;
    (*input)->interrupt_callback = (AVIOInterruptCB){is_cancelled, source->cancel};
    (*input)->flags |= AVFMT_FLAG_CUSTOM_IO | AVFMT_FLAG_GENPTS;
    int ret = avformat_open_input(input, NULL, NULL, NULL);
    if (ret >= 0) ret = avformat_find_stream_info(*input, NULL);
    return ret;
}

JNIEXPORT jstring JNICALL
Java_io_github_maas3n_mattmux_AndroidNativeRemuxEngine_nativeRemux(
    JNIEnv *env, jobject thiz, jintArray fd_array, jlongArray starts_array, jlongArray ends_array,
    jint output_fd, jlongArray chapter_starts, jlongArray chapter_ends, jintArray selected_streams, jlong iso_handle, jint title_set,
    jobjectArray language_records, jintArray palette_array)
{
    jclass engine_class = (*env)->GetObjectClass(env, thiz);
    CancelContext cancel = {env, thiz, (*env)->GetMethodID(env, engine_class, "isNativeCancelled", "()Z")};
    if (!cancel.method) return NULL; /* pending JNI exception propagates */
    char error[512] = {0};
    int ret = 0;
    SourceContext source;
    OutputContext output = { .fd = output_fd, .cancel = &cancel, .pos = 0 };
    AVIOContext *input_io = NULL, *output_io = NULL;
    AVFormatContext *input = NULL, *out = NULL;
    AVPacket *packet = NULL;
    int *stream_map = NULL;
    int *stream_ids = NULL;
    DvdClockSegment *segments = NULL;
    int segment_count = 0, segment_index = 0;
    jint *selected_indexes = NULL;
    jsize selected_count = -1;
    int64_t timestamp_origin_us = 0;


    ret = init_source(env, fd_array, starts_array, ends_array, &source, (DvdUdfSource *)(intptr_t)iso_handle, title_set, &cancel, error, sizeof(error));
    if (ret < 0) goto cleanup;
    if (is_cancelled(&cancel)) { ret = AVERROR_EXIT; goto cleanup; }
    if (ftruncate(output_fd, 0) != 0) {
        snprintf(error, sizeof(error), "Could not truncate output document: %s", strerror(errno));
        ret = AVERROR(errno); goto cleanup;
    }

    ret = dvd_clock_segments(&source, &segments, &segment_count);
    if (ret < 0) { ff_error(error, sizeof(error), "Could not read DVD navigation timing", ret); goto cleanup; }
    ret = open_clock_segment(&source, (DvdClockSegment){0, source.stream_size, 0}, &input, &input_io);
    if (ret < 0) { ff_error(error, sizeof(error), "Could not probe DVD streams", ret); goto cleanup; }

    ret = apply_dvd_ifo_metadata(env, input, language_records, palette_array);
    if (ret < 0) { ff_error(error, sizeof(error), "Could not apply DVD IFO metadata", ret); goto cleanup; }

    /* One clock origin for every stream preserves A/V offsets and aligns chapters.
       Per-stream zeroing would silently erase synchronization differences. */
    if (input->start_time != AV_NOPTS_VALUE) timestamp_origin_us = input->start_time;

    ret = avformat_alloc_output_context2(&out, NULL, "matroska", NULL);
    if (ret < 0 || !out) { if (ret >= 0) ret = AVERROR_UNKNOWN; ff_error(error, sizeof(error), "Could not create Matroska muxer", ret); goto cleanup; }

    out->avoid_negative_ts = AVFMT_AVOID_NEG_TS_DISABLED;

    stream_map = av_malloc_array(input->nb_streams, sizeof(*stream_map));
    stream_ids = av_malloc_array(input->nb_streams, sizeof(*stream_ids));
    if (!stream_map || !stream_ids) { ret = AVERROR(ENOMEM); goto cleanup; }
    for (unsigned i = 0; i < input->nb_streams; ++i) stream_map[i] = -1;

    if (selected_streams) {
        selected_count = (*env)->GetArrayLength(env, selected_streams);
        if (selected_count <= 0) { snprintf(error, sizeof(error), "No streams selected"); ret = AVERROR(EINVAL); goto cleanup; }
        selected_indexes = (*env)->GetIntArrayElements(env, selected_streams, NULL);
        if (!selected_indexes) { ret = AVERROR(ENOMEM); goto cleanup; }
    }

    for (unsigned i = 0; i < input->nb_streams; ++i) {
        AVStream *in_stream = input->streams[i];
        enum AVMediaType type = in_stream->codecpar->codec_type;
        if (type != AVMEDIA_TYPE_VIDEO && type != AVMEDIA_TYPE_AUDIO && type != AVMEDIA_TYPE_SUBTITLE) continue;
        if (selected_indexes) {
            int wanted = 0;
            for (jsize j = 0; j < selected_count; ++j) if (selected_indexes[j] == (jint)i) { wanted = 1; break; }
            if (!wanted) continue;
        }
        AVStream *out_stream = avformat_new_stream(out, NULL);
        if (!out_stream) { ret = AVERROR(ENOMEM); goto cleanup; }
        stream_map[i] = out_stream->index;
        stream_ids[out_stream->index] = in_stream->id;
        ret = avcodec_parameters_copy(out_stream->codecpar, in_stream->codecpar);
        if (ret < 0) { ff_error(error, sizeof(error), "Could not copy stream parameters", ret); goto cleanup; }
        out_stream->codecpar->codec_tag = 0;
        out_stream->time_base = in_stream->time_base;
        av_dict_copy(&out_stream->metadata, in_stream->metadata, 0);
    }
    if (out->nb_streams == 0) { snprintf(error, sizeof(error), "DVD title contains no Matroska-compatible streams"); ret = AVERROR_STREAM_NOT_FOUND; goto cleanup; }

    ret = add_chapters(env, out, chapter_starts, chapter_ends);
    if (ret < 0) { ff_error(error, sizeof(error), "Could not add DVD chapters", ret); goto cleanup; }
    av_dict_set(&out->metadata, "encoder", "MattMux Android (stream copy)", 0);

    uint8_t *output_buffer = av_malloc(IO_BUFFER_SIZE);
    if (!output_buffer) { ret = AVERROR(ENOMEM); goto cleanup; }
    output_io = avio_alloc_context(output_buffer, IO_BUFFER_SIZE, 1, &output, NULL, output_write, output_seek);
    if (!output_io) { av_free(output_buffer); ret = AVERROR(ENOMEM); goto cleanup; }
    out->pb = output_io;
    out->flags |= AVFMT_FLAG_CUSTOM_IO;

    ret = avformat_write_header(out, NULL);
    if (ret < 0) { ff_error(error, sizeof(error), "Could not write Matroska header", ret); goto cleanup; }


    packet = av_packet_alloc();
    if (!packet) { ret = AVERROR(ENOMEM); goto cleanup; }
    jclass cls = (*env)->GetObjectClass(env, thiz);
    jmethodID progress_method = cls ? (*env)->GetMethodID(env, cls, "onNativeProgress", "(I)V") : NULL;
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); progress_method = NULL; }
    int last_percent = -1;
    int64_t processed = 0;

    /* Discover the complete title first, then isolate parser lookahead at
       clock boundaries without losing streams absent from the first segment. */
    for (segment_index = 0; segment_index < segment_count; ++segment_index) {
        if (segment_count > 1) {
            avformat_close_input(&input);
            av_freep(&input_io->buffer); avio_context_free(&input_io);
            ret = open_clock_segment(&source, segments[segment_index], &input, &input_io);
            if (ret < 0) { ff_error(error, sizeof(error), "Could not open next DVD clock segment", ret); goto cleanup; }
            av_freep(&stream_map);
            stream_map = av_malloc_array(input->nb_streams, sizeof(*stream_map));
            if (!stream_map) { ret = AVERROR(ENOMEM); goto cleanup; }
            for (unsigned i = 0; i < input->nb_streams; ++i) {
                stream_map[i] = -1;
                for (unsigned j = 0; j < out->nb_streams; ++j) {
                    if (input->streams[i]->id == stream_ids[j] &&
                        input->streams[i]->codecpar->codec_id == out->streams[j]->codecpar->codec_id) {
                        stream_map[i] = j;
                        break;
                    }
                }
            }
            if (segment_index == 0 && input->start_time != AV_NOPTS_VALUE)
                timestamp_origin_us = input->start_time;
        }
        while ((ret = av_read_frame(input, packet)) >= 0) {
            if (is_cancelled(&cancel)) { ret = AVERROR_EXIT; break; }
            if (packet->pos >= 0 && segments[segment_index].start + packet->pos > processed)
                processed = segments[segment_index].start + packet->pos;
            int in_index = packet->stream_index;
            int out_index = (in_index >= 0 && (unsigned)in_index < input->nb_streams) ? stream_map[in_index] : -1;
            if (out_index >= 0) {
                AVStream *in_stream = input->streams[in_index];
                AVStream *out_stream = out->streams[out_index];
                int64_t origin = av_rescale_q(timestamp_origin_us, AV_TIME_BASE_Q, in_stream->time_base);
                int64_t shift = av_rescale_q(segments[segment_index].offset, (AVRational){1, 90000}, in_stream->time_base) - origin;
                if (packet->pts != AV_NOPTS_VALUE) packet->pts += shift;
                if (packet->dts != AV_NOPTS_VALUE) packet->dts += shift;
                packet->stream_index = out_index;
                av_packet_rescale_ts(packet, in_stream->time_base, out_stream->time_base);
                packet->pos = -1;
                /* The interleaver consumes packet even on failure. Capture context
                   first; it may also fail while flushing an earlier queued packet. */
                char write_step[256];
                snprintf(write_step, sizeof(write_step),
                    "Matroska write failed while submitting %s stream %d (%s; pts=%s, dts=%s, time_base=%d/%d)",
                    track_type_name(in_stream->codecpar->codec_type), in_index,
                    avcodec_get_name(in_stream->codecpar->codec_id),
                    av_ts2str(packet->pts), av_ts2str(packet->dts),
                    out_stream->time_base.num, out_stream->time_base.den);
                ret = av_interleaved_write_frame(out, packet);
                if (ret < 0) { ff_error(error, sizeof(error), write_step, ret); av_packet_unref(packet); break; }
            }
            av_packet_unref(packet);
            int percent = source.stream_size > 0 ? (int)(100.0 * processed / source.stream_size) : 0;
            if (percent > 99) percent = 99;
            if (percent < last_percent) percent = last_percent;
            if (percent != last_percent) { report_progress(env, thiz, progress_method, percent); last_percent = percent; }
        }
        if (ret == AVERROR_EOF) {
            ret = (input_io->error < 0 && input_io->error != AVERROR_EOF) ? input_io->error : 0;
        }
        if (ret < 0) break;
    } /* clock segments */
    if (ret == AVERROR_EXIT || is_cancelled(&cancel)) {
        snprintf(error, sizeof(error), "Remux cancelled");
        ret = AVERROR_EXIT;
    } else if (ret < 0 && error[0] == '\0') {
        ff_error(error, sizeof(error), "DVD read failed", ret);
    }

    if (ret >= 0) {
        ret = av_write_trailer(out);
        if (ret < 0) ff_error(error, sizeof(error), "Could not finalize Matroska file", ret);
        else {
            avio_flush(output_io);
            if (output_io->error < 0) ret = output_io->error;
            else if (fsync(output_fd) < 0) ret = AVERROR(errno);
            if (is_cancelled(&cancel)) ret = AVERROR_EXIT;
            if (ret < 0) ff_error(error, sizeof(error), "Could not flush output document", ret);
            else report_progress(env, thiz, progress_method, 100);
        }
    }

cleanup:
    if (packet) av_packet_free(&packet);
    if (selected_indexes) (*env)->ReleaseIntArrayElements(env, selected_streams, selected_indexes, JNI_ABORT);
    av_freep(&stream_map);
    av_freep(&stream_ids);
    av_freep(&segments);
    if (out) {
        out->pb = NULL;
        avformat_free_context(out);
    }
    if (output_io) { av_freep(&output_io->buffer); avio_context_free(&output_io); }
    if (input) {
        if (input->iformat) avformat_close_input(&input);
        else avformat_free_context(input);
    }
    if (input_io) { av_freep(&input_io->buffer); avio_context_free(&input_io); }
    free_source(&source);

    if (ret >= 0) return NULL;
    if (ret == AVERROR_EXIT) snprintf(error, sizeof(error), "Remux cancelled");
    if (error[0] == '\0') ff_error(error, sizeof(error), "Native remux failed", ret);
    return (*env)->NewStringUTF(env, error);
}

#include "advanced_merger_jni.c"
#include "demux_jni.c"


#ifdef MATTMUX_DVDNAV
JNIEXPORT jlongArray JNICALL
Java_io_github_maas3n_mattmux_AndroidNativeRemuxEngine_nativeScanDvdNav(JNIEnv *env, jobject thiz, jstring path_string)
{
    (void)thiz;
    if (!path_string) return NULL;
    const char *path = (*env)->GetStringUTFChars(env, path_string, NULL);
    if (!path) return NULL;

    dvdnav_t *nav = NULL;
    if (dvdnav_open(&nav, path) != DVDNAV_STATUS_OK || !nav) {
        __android_log_print(ANDROID_LOG_ERROR, "MattRipDVDNav", "dvdnav_open failed for %s", path);
        (*env)->ReleaseStringUTFChars(env, path_string, path);
        if (nav) dvdnav_close(nav);
        return NULL;
    }
    (*env)->ReleaseStringUTFChars(env, path_string, path);

    int32_t title_count = 0;
    if (dvdnav_get_number_of_titles(nav, &title_count) != DVDNAV_STATUS_OK || title_count <= 0) {
        __android_log_print(ANDROID_LOG_ERROR, "MattRipDVDNav", "dvdnav_get_number_of_titles failed");
        dvdnav_close(nav);
        return NULL;
    }

    const jsize result_count = (jsize)title_count + 3;
    jlong *values = calloc((size_t)result_count, sizeof(*values));
    if (!values) {
        dvdnav_close(nav);
        return NULL;
    }

    // libdvdnav takes one-based DVD title numbers; only libdvdread arrays are zero-based.
    int32_t best_title = 0;
    uint64_t best_duration = 0;
    for (int32_t title = 1; title <= title_count; ++title) {
        uint64_t *chapter_times = NULL;
        uint64_t duration = 0;
        uint32_t chapters = dvdnav_describe_title_chapters(nav, title, &chapter_times, &duration);
        __android_log_print(ANDROID_LOG_INFO, "MattRipDVDNav",
                            "title %d/%d chapters=%u duration_ticks=%llu",
                            title, title_count, chapters, (unsigned long long)duration);
        free(chapter_times);
        values[title + 2] = (jlong)duration;
        if (chapters > 0 && duration > best_duration) {
            best_duration = duration;
            best_title = title;
        }
    }

    if (!best_title) {
        free(values);
        dvdnav_close(nav);
        return NULL;
    }

    values[0] = (jlong)title_count;
    values[1] = (jlong)best_title;
    values[2] = (jlong)best_duration;
    jlongArray result = (*env)->NewLongArray(env, result_count);
    if (result) (*env)->SetLongArrayRegion(env, result, 0, result_count, values);
    free(values);
    dvdnav_close(nav);
    return result;
}

static int mattmux_ascii_language(uint16_t code, char out[3])
{
    unsigned char a = (unsigned char)(code >> 8), b = (unsigned char)(code & 0xff);
    if (!((a >= 'A' && a <= 'Z') || (a >= 'a' && a <= 'z')) ||
        !((b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z'))) return 0;
    out[0] = (char)((a >= 'A' && a <= 'Z') ? a + ('a' - 'A') : a);
    out[1] = (char)((b >= 'A' && b <= 'Z') ? b + ('a' - 'A') : b);
    out[2] = '\0';
    return 1;
}

static int mattmux_audio_stream_id(int format, int position)
{
    switch (format) {
        case 0: return 0x80 + position;
        case 2:
        case 3: return 0x1c0 + position;
        case 4: return 0xa0 + position;
        case 6: return 0x88 + position;
        default: return -1;
    }
}

static int mattmux_yuv_to_rgb(uint32_t raw)
{
    int y = (raw >> 16) & 0xff, cr = (raw >> 8) & 0xff, cb = raw & 0xff;
    int c = y - 16, d = cb - 128, e = cr - 128;
    int r = (298 * c + 409 * e + 128) >> 8;
    int g = (298 * c - 100 * d - 208 * e + 128) >> 8;
    int b = (298 * c + 516 * d + 128) >> 8;
    if (r < 0) r = 0; else if (r > 255) r = 255;
    if (g < 0) g = 0; else if (g > 255) g = 255;
    if (b < 0) b = 0; else if (b > 255) b = 255;
    return (r << 16) | (g << 8) | b;
}

static int mattmux_add_row(JNIEnv *env, jobjectArray array, int index, const char *text)
{
    jstring value = (*env)->NewStringUTF(env, text);
    if (!value) return 0;
    (*env)->SetObjectArrayElement(env, array, index, value);
    (*env)->DeleteLocalRef(env, value);
    return !(*env)->ExceptionCheck(env);
}

JNIEXPORT jobjectArray JNICALL
Java_io_github_maas3n_mattmux_AndroidNativeRemuxEngine_nativePlanDvdNav(JNIEnv *env, jobject thiz, jstring path_string, jint global_title)
{
    (void)thiz;
    if (!path_string || global_title <= 0) return NULL;
    const char *path = (*env)->GetStringUTFChars(env, path_string, NULL);
    if (!path) return NULL;

    dvdnav_t *nav = NULL;
    dvd_reader_t *dvd = NULL;
    ifo_handle_t *vmg = NULL, *vts = NULL;
    uint64_t *chapter_times = NULL, duration = 0;
    jobjectArray result = NULL;
    char **rows = NULL;
    int row_count = 0, row_capacity = 0;

#define ADD_ROW(...) do { \
    if (row_count >= row_capacity) { \
        int next_capacity = row_capacity ? row_capacity * 2 : 64; \
        char **next = realloc(rows, (size_t)next_capacity * sizeof(*rows)); \
        if (!next) goto cleanup_plan; \
        rows = next; row_capacity = next_capacity; \
    } \
    char temp[256]; snprintf(temp, sizeof(temp), __VA_ARGS__); \
    rows[row_count] = strdup(temp); \
    if (!rows[row_count]) goto cleanup_plan; \
    row_count++; \
} while (0)

    if (dvdnav_open(&nav, path) != DVDNAV_STATUS_OK || !nav) goto cleanup_plan;
    int32_t title_count = 0;
    if (dvdnav_get_number_of_titles(nav, &title_count) != DVDNAV_STATUS_OK || global_title > title_count) goto cleanup_plan;
    uint32_t nav_chapters = dvdnav_describe_title_chapters(nav, global_title, &chapter_times, &duration);
    if (!nav_chapters || !duration) goto cleanup_plan;

    dvd = DVDOpen(path);
    if (!dvd) goto cleanup_plan;
    vmg = ifoOpen(dvd, 0);
    if (!vmg || !vmg->tt_srpt || global_title > vmg->tt_srpt->nr_of_srpts) goto cleanup_plan;
    title_info_t info = vmg->tt_srpt->title[global_title - 1];
    if (info.title_set_nr <= 0 || info.vts_ttn <= 0) goto cleanup_plan;
    vts = ifoOpen(dvd, info.title_set_nr);
    if (!vts || !vts->vts_ptt_srpt || !vts->vts_pgcit || !vts->vtsi_mat) goto cleanup_plan;
    if (info.vts_ttn > vts->vts_ptt_srpt->nr_of_srpts) goto cleanup_plan;

    ttu_t *ttu = &vts->vts_ptt_srpt->title[info.vts_ttn - 1];
    if (!ttu || ttu->nr_of_ptts <= 0 || !ttu->ptt) goto cleanup_plan;
    int pgcn = ttu->ptt[0].pgcn;
    int first_pgn = ttu->ptt[0].pgn;
    int last_pgn = ttu->ptt[ttu->nr_of_ptts - 1].pgn;
    if (pgcn <= 0 || pgcn > vts->vts_pgcit->nr_of_pgci_srp) goto cleanup_plan;
    for (int i = 0, prev = 0; i < ttu->nr_of_ptts; ++i) {
        if (ttu->ptt[i].pgcn != pgcn || ttu->ptt[i].pgn <= prev) goto cleanup_plan;
        prev = ttu->ptt[i].pgn;
    }
    pgc_t *pgc = vts->vts_pgcit->pgci_srp[pgcn - 1].pgc;
    if (!pgc || !pgc->program_map || !pgc->cell_playback || pgc->nr_of_programs <= 0 || pgc->nr_of_cells <= 0) goto cleanup_plan;
    if (first_pgn <= 0 || first_pgn > pgc->nr_of_programs || last_pgn > pgc->nr_of_programs) goto cleanup_plan;
    if (pgc->pg_playback_mode != 0 || pgc->still_time != 0) goto cleanup_plan;

    int end_program_exclusive = pgc->nr_of_programs + 1;
    for (int i = 0; i < vts->vts_ptt_srpt->nr_of_srpts; ++i) {
        if (i == info.vts_ttn - 1) continue;
        ttu_t *other = &vts->vts_ptt_srpt->title[i];
        if (!other || other->nr_of_ptts <= 0 || !other->ptt) continue;
        if (other->ptt[0].pgcn == pgcn && other->ptt[0].pgn > last_pgn && other->ptt[0].pgn < end_program_exclusive)
            end_program_exclusive = other->ptt[0].pgn;
    }

    ADD_ROW("T\t%d\t%d\t%llu", global_title, info.title_set_nr, (unsigned long long)(duration / 90ULL));

    for (int program = first_pgn; program < end_program_exclusive; ++program) {
        int first_cell = pgc->program_map[program - 1];
        int last_cell = program < pgc->nr_of_programs ? pgc->program_map[program] - 1 : pgc->nr_of_cells;
        if (first_cell <= 0 || last_cell < first_cell || last_cell > pgc->nr_of_cells) goto cleanup_plan;
        for (int cell = first_cell; cell <= last_cell; ++cell) {
            cell_playback_t *cp = &pgc->cell_playback[cell - 1];
            if (cp->interleaved || cp->still_time != 0) goto cleanup_plan;
            if (cp->block_type == BLOCK_TYPE_ANGLE_BLOCK) {
                if (cp->block_mode == BLOCK_MODE_IN_BLOCK || cp->block_mode == BLOCK_MODE_LAST_CELL) continue;
                if (cp->block_mode != BLOCK_MODE_FIRST_CELL) goto cleanup_plan;
            } else if (cp->block_mode != BLOCK_MODE_NOT_IN_BLOCK) goto cleanup_plan;
            if (cp->last_sector < cp->first_sector) goto cleanup_plan;
            ADD_ROW("C\t%u\t%u", cp->first_sector, cp->last_sector + 1U);
        }
    }

    uint64_t previous = 0;
    for (uint32_t i = 0; i < nav_chapters; ++i) {
        uint64_t end = chapter_times ? chapter_times[i] : 0;
        if (i + 1 == nav_chapters && duration > end) end = duration;
        if (end <= previous) goto cleanup_plan;
        ADD_ROW("H\t%llu\t%llu", (unsigned long long)(previous / 90ULL), (unsigned long long)(end / 90ULL));
        previous = end;
    }

    for (int i = 0; i < vts->vtsi_mat->nr_of_vts_audio_streams && i < 8; ++i) {
        uint16_t control = pgc->audio_control[i];
        if (!(control & 0x8000)) continue;
        int stream_id = mattmux_audio_stream_id(vts->vtsi_mat->vts_audio_attr[i].audio_format, (control >> 8) & 0x7f);
        char lang[3];
        if (stream_id >= 0 && mattmux_ascii_language(vts->vtsi_mat->vts_audio_attr[i].lang_code, lang)) ADD_ROW("L\t%d\t%s", stream_id, lang);
    }
    for (int i = 0; i < vts->vtsi_mat->nr_of_vts_subp_streams && i < 32; ++i) {
        uint32_t control = pgc->subp_control[i];
        char lang[3];
        if (!(control & 0x80000000U) || !mattmux_ascii_language(vts->vtsi_mat->vts_subp_attr[i].lang_code, lang)) continue;
        int offsets[4] = { (control >> 24) & 0x1f, (control >> 16) & 0x1f, (control >> 8) & 0x1f, control & 0x1f };
        for (int j = 0; j < 4; ++j) ADD_ROW("L\t%d\t%s", 0x20 + offsets[j], lang);
    }
    for (int i = 0; i < 16; ++i) ADD_ROW("P\t%d\t%d", i, mattmux_yuv_to_rgb(pgc->palette[i]));

    result = (*env)->NewObjectArray(env, row_count, (*env)->FindClass(env, "java/lang/String"), NULL);
    if (!result) goto cleanup_plan;
    for (int i = 0; i < row_count; ++i) if (!mattmux_add_row(env, result, i, rows[i])) { result = NULL; break; }

cleanup_plan:
    for (int i = 0; i < row_count; ++i) free(rows[i]);
    free(rows);
    free(chapter_times);
    if (vts) ifoClose(vts);
    if (vmg) ifoClose(vmg);
    if (dvd) DVDClose(dvd);
    if (nav) dvdnav_close(nav);
    (*env)->ReleaseStringUTFChars(env, path_string, path);
    return result;
#undef ADD_ROW
}

#endif
