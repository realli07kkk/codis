#include "server.h"

void codisSlotsBuildHarnessMarker(void) {
}

zskiplist *codisTagIndexCreate(void) {
    return server.codis_enabled ? zslCreate() : NULL;
}

void codisTagIndexFree(zskiplist *index) {
    if (index) zslFree(index);
}

void codisTagIndexReset(redisDb *db) {
    if (!server.codis_enabled) return;
    codisTagIndexFree(db->codis_tagged_keys);
    db->codis_tagged_keys = codisTagIndexCreate();
}

static int codisTagIndexContains(zskiplist *index, sds key, uint32_t crc) {
    if (!index) return 0;

    zrangespec range = {
        .min = (double)crc,
        .max = (double)crc,
        .minex = 0,
        .maxex = 0,
    };
    zskiplistNode *node = zslNthInRange(index, &range, 0, NULL);
    while (node && node->score == (double)crc) {
        if (sdscmp(zslGetNodeElement(node), key) == 0) return 1;
        node = node->level[0].forward;
    }
    return 0;
}

static void codisTagIndexAddNoCheck(redisDb *db, sds key, codisHashInfo info) {
    zslInsert(db->codis_tagged_keys, (double)info.crc, key);
}

void codisTagIndexAdd(redisDb *db, sds key) {
    if (!server.codis_enabled || !db->codis_tagged_keys) return;

    codisHashInfo info = codisHashInfoForKey(key, sdslen(key));
    if (!info.has_tag) return;
    if (codisTagIndexContains(db->codis_tagged_keys, key, info.crc)) return;
    codisTagIndexAddNoCheck(db, key, info);
}

void codisTagIndexDelete(redisDb *db, sds key) {
    if (!server.codis_enabled || !db->codis_tagged_keys) return;

    codisHashInfo info = codisHashInfoForKey(key, sdslen(key));
    if (!info.has_tag) return;
    zslDeleteByScoreAndElement(db->codis_tagged_keys, (double)info.crc, key);
}

void codisTagIndexRebuild(redisDb *db) {
    if (!server.codis_enabled) return;

    codisTagIndexReset(db);
    dictEntry *de;
    kvstoreIterator it;
    kvstoreIteratorInit(&it, db->keys);
    while ((de = kvstoreIteratorNext(&it)) != NULL) {
        kvobj *kv = dictGetKV(de);
        sds key = kvobjGetKey(kv);
        codisHashInfo info = codisHashInfoForKey(key, sdslen(key));
        if (info.has_tag) codisTagIndexAddNoCheck(db, key, info);
    }
    kvstoreIteratorReset(&it);
}

int codisTagIndexAssert(redisDb *db, sds *err) {
    size_t tagged_keys = 0;

    if (!server.codis_enabled) {
        if (err) *err = sdsnew("codis mode is disabled");
        return C_ERR;
    }
    if (!db->codis_tagged_keys) {
        if (err) *err = sdsnew("codis tag index is not initialized");
        return C_ERR;
    }

    dictEntry *de;
    kvstoreIterator it;
    kvstoreIteratorInit(&it, db->keys);
    while ((de = kvstoreIteratorNext(&it)) != NULL) {
        kvobj *kv = dictGetKV(de);
        sds key = kvobjGetKey(kv);
        codisHashInfo info = codisHashInfoForKey(key, sdslen(key));
        if (!info.has_tag) continue;
        tagged_keys++;
        if (!codisTagIndexContains(db->codis_tagged_keys, key, info.crc)) {
            kvstoreIteratorReset(&it);
            if (err) {
                sds keyrepr = sdscatrepr(sdsempty(), key, sdslen(key));
                *err = sdscatprintf(sdsempty(),
                                    "codis tag index is missing tagged key %s (crc=%u, slot=%u)",
                                    keyrepr, info.crc, info.slot);
                sdsfree(keyrepr);
            }
            return C_ERR;
        }
    }
    kvstoreIteratorReset(&it);

    if (db->codis_tagged_keys->length != tagged_keys) {
        if (err) {
            *err = sdscatprintf(sdsempty(),
                                "codis tag index length mismatch: index=%lu keyspace=%zu",
                                db->codis_tagged_keys->length, tagged_keys);
        }
        return C_ERR;
    }
    return C_OK;
}

static int parseSlot(client *c, robj *obj, int *slot) {
    long long val;

    if (getLongLongFromObjectOrReply(c, obj, &val, NULL) != C_OK) return C_ERR;
    if (val < 0 || val >= CODIS_SLOTS) {
        addReplyErrorFormat(c, "invalid slot number = %lld", val);
        return C_ERR;
    }
    *slot = (int)val;
    return C_OK;
}

static int parseSlotCount(client *c, robj *obj, int *count) {
    long long val;

    if (getLongLongFromObjectOrReply(c, obj, &val, NULL) != C_OK) return C_ERR;
    if (val < 0) {
        addReplyErrorFormat(c, "invalid slot count = %lld", val);
        return C_ERR;
    }
    /* Redis 3 Codis clamps overlarge SLOTSINFO count instead of returning an error. */
    *count = val > CODIS_SLOTS ? CODIS_SLOTS : (int)val;
    return C_OK;
}

static dict *codisSlotKeyDict(redisDb *db, int slot) {
    if (slot < 0 || slot >= CODIS_SLOTS) return NULL;
    return kvstoreGetDict(db->keys, slot);
}

static int codisSlotKeyCount(redisDb *db, int slot, unsigned long *count) {
    if (slot < 0 || slot >= CODIS_SLOTS) return C_ERR;
    dict *d = codisSlotKeyDict(db, slot);
    *count = d ? dictSize(d) : 0;
    return C_OK;
}

typedef struct codisSlotScanData {
    list *keys;
    unsigned long sampled;
} codisSlotScanData;

static void codisSlotScanCallback(void *privdata, const dictEntry *de, dictEntryLink plink) {
    UNUSED(plink);
    codisSlotScanData *data = privdata;
    kvobj *kv = dictGetKV(de);
    listAddNodeTail(data->keys, sdsdup(kvobjGetKey(kv)));
    data->sampled++;
}

static void codisSlotCollectKeyCallback(void *privdata, const dictEntry *de, dictEntryLink plink) {
    UNUSED(plink);
    list *keys = privdata;
    kvobj *kv = dictGetKV(de);
    sds key = kvobjGetKey(kv);
    listAddNodeTail(keys, createStringObject(key, sdslen(key)));
}

static void codisDecrRefCountVoid(void *o) {
    decrRefCount(o);
}

void slotshashkeyCommand(client *c) {
    addReplyArrayLen(c, c->argc - 1);
    for (int i = 1; i < c->argc; i++) {
        sds key = c->argv[i]->ptr;
        codisHashInfo info = codisHashInfoForKey(key, sdslen(key));
        addReplyLongLong(c, info.slot);
    }
}

void slotsinfoCommand(client *c) {
    int beg = 0;
    int end = CODIS_SLOTS;
    int count = CODIS_SLOTS;
    int non_empty = 0;

    if (!server.codis_enabled) {
        addReplyError(c, "codis mode is disabled");
        return;
    }
    if (c->argc > 3) {
        addReplyErrorFormat(c, "wrong number of arguments for '%s' command", c->cmd->fullname);
        return;
    }
    if (c->argc >= 2 && parseSlot(c, c->argv[1], &beg) != C_OK) return;
    if (c->argc >= 3 && parseSlotCount(c, c->argv[2], &count) != C_OK) return;
    if (count < CODIS_SLOTS - beg) end = beg + count;

    for (int i = beg; i < end; i++) {
        unsigned long size;
        if (codisSlotKeyCount(c->db, i, &size) != C_OK) continue;
        if (size != 0) non_empty++;
    }

    addReplyArrayLen(c, non_empty);
    for (int i = beg; i < end; i++) {
        unsigned long size;
        if (codisSlotKeyCount(c->db, i, &size) != C_OK) continue;
        if (size == 0) continue;
        addReplyArrayLen(c, 2);
        addReplyLongLong(c, i);
        addReplyLongLong(c, size);
    }
}

void slotsscanCommand(client *c) {
    int slot;
    unsigned long long cursor;
    unsigned long count = 10;

    if (!server.codis_enabled) {
        addReplyError(c, "codis mode is disabled");
        return;
    }
    if (c->argc != 3 && c->argc != 5) {
        addReplyErrorFormat(c, "wrong number of arguments for '%s' command", c->cmd->fullname);
        return;
    }
    if (parseSlot(c, c->argv[1], &slot) != C_OK) return;
    if (parseScanCursorOrReply(c, c->argv[2], &cursor) == C_ERR) return;
    if (c->argc == 5) {
        if (strcasecmp(c->argv[3]->ptr, "count") != 0) {
            addReplyError(c, "syntax error");
            return;
        }
        long parsed_count;
        if (getLongFromObjectOrReply(c, c->argv[4], &parsed_count, NULL) != C_OK) return;
        if (parsed_count < 1) {
            addReplyError(c, "syntax error");
            return;
        }
        count = (unsigned long)parsed_count;
    }

    codisSlotScanData data = {
        .keys = listCreate(),
        .sampled = 0,
    };
    listSetFreeMethod(data.keys, sdsfreegeneric);
    unsigned long maxiterations = count * 10;
    do {
        cursor = kvstoreScan(c->db->keys, cursor, slot, codisSlotScanCallback, NULL, &data);
    } while (cursor != 0 && maxiterations-- && data.sampled < count);

    addReplyArrayLen(c, 2);
    addReplyBulkLongLong(c, cursor);
    addReplyArrayLen(c, listLength(data.keys));
    listNode *node;
    while ((node = listFirst(data.keys)) != NULL) {
        sds key = listNodeValue(node);
        addReplyBulkCBuffer(c, key, sdslen(key));
        listDelNode(data.keys, node);
    }
    listRelease(data.keys);
}

void slotsdelCommand(client *c) {
    if (!server.codis_enabled) {
        addReplyError(c, "codis mode is disabled");
        return;
    }
    if (c->argc < 2) {
        addReplyErrorFormat(c, "wrong number of arguments for '%s' command", c->cmd->fullname);
        return;
    }

    int nslots = c->argc - 1;
    int *slots = zmalloc(sizeof(int) * nslots);
    for (int i = 0; i < nslots; i++) {
        if (parseSlot(c, c->argv[i + 1], &slots[i]) != C_OK) {
            zfree(slots);
            return;
        }
    }

    for (int i = 0; i < nslots; i++) {
        unsigned long long cursor = 0;
        /* Keep the scan callback read-only: collect key object copies for each batch,
         * then delete outside dictScan/kvstoreScan before advancing the cursor. */
        do {
            list *keys = listCreate();
            listSetFreeMethod(keys, codisDecrRefCountVoid);
            cursor = kvstoreScan(c->db->keys, cursor, slots[i], codisSlotCollectKeyCallback, NULL, keys);
            listNode *node;
            while ((node = listFirst(keys)) != NULL) {
                robj *key = listNodeValue(node);
                if (dbSyncDelete(c->db, key)) {
                    keyModified(c, c->db, key, NULL, 1);
                    server.dirty++;
                }
                listDelNode(keys, node);
            }
            listRelease(keys);
        } while (cursor != 0);
    }

    addReplyArrayLen(c, nslots);
    for (int i = 0; i < nslots; i++) {
        unsigned long size = 0;
        if (codisSlotKeyCount(c->db, slots[i], &size) != C_OK) size = 0;
        addReplyArrayLen(c, 2);
        addReplyLongLong(c, slots[i]);
        addReplyLongLong(c, size);
    }
    zfree(slots);
}

void slotscheckCommand(client *c) {
    if (!server.codis_enabled) {
        addReplyError(c, "codis mode is disabled");
        return;
    }
    if (c->argc != 1) {
        addReplyErrorFormat(c, "wrong number of arguments for '%s' command", c->cmd->fullname);
        return;
    }

    dictEntry *de;
    kvstoreIterator it;
    kvstoreIteratorInit(&it, c->db->keys);
    while ((de = kvstoreIteratorNext(&it)) != NULL) {
        int slot = kvstoreIteratorGetCurrentDictIndex(&it);
        kvobj *kv = dictGetKV(de);
        sds key = kvobjGetKey(kv);
        codisHashInfo info = codisHashInfoForKey(key, sdslen(key));
        if (slot != (int)info.slot) {
            kvstoreIteratorReset(&it);
            sds keyrepr = sdscatrepr(sdsempty(), key, sdslen(key));
            addReplyErrorFormat(c, "codis slot keyspace mismatch: key %s is in slot %d, expected %u",
                                keyrepr, slot, info.slot);
            sdsfree(keyrepr);
            return;
        }
    }
    kvstoreIteratorReset(&it);

    sds err = NULL;
    if (codisTagIndexAssert(c->db, &err) != C_OK) {
        addReplyErrorFormat(c, "codis tag index check failed: %s", err ? err : "unknown error");
        sdsfree(err);
        return;
    }
    addReply(c, shared.ok);
}

/* ============================ Sync Migration: Helpers ============================ */

static int parseInt(client *c, robj *obj, int *p) {
    long long v;
    if (getLongLongFromObjectOrReply(c, obj, &v, NULL) != C_OK) return -1;
    if (v < INT_MIN || v > INT_MAX) {
        addReplyError(c, "value is out of range");
        return -1;
    }
    *p = (int)v;
    return 0;
}

static int parseTimeout(client *c, robj *obj, int *p) {
    int v;
    if (parseInt(c, obj, &v) != 0) return -1;
    if (v < 0) {
        addReplyErrorFormat(c, "invalid timeout = %d", v);
        return -1;
    }
    *p = (v == 0) ? 100 : v;
    return 0;
}

static int parsePort(client *c, robj *obj, int *p) {
    long port;
    if (getRangeLongFromObjectOrReply(c, obj, 0, 65535, &port, "invalid port") != C_OK) {
        return -1;
    }
    *p = (int)port;
    return 0;
}

/* ============================ Sync Migration: Socket Cache ============================ */

#define SLOTSMGRT_SOCKET_CACHE_ITEMS 64
#define SLOTSMGRT_SOCKET_CACHE_TTL 15

static sds slotsmgrtSocketName(sds host, int port) {
    sds name = sdsempty();
    name = sdscatlen(name, host, sdslen(host));
    return sdscatprintf(name, ":%d", port);
}

static void slotsmgrtFreeSockfd(slotsmgrt_sockfd *pfd) {
    close(pfd->fd);
    zfree(pfd);
}

static void slotsmgrtEvictOldestSocket(void) {
    dictIterator *di = dictGetIterator(server.slotsmgrt_cached_sockets);
    dictEntry *de, *oldest = NULL;
    time_t oldest_time = 0;

    while ((de = dictNext(di)) != NULL) {
        slotsmgrt_sockfd *pfd = dictGetVal(de);
        if (oldest == NULL || pfd->lasttime < oldest_time) {
            oldest = de;
            oldest_time = pfd->lasttime;
        }
    }
    dictReleaseIterator(di);

    if (oldest != NULL) {
        slotsmgrt_sockfd *pfd = dictGetVal(oldest);
        slotsmgrtFreeSockfd(pfd);
        dictDelete(server.slotsmgrt_cached_sockets, dictGetKey(oldest));
    }
}

static slotsmgrt_sockfd *slotsmgrtGetSockfd(client *c, sds host, int port, int timeout) {
    sds name = slotsmgrtSocketName(host, port);

    slotsmgrt_sockfd *pfd = dictFetchValue(server.slotsmgrt_cached_sockets, name);
    if (pfd != NULL) {
        sdsfree(name);
        pfd->lasttime = server.unixtime;
        return pfd;
    }

    if (dictSize(server.slotsmgrt_cached_sockets) >= SLOTSMGRT_SOCKET_CACHE_ITEMS) {
        slotsmgrtEvictOldestSocket();
    }

    int fd = anetTcpNonBlockConnect(server.neterr, host, port);
    if (fd == -1) {
        serverLog(LL_WARNING, "slotsmgrt: connect to target %s:%d, error = '%s'",
                host, port, server.neterr);
        sdsfree(name);
        addReplyErrorFormat(c,"Can't connect to target node: %s", server.neterr);
        return NULL;
    }
    anetEnableTcpNoDelay(server.neterr, fd);
    if ((aeWait(fd, AE_WRITABLE, timeout) & AE_WRITABLE) == 0) {
        serverLog(LL_WARNING, "slotsmgrt: connect to target %s:%d, aewait error = '%s'",
                host, port, server.neterr);
        sdsfree(name);
        close(fd);
        addReplySds(c, sdsnew("-IOERR error or timeout connecting to the client\r\n"));
        return NULL;
    }
    serverLog(LL_WARNING, "slotsmgrt: connect to target %s:%d", host, port);

    pfd = zmalloc(sizeof(*pfd));
    pfd->fd = fd;
    pfd->db = -1;
    pfd->authorized = (server.requirepass == NULL && server.codis_migration_auth_pass == NULL) ? 1 : 0;
    pfd->lasttime = server.unixtime;
    dictAdd(server.slotsmgrt_cached_sockets, name, pfd);
    return pfd;
}

static void slotsmgrtCloseSocket(sds host, int port) {
    sds name = slotsmgrtSocketName(host, port);

    slotsmgrt_sockfd *pfd = dictFetchValue(server.slotsmgrt_cached_sockets, name);
    if (pfd == NULL) {
        serverLog(LL_WARNING, "slotsmgrt: close target %s:%d again", host, port);
        sdsfree(name);
        return;
    }
    serverLog(LL_WARNING, "slotsmgrt: close target %s:%d", host, port);
    dictDelete(server.slotsmgrt_cached_sockets, name);
    slotsmgrtFreeSockfd(pfd);
    sdsfree(name);
}

void slotsmgrt_cleanup(void) {
    dictIterator *di = dictGetSafeIterator(server.slotsmgrt_cached_sockets);
    dictEntry *de;
    while ((de = dictNext(di)) != NULL) {
        slotsmgrt_sockfd *pfd = dictGetVal(de);
        if ((server.unixtime - pfd->lasttime) > SLOTSMGRT_SOCKET_CACHE_TTL) {
            serverLog(LL_WARNING, "slotsmgrt: timeout target %s, lasttime = %ld, now = %ld",
                   (char *)dictGetKey(de), pfd->lasttime, server.unixtime);
            dictDelete(server.slotsmgrt_cached_sockets, dictGetKey(de));
            slotsmgrtFreeSockfd(pfd);
        }
    }
    dictReleaseIterator(di);
}

/* ============================ Sync Migration: Core ================================== */

/* Build a SLOTSRESTORE RESP command with AUTH/SELECT prefix and dump payloads,
 * write it to the cached socket, and read back responses.
 * Returns 0 on success, -1 on error (reply already sent to client). */
static int slotsmgrt(client *c, sds host, int port, slotsmgrt_sockfd *pfd,
                     int db, int timeout, robj *keys[], kvobj *vals[], int n) {
    rio cmd;
    rioInitWithBuffer(&cmd, sdsempty());

    char *auth_user = server.codis_migration_auth_user;
    char *auth_pass = server.codis_migration_auth_pass;
    if (auth_pass == NULL) {
        auth_user = NULL;
        auth_pass = server.requirepass;
    }

    int needauth = 0;
    if (pfd->authorized == 0 && auth_pass != NULL) {
        needauth = 1;
        serverAssertWithInfo(c, NULL, rioWriteBulkCount(&cmd, '*', auth_user != NULL ? 3 : 2));
        serverAssertWithInfo(c, NULL, rioWriteBulkString(&cmd, "AUTH", 4));
        if (auth_user != NULL) {
            serverAssertWithInfo(c, NULL, rioWriteBulkString(&cmd, auth_user, strlen(auth_user)));
        }
        serverAssertWithInfo(c, NULL, rioWriteBulkString(&cmd, auth_pass, strlen(auth_pass)));
    }

    int selectdb = 0;
    if (pfd->db != db) {
        selectdb = 1;
        serverAssertWithInfo(c, NULL, rioWriteBulkCount(&cmd, '*', 2));
        serverAssertWithInfo(c, NULL, rioWriteBulkString(&cmd, "SELECT", 6));
        serverAssertWithInfo(c, NULL, rioWriteBulkLongLong(&cmd, db));
    }

    serverAssertWithInfo(c, NULL, rioWriteBulkCount(&cmd, '*', 1 + 3 * n));
    serverAssertWithInfo(c, NULL, rioWriteBulkString(&cmd, "SLOTSRESTORE", 12));

    sds onekey = NULL;
    for (int i = 0; i < n; i++) {
        robj *key = keys[i];
        kvobj *val = vals[i];
        long long ttl = 0, expireat = getExpire(c->db, key->ptr, val);
        if (expireat != -1) {
            ttl = expireat - mstime();
            if (ttl < 1) {
                ttl = 1;
            }
        }
        sds skey = key->ptr;
        serverAssertWithInfo(c, NULL, rioWriteBulkString(&cmd, skey, sdslen(skey)));
        serverAssertWithInfo(c, NULL, rioWriteBulkLongLong(&cmd, ttl));
        {
            rio pld;
            createDumpPayload(&pld, val, key, c->db->id, 0);
            sds buf = pld.io.buffer.ptr;
            serverAssertWithInfo(c, NULL, rioWriteBulkString(&cmd, buf, sdslen(buf)));
            sdsfree(buf);
        }
        if (onekey == NULL) {
            onekey = skey;
        }
    }

    {
        sds buf = cmd.io.buffer.ptr;
        size_t pos = 0, towrite;
        int nwritten = 0;
        while ((towrite = sdslen(buf) - pos) > 0) {
            towrite = (towrite > (64 * 1024) ? (64 * 1024) : towrite);
            nwritten = syncWrite(pfd->fd, buf + pos, towrite, timeout);
            if (nwritten != (signed)towrite) {
                serverLog(LL_WARNING, "slotsmgrt: writing to target %s:%d, error '%s', "
                        "nkeys = %d, onekey = '%s', cmd.len = %ld, pos = %ld, towrite = %ld",
                        host, port, server.neterr, n, onekey, sdslen(buf), pos, towrite);
                addReplySds(c, sdsnew("-IOERR error or timeout writing to target\r\n"));
                sdsfree(buf);
                return -1;
            }
            pos += nwritten;
        }
        sdsfree(buf);
    }

    {
        char rbuf[1024];
        if (needauth) {
            if (syncReadLine(pfd->fd, rbuf, sizeof(rbuf), timeout) <= 0) {
                serverLog(LL_WARNING, "slotsmgrt: auth failed, reading from target %s:%d: nkeys = %d, onekey = '%s', error = '%s'",
                        host, port, n, onekey, server.neterr);
                addReplySds(c, sdsnew("-IOERR error or timeout reading from target\r\n"));
                return -1;
            }
            if (rbuf[0] != '+') {
                serverLog(LL_WARNING, "slotsmgrt: auth failed, reading from target %s:%d: nkeys = %d, onekey = '%s', response = '%s'",
                        host, port, n, onekey, rbuf);
                addReplyError(c, "error on slotsrestore, auth failed");
                return -1;
            }
            pfd->authorized = 1;
        }

        if (selectdb) {
            if (syncReadLine(pfd->fd, rbuf, sizeof(rbuf), timeout) <= 0) {
                serverLog(LL_WARNING, "slotsmgrt: select failed, reading from target %s:%d: nkeys = %d, onekey = '%s', error = '%s'",
                        host, port, n, onekey, server.neterr);
                addReplySds(c, sdsnew("-IOERR error or timeout reading from target\r\n"));
                return -1;
            }
            if (rbuf[0] != '+') {
                serverLog(LL_WARNING, "slotsmgrt: select failed, reading from target %s:%d: nkeys = %d, onekey = '%s', response = '%s'",
                        host, port, n, onekey, rbuf);
                addReplyError(c, "error on slotsrestore, select failed");
                return -1;
            }
            pfd->db = db;
        }

        if (syncReadLine(pfd->fd, rbuf, sizeof(rbuf), timeout) <= 0) {
            serverLog(LL_WARNING, "slotsmgrt: migration failed, reading from target %s:%d: nkeys = %d, onekey = '%s', error = '%s'",
                    host, port, n, onekey, server.neterr);
            addReplySds(c, sdsnew("-IOERR error or timeout reading from target\r\n"));
            return -1;
        }
        if (rbuf[0] == '-') {
            serverLog(LL_WARNING, "slotsmgrt: migration failed, reading from target %s:%d: nkeys = %d, onekey = '%s', response = '%s'",
                    host, port, n, onekey, rbuf);
            addReplyError(c, "error on slotsrestore, migration failed");
            return -1;
        }
    }

    pfd->lasttime = server.unixtime;

    serverLog(LL_VERBOSE, "slotsmgrt: migrate to %s:%d, nkeys = %d, onekey = '%s'", host, port, n, onekey);
    return 0;
}

static void slotsmgrtRemoveKeys(client *c, robj **keys, int n, int rewrite) {
    robj **propargv = rewrite ? zmalloc(sizeof(robj *) * (n + 1)) : NULL;
    int propargc = 0;

    if (propargv != NULL) {
        propargv[propargc++] = shared.del;
    }

    for (int i = 0; i < n; i++) {
        if (dbSyncDelete(c->db, keys[i])) {
            keyModified(c, c->db, keys[i], NULL, 1);
            server.dirty++;
            if (propargv != NULL) propargv[propargc++] = keys[i];
        }
    }

    if (propargc > 1) {
        alsoPropagate(c->db->id, propargv, propargc, PROPAGATE_AOF | PROPAGATE_REPL);
        preventCommandPropagation(c);
    }
    zfree(propargv);
}

/* Migrate a single key. Returns -1 on error, >=0 for count migrated. */
static int slotsmgrtOneKey(client *c, sds host, int port, int timeout, robj *key) {
    slotsmgrt_sockfd *pfd = slotsmgrtGetSockfd(c, host, port, timeout);
    if (pfd == NULL) {
        return -1;
    }

    kvobj *val = lookupKeyWrite(c->db, key);
    if (val == NULL) {
        return 0;
    }
    robj *keys[] = {key};
    kvobj *vals[] = {val};
    if (slotsmgrt(c, host, port, pfd, c->db->id, timeout, keys, vals, 1) != 0) {
        slotsmgrtCloseSocket(host, port);
        return -1;
    }
    slotsmgrtRemoveKeys(c, keys, 1, 1);
    return 1;
}

/* ============================ Sync Migration: Tag-Aware ============================ */

/* Migrate a key with hash-tag awareness. If the key has a hash tag, all keys
 * sharing the same CRC are migrated together. Returns -1 on error, >=0 for
 * number of keys migrated. */
static int slotsmgrtTagCommand(client *c, sds host, int port, int timeout, robj *key) {
    sds key_sds = key->ptr;
    codisHashInfo info = codisHashInfoForKey(key_sds, sdslen(key_sds));
    if (!info.has_tag) {
        return slotsmgrtOneKey(c, host, port, timeout, key);
    }

    slotsmgrt_sockfd *pfd = slotsmgrtGetSockfd(c, host, port, timeout);
    if (pfd == NULL) return -1;

    int slot = (int)info.slot;
    dict *d = kvstoreGetDict(c->db->keys, slot);
    if (d == NULL || dictSize(d) == 0) return 0;

    zrangespec range;
    range.min = (double)info.crc;
    range.minex = 0;
    range.max = (double)info.crc;
    range.maxex = 0;

    list *l = listCreate();
    listSetFreeMethod(l, codisDecrRefCountVoid);

    zskiplistNode *node = zslNthInRange(c->db->codis_tagged_keys, &range, 0, NULL);
    while (node != NULL && node->score == (double)info.crc) {
        sds tagged_sds = zslGetNodeElement(node);
        robj *tagged_key = createStringObject(tagged_sds, sdslen(tagged_sds));
        listAddNodeTail(l, tagged_key);
        node = node->level[0].forward;
    }

    int max = (int)listLength(l);
    if (max == 0) {
        listRelease(l);
        return 0;
    }

    robj **keys = zmalloc(sizeof(robj *) * max);
    kvobj **vals = zmalloc(sizeof(kvobj *) * max);

    int n = 0;
    for (int i = 0; i < max; i++) {
        listNode *head = listFirst(l);
        robj *tagged_key = listNodeValue(head);
        kvobj *val = lookupKeyWrite(c->db, tagged_key);
        if (val != NULL) {
            keys[n] = tagged_key;
            vals[n] = val;
            n++;
            incrRefCount(tagged_key);
            incrRefCount(val);
        }
        listDelNode(l, head);
    }

    int ret = 0;
    if (n != 0) {
        if (slotsmgrt(c, host, port, pfd, c->db->id, timeout, keys, vals, n) != 0) {
            slotsmgrtCloseSocket(host, port);
            ret = -1;
        } else {
            slotsmgrtRemoveKeys(c, keys, n, 1);
            ret = n;
        }
    }

    listRelease(l);
    for (int i = 0; i < n; i++) {
        decrRefCount(keys[i]);
        decrRefCount(vals[i]);
    }
    zfree(keys);
    zfree(vals);
    return ret;
}

/* ============================ Sync Migration Commands ============================== */

typedef int (*slotsmgrtMigrateProc)(client *c, sds host, int port, int timeout, robj *key);

static void slotsmgrtKeyCommand(client *c, slotsmgrtMigrateProc migrate) {
    sds host = c->argv[1]->ptr;
    int port, timeout;
    if (parsePort(c, c->argv[2], &port) != 0) return;
    if (parseTimeout(c, c->argv[3], &timeout) != 0) return;

    int succ = migrate(c, host, port, timeout, c->argv[4]);
    if (succ < 0) return;
    addReplyLongLong(c, succ);
}

static void slotsmgrtSlotCommand(client *c, slotsmgrtMigrateProc migrate) {
    sds host = c->argv[1]->ptr;
    int port, timeout, slot;
    if (parsePort(c, c->argv[2], &port) != 0) return;
    if (parseTimeout(c, c->argv[3], &timeout) != 0) return;
    if (parseSlot(c, c->argv[4], &slot) != 0) return;

    dict *d = kvstoreGetDict(c->db->keys, slot);
    int succ = 0;
    const dictEntry *de = d ? dictGetRandomKey(d) : NULL;
    if (de != NULL) {
        kvobj *kv = dictGetKV(de);
        sds skey = kvobjGetKey(kv);
        robj *key = createStringObject(skey, sdslen(skey));
        succ = migrate(c, host, port, timeout, key);
        decrRefCount(key);
        if (succ < 0) return;
    }
    addReplyArrayLen(c, 2);
    addReplyLongLong(c, succ);
    addReplyLongLong(c, d ? (int)dictSize(d) : 0);
}

/* SLOTSMGRTSLOT host port timeout slot */
void slotsmgrtslotCommand(client *c) {
    slotsmgrtSlotCommand(c, slotsmgrtOneKey);
}

/* SLOTSMGRTONE host port timeout key */
void slotsmgrtoneCommand(client *c) {
    slotsmgrtKeyCommand(c, slotsmgrtOneKey);
}

/* SLOTSMGRTTAGSLOT host port timeout slot */
void slotsmgrttagslotCommand(client *c) {
    slotsmgrtSlotCommand(c, slotsmgrtTagCommand);
}

/* SLOTSMGRTTAGONE host port timeout key */
void slotsmgrttagoneCommand(client *c) {
    slotsmgrtKeyCommand(c, slotsmgrtTagCommand);
}

/* SLOTSRESTORE key ttlms payload [key ttlms payload ...] */
void slotsrestoreCommand(client *c) {
    if (c->argc < 4 || (c->argc - 1) % 3 != 0) {
        addReplyErrorFormat(c, "wrong number of arguments for 'slotsrestore' command");
        return;
    }
    int n = (c->argc - 1) / 3;

    long long *ttls = zmalloc(sizeof(long long) * n);
    robj **vals = zmalloc(sizeof(robj *) * n);
    KeyMetaSpec *metas = zmalloc(sizeof(KeyMetaSpec) * n);
    for (int i = 0; i < n; i++) {
        vals[i] = NULL;
        keyMetaSpecInit(&metas[i]);
    }

    for (int i = 0; i < n; i++) {
        robj *key = c->argv[i * 3 + 1];
        robj *ttl = c->argv[i * 3 + 2];
        robj *val = c->argv[i * 3 + 3];
        if (lookupKeyWrite(c->db, key) != NULL) {
            serverLog(LL_WARNING, "slotsrestore: key = '%s' already exists",
                    (char *)key->ptr);
        }
        if (getLongLongFromObjectOrReply(c, ttl, &ttls[i], NULL) != C_OK) {
            goto cleanup;
        } else if (ttls[i] < 0) {
            addReplyError(c, "invalid ttl value, must be >= 0");
            goto cleanup;
        }
        if (ttls[i] != 0) {
            keyMetaSpecAdd(&metas[i], KEY_META_ID_EXPIRE, commandTimeSnapshot() + ttls[i]);
        }
        rio payload;
        if (verifyDumpPayload(val->ptr, sdslen(val->ptr), NULL) != C_OK) {
            addReplyError(c, "dump payload version or checksum are wrong");
            goto cleanup;
        }
        int type;
        rioInitWithBuffer(&payload, val->ptr);
        type = rdbLoadType(&payload);
        if (rdbResolveKeyType(&payload, &type, c->db->id, &metas[i]) == -1 ||
                ((vals[i] = rdbLoadObject(type, &payload, key->ptr, c->db->id, NULL)) == NULL)) {
            addReplyError(c, "bad data format");
            goto cleanup;
        }
    }

    for (int i = 0; i < n; i++) {
        robj *key = c->argv[i * 3 + 1];
        robj *val = vals[i];
        dbDelete(c->db, key);
        kvobj *kv = dbAddInternal(c->db, key, &val, NULL, &metas[i]);
        /* After dbAdd, val points to the stored kvobj; vals[i] points to the old
         * robj whose reference has been consumed by kvobjSet. Mark as consumed. */
        vals[i] = NULL;

        if (kv->type == OBJ_HASH) {
            uint64_t minExpiredField = hashTypeGetMinExpire(kv, 1);
            if (minExpiredField != EB_EXPIRE_TIME_INVALID)
                estoreAdd(c->db->subexpires, getKeySlot(key->ptr), kv, minExpiredField);
        }

        if (kv->type == OBJ_STREAM) {
            stream *s = kv->ptr;
            if (s->idmp_producers != NULL) {
                if (dictAdd(c->db->stream_idmp_keys, key, NULL) == DICT_OK)
                    incrRefCount(key);
            }
        }

        metas[i].numMeta = 0;
        metas[i].metabits = 0;
        keyModified(c, c->db, key, NULL, 1);
        server.dirty++;
    }
    addReply(c, shared.ok);

cleanup:
    for (int i = 0; i < n; i++) {
        if (vals[i] != NULL) decrRefCount(vals[i]);
        keyMetaSpecCleanup(&metas[i]);
    }
    zfree(metas);
    zfree(vals);
    zfree(ttls);
}
