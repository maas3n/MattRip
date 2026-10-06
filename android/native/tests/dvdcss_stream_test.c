/* Exercise the production adapter with both byte callbacks and libdvdcss's
 * public block API. Synthetic bytes suffice; no encrypted DVD is required. */
#include <assert.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/types.h>
#include <dvdcss/dvdcss.h>

typedef struct {
    int64_t total_source_size, dvdcss_stream_pos;
    const unsigned char *data;
    size_t max_read;
    int fail_read;
} SourceContext;

static ssize_t source_raw_read_at(SourceContext *ctx, int64_t pos, void *buf, size_t bytes)
{
    /* Catch the old 2048x over-read before writing, including when libdvdcss
     * owns the destination buffer and was built without a sanitizer. */
    assert(bytes <= ctx->max_read);
    if (ctx->fail_read || pos < 0 || pos > ctx->total_source_size) return -1;
    size_t available = (size_t)(ctx->total_source_size - pos);
    if (bytes > available) bytes = available;
    if (bytes) memcpy(buf, ctx->data + pos, bytes);
    return (ssize_t)bytes;
}

#ifndef CSS_STREAM_HEADER
#define CSS_STREAM_HEADER "../dvdcss_stream.h"
#endif
#include CSS_STREAM_HEADER

int main(void)
{
    unsigned char data[8 * DVDCSS_BLOCK_SIZE];
    for (size_t i = 0; i < sizeof(data); ++i) data[i] = (unsigned char)(i * 13 + i / 2048);
    /* Recognize this as a VOB stream and avoid disc-cache metadata probing. */
    data[0] = 0; data[1] = 0; data[2] = 1; data[3] = 0xba;
    SourceContext ctx = {sizeof(data), 0, data, 2 * DVDCSS_BLOCK_SIZE, 0};
    unsigned char out[2 * DVDCSS_BLOCK_SIZE];

    assert(mattmux_dvdcss_stream_seek(&ctx, 2048) == 0);
    assert(ctx.dvdcss_stream_pos == 2048);
    assert(mattmux_dvdcss_stream_read(&ctx, out, 2048) == 2048);
    assert(ctx.dvdcss_stream_pos == 4096);
    assert(memcmp(out, data + 2048, 2048) == 0);
    /* Byte API also permits unaligned requests and short reads at EOF. */
    assert(mattmux_dvdcss_stream_seek(&ctx, sizeof(data) - 17) == 0);
    assert(mattmux_dvdcss_stream_read(&ctx, out, 31) == 17);
    assert(memcmp(out, data + sizeof(data) - 17, 17) == 0);
    assert(mattmux_dvdcss_stream_read(&ctx, out, 31) == 0);
    assert(mattmux_dvdcss_stream_seek(&ctx, sizeof(data)) == 0);
    assert(mattmux_dvdcss_stream_seek(&ctx, sizeof(data) + 1) == -1);
    assert(mattmux_dvdcss_stream_seek(&ctx, UINT64_MAX) == -1);
    assert(ctx.dvdcss_stream_pos == sizeof(data));
    assert(mattmux_dvdcss_stream_read(&ctx, NULL, 0) == 0);
    assert(mattmux_dvdcss_stream_read(&ctx, NULL, 1) == -1);
    assert(mattmux_dvdcss_stream_read(&ctx, out, -1) == -1);
    assert(mattmux_dvdcss_stream_seek(NULL, 0) == -1);
    assert(mattmux_dvdcss_stream_read(NULL, out, 1) == -1);
    ctx.fail_read = 1;
    assert(mattmux_dvdcss_stream_read(&ctx, out, 1) == -1);
    assert(ctx.dvdcss_stream_pos == sizeof(data));
    ctx.fail_read = 0;

    /* DVD byte positions can exceed INT_MAX even though block indices do not. */
    ctx.total_source_size = INT64_C(8) * 1024 * 1024 * 1024;
    assert(mattmux_dvdcss_stream_seek(&ctx, UINT64_C(5) * 1024 * 1024 * 1024) == 0);
    assert(ctx.dvdcss_stream_pos == INT64_C(5) * 1024 * 1024 * 1024);
    ctx.total_source_size = sizeof(data);
    ctx.dvdcss_stream_pos = 0;

    dvdcss_stream_cb callbacks = {mattmux_dvdcss_stream_seek, mattmux_dvdcss_stream_read, NULL};
    dvdcss_t css = dvdcss_open_stream(&ctx, &callbacks);
    assert(css != NULL);
    assert(dvdcss_seek(css, 2, DVDCSS_NOFLAGS) == 2);
    assert(ctx.dvdcss_stream_pos == 2 * DVDCSS_BLOCK_SIZE);
    assert(dvdcss_read(css, out, 2, DVDCSS_NOFLAGS) == 2);
    assert(memcmp(out, data + 2 * DVDCSS_BLOCK_SIZE, sizeof(out)) == 0);
    assert(ctx.dvdcss_stream_pos == 4 * DVDCSS_BLOCK_SIZE);
    assert(dvdcss_seek(css, 7, DVDCSS_NOFLAGS) == 7);
    assert(dvdcss_read(css, out, 1, DVDCSS_NOFLAGS) == 1);
    assert(memcmp(out, data + 7 * DVDCSS_BLOCK_SIZE, DVDCSS_BLOCK_SIZE) == 0);
    assert(dvdcss_read(css, out, 1, DVDCSS_NOFLAGS) == 0);
    assert(dvdcss_close(css) == 0);
    puts("libdvdcss stream callback contract PASS");
    return 0;
}
