#ifndef MATTRIP_DVDCSS_STREAM_H
#define MATTRIP_DVDCSS_STREAM_H

/* Included after SourceContext and source_raw_read_at. Unlike the public
 * dvdcss_seek/read API, libdvdcss 1.6.0 stream callbacks use BYTES. Its device.c
 * converts blocks before calling us; seek returns 0, read returns bytes read.
 * Keep this adapter shared with the callback contract regression test. */
static int mattmux_dvdcss_stream_seek(void *opaque, uint64_t byte_offset)
{
    SourceContext *ctx = (SourceContext *)opaque;
    if (!ctx || ctx->total_source_size < 0 ||
        byte_offset > (uint64_t)ctx->total_source_size) return -1;
    ctx->dvdcss_stream_pos = (int64_t)byte_offset;
    return 0;
}

static int mattmux_dvdcss_stream_read(void *opaque, void *buffer, int bytes)
{
    SourceContext *ctx = (SourceContext *)opaque;
    if (!ctx || bytes < 0 || (!buffer && bytes != 0)) return -1;
    if (bytes == 0) return 0;
    ssize_t n = source_raw_read_at(ctx, ctx->dvdcss_stream_pos, buffer, (size_t)bytes);
    if (n < 0 || n > bytes) return -1;
    ctx->dvdcss_stream_pos += n;
    return (int)n;
}

#endif
