#include "server.h"

#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <strings.h>
#include <sys/stat.h>
#include <unistd.h>

#define CODIS_RDB_EXPORT_PATH "/codis/rdb/latest"
#define CODIS_RDB_EXPORT_AUTH_HEADER "X-Codis-RDB-Auth"
#define CODIS_RDB_EXPORT_MAX_HEADER (1024*8)

extern int time_independent_strcmp(char *a, char *b, int len);

typedef struct codisRdbExportState {
    int fd;
    sds filename;
    sds header;
    size_t header_sent;
    off_t filesize;
    off_t sent_file_bytes;
    time_t mtime;
    char buf[PROTO_IOBUF_LEN];
    size_t buf_len;
    size_t buf_sent;
} codisRdbExportState;

typedef struct codisRdbExportFile {
    int fd;
    sds filename;
    off_t filesize;
    time_t mtime;
} codisRdbExportFile;

static void codisRdbExportWriteHandler(connection *conn);

static int codisRdbExportMoveToMainThread(client *c) {
    if (c->running_tid == IOTHREAD_MAIN_THREAD_ID) return 0;
    c->io_flags |= CLIENT_IO_PENDING_RDB_EXPORT;
    enqueuePendingClientsToMainThread(c, 0);
    return 1;
}

static int codisRdbExportTokenEquals(const char *p, size_t len, const char *token) {
    size_t token_len = strlen(token);
    return len == token_len && memcmp(p, token, token_len) == 0;
}

static const char *codisRdbExportFindCRLF(const char *p, size_t len) {
    for (size_t i = 0; i + 1 < len; i++) {
        if (p[i] == '\r' && p[i+1] == '\n') return p + i;
    }
    return NULL;
}

static const char *codisRdbExportFindHeaderEnd(const char *p, size_t len) {
    for (size_t i = 0; i + 3 < len; i++) {
        if (p[i] == '\r' && p[i+1] == '\n' &&
            p[i+2] == '\r' && p[i+3] == '\n') return p + i + 4;
    }
    return NULL;
}

static int codisRdbExportGetRequestLineMatches(const char *p, size_t len) {
    const char *req10 = "GET " CODIS_RDB_EXPORT_PATH " HTTP/1.0";
    const char *req11 = "GET " CODIS_RDB_EXPORT_PATH " HTTP/1.1";
    size_t req_len = strlen(req10);
    size_t cmp_len = len < req_len ? len : req_len;

    return memcmp(p, req10, cmp_len) == 0 || memcmp(p, req11, cmp_len) == 0;
}

static int codisRdbExportVersionOk(const char *p, size_t len) {
    return codisRdbExportTokenEquals(p, len, "HTTP/1.0") ||
           codisRdbExportTokenEquals(p, len, "HTTP/1.1");
}

static int codisRdbExportParseRequestLine(const char *line, size_t len,
                                          int *is_export_get)
{
    const char *sp1 = memchr(line, ' ', len);
    if (!sp1) return C_ERR;
    const char *sp2 = memchr(sp1 + 1, ' ', len - (sp1 + 1 - line));
    if (!sp2) return C_ERR;

    size_t method_len = sp1 - line;
    const char *target = sp1 + 1;
    size_t target_len = sp2 - target;
    const char *version = sp2 + 1;
    size_t version_len = len - (version - line);

    if (memchr(version, ' ', version_len)) return C_ERR;

    int target_exact = codisRdbExportTokenEquals(target, target_len, CODIS_RDB_EXPORT_PATH);
    int version_ok = codisRdbExportVersionOk(version, version_len);
    int method_get = codisRdbExportTokenEquals(line, method_len, "GET");

    *is_export_get = 0;

    if (target_exact && version_ok && method_get) {
        *is_export_get = 1;
        return C_OK;
    }

    return C_ERR;
}

static int codisRdbExportHeaderNameEquals(const char *p, size_t len, const char *name) {
    size_t name_len = strlen(name);
    if (len != name_len) return 0;
    for (size_t i = 0; i < len; i++) {
        if (tolower((unsigned char)p[i]) != tolower((unsigned char)name[i]))
            return 0;
    }
    return 1;
}

static int codisRdbExportFindHeader(const char *headers, size_t len,
                                    const char *name,
                                    const char **value, size_t *value_len)
{
    const char *p = headers;
    const char *end = headers + len;

    while (p < end) {
        const char *line_end = codisRdbExportFindCRLF(p, end - p);
        if (!line_end) return 0;
        if (line_end == p) return 0;

        const char *colon = memchr(p, ':', line_end - p);
        if (colon) {
            const char *name_end = colon;
            while (name_end > p && (name_end[-1] == ' ' || name_end[-1] == '\t'))
                name_end--;

            const char *v = colon + 1;
            while (v < line_end && (*v == ' ' || *v == '\t')) v++;
            const char *v_end = line_end;
            while (v_end > v && (v_end[-1] == ' ' || v_end[-1] == '\t'))
                v_end--;

            if (codisRdbExportHeaderNameEquals(p, name_end - p, name)) {
                *value = v;
                *value_len = v_end - v;
                return 1;
            }
        }
        p = line_end + 2;
    }
    return 0;
}

static int codisRdbExportAuthMatches(const char *value, size_t value_len) {
    char *auth = server.codis_rdb_export_auth;
    size_t auth_len = auth ? strlen(auth) : 0;

    if (auth_len == 0 || value_len != auth_len || value_len > INT_MAX)
        return 0;
    return time_independent_strcmp((char*)value, auth, (int)value_len) == 0;
}

static int codisRdbExportReadMagic(int fd, char magic[5]) {
    size_t done = 0;
    while (done < 5) {
        ssize_t nread = pread(fd, magic + done, 5 - done, done);
        if (nread == -1) {
            if (errno == EINTR) continue;
            return C_ERR;
        }
        if (nread == 0) return C_ERR;
        done += nread;
    }
    return C_OK;
}

static int codisRdbExportOpenDbfilename(codisRdbExportFile *file, const char **reason) {
    char *filename = server.rdb_filename;
    struct stat lst, fst;
    char magic[5];

    memset(file, 0, sizeof(*file));
    file->fd = -1;
    *reason = "unknown";

    if (filename == NULL || filename[0] == '\0') {
        *reason = "empty-dbfilename";
        return C_ERR;
    }
    if (strchr(filename, '/') != NULL || strchr(filename, '\\') != NULL) {
        *reason = "path-dbfilename";
        return C_ERR;
    }

    size_t filename_len = strlen(filename);
    if (filename_len < 4 || strcasecmp(filename + filename_len - 4, ".rdb") != 0) {
        *reason = "non-rdb-dbfilename";
        return C_ERR;
    }

    if (lstat(filename, &lst) == -1) {
        *reason = "lstat-failed";
        return C_ERR;
    }
    if (!S_ISREG(lst.st_mode)) {
        *reason = "not-regular-file";
        return C_ERR;
    }

#ifdef O_CLOEXEC
    int fd = open(filename, O_RDONLY | O_CLOEXEC);
#else
    int fd = open(filename, O_RDONLY);
#endif
    if (fd == -1) {
        *reason = "open-failed";
        return C_ERR;
    }

    if (fstat(fd, &fst) == -1) {
        *reason = "fstat-failed";
        close(fd);
        return C_ERR;
    }
    if (lst.st_dev != fst.st_dev || lst.st_ino != fst.st_ino ||
        lst.st_mode != fst.st_mode || !S_ISREG(fst.st_mode))
    {
        *reason = "identity-changed";
        close(fd);
        return C_ERR;
    }
    if (fst.st_size < 5 || codisRdbExportReadMagic(fd, magic) == C_ERR ||
        memcmp(magic, "REDIS", 5) != 0)
    {
        *reason = "bad-rdb-magic";
        close(fd);
        return C_ERR;
    }

    file->fd = fd;
    file->filename = sdsnew(filename);
    file->filesize = fst.st_size;
    file->mtime = fst.st_mtime;
    return C_OK;
}

static sds codisRdbExportAppendHeaderFilename(sds out, const char *filename) {
    for (const char *p = filename; *p; p++) {
        unsigned char ch = (unsigned char)*p;
        if (ch < ' ' || ch == '"' || ch == '\\' || ch == '\r' || ch == '\n')
            out = sdscatlen(out, "_", 1);
        else
            out = sdscatlen(out, p, 1);
    }
    return out;
}

static void codisRdbExportAccountWritten(client *c, ssize_t nwritten) {
    if (nwritten <= 0) return;
    atomicIncr(server.stat_net_output_bytes, nwritten);
    c->net_output_bytes += nwritten;
    c->lastinteraction = server.unixtime;
}

static void codisRdbExportFinish(client *c) {
    if (c->conn) connSetWriteHandler(c->conn, NULL);
    codisRdbExportCleanupClient(c);
    freeClientAsync(c);
}

static void codisRdbExportAbort(client *c, const char *reason) {
    codisRdbExportState *state = c->codis_rdb_export_state;
    if (state && state->fd != -1) {
        serverLog(LL_WARNING,
            "Codis RDB export aborted for %s after %lld/%lld bytes: %s",
            state->filename ? state->filename : "?", (long long)state->sent_file_bytes,
            (long long)state->filesize, reason);
    }
    codisRdbExportFinish(c);
}

static int codisRdbExportWriteOrDefer(client *c, const char *buf, size_t len,
                                      ssize_t *nwritten)
{
    *nwritten = connWrite(c->conn, buf, len);
    if (*nwritten <= 0) {
        if (*nwritten == -1 && connGetState(c->conn) != CONN_STATE_CONNECTED) {
            codisRdbExportAbort(c, connGetLastError(c->conn));
            return C_ERR;
        }
        return C_ERR;
    }
    codisRdbExportAccountWritten(c, *nwritten);
    return C_OK;
}

static int codisRdbExportStartState(client *c, codisRdbExportState *state) {
    c->codis_rdb_export_state = state;

    if (c->querybuf) {
        sdsclear(c->querybuf);
        c->qb_pos = 0;
    }
    c->io_flags &= ~CLIENT_IO_READ_ENABLED;
    if (c->conn) connSetReadHandler(c->conn, NULL);
    if (!c->conn || connSetWriteHandler(c->conn, codisRdbExportWriteHandler) == C_ERR) {
        codisRdbExportCleanupClient(c);
        freeClientAsync(c);
        return C_ERR;
    }
    return C_OK;
}

static int codisRdbExportStartErrorResponse(client *c, int code, const char *text) {
    codisRdbExportState *state = zcalloc(sizeof(*state));
    sds body = sdscatprintf(sdsempty(), "%s\n", text);

    state->fd = -1;
    state->header = sdscatprintf(sdsempty(),
        "HTTP/1.1 %d %s\r\n"
        "Content-Type: text/plain\r\n"
        "Content-Length: %zu\r\n"
        "Connection: close\r\n"
        "\r\n"
        "%s",
        code, text, sdslen(body), body);
    sdsfree(body);

    return codisRdbExportStartState(c, state);
}

static int codisRdbExportStartFileResponse(client *c, codisRdbExportFile *file) {
    codisRdbExportState *state = zcalloc(sizeof(*state));

    state->fd = file->fd;
    state->filename = file->filename;
    state->filesize = file->filesize;
    state->mtime = file->mtime;
    state->header = sdscatprintf(sdsempty(),
        "HTTP/1.1 200 OK\r\n"
        "Content-Type: application/octet-stream\r\n"
        "Content-Length: %lld\r\n"
        "Content-Disposition: attachment; filename=\"",
        (long long)file->filesize);
    state->header = codisRdbExportAppendHeaderFilename(state->header, file->filename);
    state->header = sdscatprintf(state->header,
        "\"\r\n"
        "X-Codis-RDB-Mtime: %lld\r\n"
        "Connection: close\r\n"
        "\r\n",
        (long long)file->mtime);

    serverLog(LL_NOTICE,
        "Codis RDB export started for %s (%lld bytes) to %s",
        file->filename, (long long)file->filesize, getClientPeerId(c));

    file->fd = -1;
    file->filename = NULL;
    return codisRdbExportStartState(c, state);
}

int codisRdbExportTryHandle(client *c) {
    if (c->codis_rdb_export_state != NULL || c->querybuf == NULL || c->qb_pos != 0)
        return CODIS_RDB_EXPORT_NOT_HTTP;

    const char *p = c->querybuf;
    size_t len = sdslen(c->querybuf);
    const char *line_end = codisRdbExportFindCRLF(p, len);

    if (!line_end) {
        if (codisRdbExportGetRequestLineMatches(p, len)) {
            if (len > CODIS_RDB_EXPORT_MAX_HEADER) {
                if (codisRdbExportMoveToMainThread(c))
                    return CODIS_RDB_EXPORT_HANDLED;
                codisRdbExportStartErrorResponse(c, 400, "Bad Request");
                return CODIS_RDB_EXPORT_HANDLED;
            }
            return CODIS_RDB_EXPORT_WAIT_MORE;
        }
        return CODIS_RDB_EXPORT_NOT_HTTP;
    }

    int is_export_get = 0;
    if (codisRdbExportParseRequestLine(p, line_end - p, &is_export_get) == C_ERR)
    {
        return CODIS_RDB_EXPORT_NOT_HTTP;
    }

    if (!is_export_get) return CODIS_RDB_EXPORT_NOT_HTTP;

    const char *header_end = codisRdbExportFindHeaderEnd(p, len);
    if (!header_end) {
        if (len > CODIS_RDB_EXPORT_MAX_HEADER) {
            if (codisRdbExportMoveToMainThread(c))
                return CODIS_RDB_EXPORT_HANDLED;
            codisRdbExportStartErrorResponse(c, 400, "Bad Request");
            return CODIS_RDB_EXPORT_HANDLED;
        }
        return CODIS_RDB_EXPORT_WAIT_MORE;
    }
    if ((size_t)(header_end - p) > CODIS_RDB_EXPORT_MAX_HEADER) {
        if (codisRdbExportMoveToMainThread(c))
            return CODIS_RDB_EXPORT_HANDLED;
        codisRdbExportStartErrorResponse(c, 400, "Bad Request");
        return CODIS_RDB_EXPORT_HANDLED;
    }

    if (codisRdbExportMoveToMainThread(c))
        return CODIS_RDB_EXPORT_HANDLED;

    if (!server.codis_rdb_export_enabled) {
        codisRdbExportStartErrorResponse(c, 404, "Not Found");
        return CODIS_RDB_EXPORT_HANDLED;
    }

    const char *auth_value = NULL;
    size_t auth_len = 0;
    const char *headers = line_end + 2;
    size_t headers_len = header_end - headers - 2;
    if (!codisRdbExportFindHeader(headers, headers_len, CODIS_RDB_EXPORT_AUTH_HEADER,
                                  &auth_value, &auth_len) ||
        !codisRdbExportAuthMatches(auth_value, auth_len))
    {
        codisRdbExportStartErrorResponse(c, 403, "Forbidden");
        return CODIS_RDB_EXPORT_HANDLED;
    }

    codisRdbExportFile file;
    const char *reason = NULL;
    if (codisRdbExportOpenDbfilename(&file, &reason) == C_ERR) {
        serverLog(LL_WARNING,
            "Codis RDB export candidate not available for %s to %s: %s",
            server.rdb_filename ? server.rdb_filename : "?", getClientPeerId(c), reason);
        codisRdbExportStartErrorResponse(c, 404, "Not Found");
        return CODIS_RDB_EXPORT_HANDLED;
    }

    codisRdbExportStartFileResponse(c, &file);
    return CODIS_RDB_EXPORT_HANDLED;
}

void codisRdbExportCleanupClient(client *c) {
    codisRdbExportState *state = c->codis_rdb_export_state;
    if (!state) return;

    if (state->fd != -1) close(state->fd);
    sdsfree(state->filename);
    sdsfree(state->header);
    zfree(state);
    c->codis_rdb_export_state = NULL;
}

static void codisRdbExportWriteHandler(connection *conn) {
    client *c = connGetPrivateData(conn);
    codisRdbExportState *state = c->codis_rdb_export_state;
    ssize_t nwritten;
    ssize_t nread;
    size_t written_in_event = 0;

    if (!state) {
        freeClientAsync(c);
        return;
    }

    while (state->header_sent < sdslen(state->header)) {
        size_t left = sdslen(state->header) - state->header_sent;
        if (codisRdbExportWriteOrDefer(c, state->header + state->header_sent,
                                       left, &nwritten) == C_ERR)
            return;
        state->header_sent += nwritten;
        written_in_event += nwritten;
        if (written_in_event >= NET_MAX_WRITES_PER_EVENT) return;
    }

    if (state->fd == -1) {
        codisRdbExportFinish(c);
        return;
    }

    while (1) {
        if (state->buf_sent == state->buf_len) {
            do {
                nread = read(state->fd, state->buf, sizeof(state->buf));
            } while (nread == -1 && errno == EINTR);

            if (nread == -1) {
                codisRdbExportAbort(c, strerror(errno));
                return;
            }
            if (nread == 0) {
                if (state->sent_file_bytes != state->filesize) {
                    codisRdbExportAbort(c, "short read");
                    return;
                }
                codisRdbExportFinish(c);
                return;
            }
            state->buf_len = nread;
            state->buf_sent = 0;
        }

        size_t left = state->buf_len - state->buf_sent;
        if (codisRdbExportWriteOrDefer(c, state->buf + state->buf_sent,
                                       left, &nwritten) == C_ERR)
            return;
        state->buf_sent += nwritten;
        state->sent_file_bytes += nwritten;
        written_in_event += nwritten;
        if (written_in_event >= NET_MAX_WRITES_PER_EVENT) return;
    }
}
