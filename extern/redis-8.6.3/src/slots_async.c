#include "server.h"

#define SLOTSMGRT_ASYNC_DEFAULT_TIMEOUT_MS (1000 * 30)
#define SLOTSMGRT_ASYNC_DEFAULT_MAXBULKS 200
#define SLOTSMGRT_ASYNC_DUMP_DEFAULT_MAXBULKS 1000
#define SLOTSMGRT_ASYNC_MAX_MAXBULKS (512LL * 1024)
#define SLOTSMGRT_ASYNC_DEFAULT_MAXBYTES (512LL * 1024)
#define SLOTSMGRT_ASYNC_MAX_MAXBYTES (INT_MAX / 2)
#define SLOTSMGRT_ASYNC_IDLE_TIMEOUT_MS (1000LL * 60)

struct batchedObjectIterator {
    dict *keys;
    dictIterator *di;
    list *removed_keys;
    long long timeout;
    long long maxbulks;
    long long maxbytes;
    long long emitted_msgs;
    long long migrated;
    int use_slot;
    int slot;
};

static void slotsmgrtAsyncDecrRefCountVoid(void *o) {
    decrRefCount(o);
}

static batchedObjectIterator *createBatchedObjectIterator(long long timeout, long long maxbulks, long long maxbytes) {
    batchedObjectIterator *it = zcalloc(sizeof(*it));
    it->keys = dictCreate(&objectKeyPointerValueDictType);
    it->removed_keys = listCreate();
    listSetFreeMethod(it->removed_keys, slotsmgrtAsyncDecrRefCountVoid);
    it->timeout = timeout;
    it->maxbulks = maxbulks;
    it->maxbytes = maxbytes;
    it->slot = -1;
    return it;
}

static void freeBatchedObjectIterator(batchedObjectIterator *it) {
    if (it == NULL) return;
    if (it->di != NULL) dictReleaseIterator(it->di);
    if (it->keys != NULL) dictRelease(it->keys);
    if (it->removed_keys != NULL) listRelease(it->removed_keys);
    zfree(it);
}

static int batchedObjectIteratorAddDirect(batchedObjectIterator *it, robj *key) {
    incrRefCount(key);
    if (dictAdd(it->keys, key, NULL) == DICT_OK) return 1;
    decrRefCount(key);
    return 0;
}

static int batchedObjectIteratorAddSds(batchedObjectIterator *it, sds key) {
    robj *keyobj = createStringObject(key, sdslen(key));
    if (dictAdd(it->keys, keyobj, NULL) == DICT_OK) return 1;
    decrRefCount(keyobj);
    return 0;
}

static int batchedObjectIteratorAddTagged(redisDb *db, batchedObjectIterator *it, robj *key) {
    sds key_sds = key->ptr;
    codisHashInfo info = codisHashInfoForKey(key_sds, sdslen(key_sds));
    if (!info.has_tag || db->codis_tagged_keys == NULL) {
        return batchedObjectIteratorAddDirect(it, key);
    }

    int added = 0;
    zrangespec range = {
        .min = (double)info.crc,
        .max = (double)info.crc,
        .minex = 0,
        .maxex = 0,
    };
    zskiplistNode *node = zslNthInRange(db->codis_tagged_keys, &range, 0, NULL);
    while (node != NULL && node->score == (double)info.crc) {
        added += batchedObjectIteratorAddSds(it, zslGetNodeElement(node));
        node = node->level[0].forward;
    }
    return added;
}

static int batchedObjectIteratorContains(batchedObjectIterator *it, robj *key, int usetag) {
    if (dictFind(it->keys, key) != NULL) return 1;
    if (!usetag) return 0;

    sds key_sds = key->ptr;
    codisHashInfo info = codisHashInfoForKey(key_sds, sdslen(key_sds));
    if (!info.has_tag) return 0;

    dictIterator *di = dictGetIterator(it->keys);
    dictEntry *de;
    while ((de = dictNext(di)) != NULL) {
        robj *migrating = dictGetKey(de);
        sds migrating_sds = migrating->ptr;
        codisHashInfo migrating_info = codisHashInfoForKey(migrating_sds, sdslen(migrating_sds));
        if (migrating_info.has_tag && migrating_info.crc == info.crc) {
            dictReleaseIterator(di);
            return 1;
        }
    }
    dictReleaseIterator(di);
    return 0;
}

static int batchedObjectIteratorHasKeys(batchedObjectIterator *it) {
    return dictSize(it->keys) != 0;
}

static unsigned long batchedObjectIteratorKeyCount(batchedObjectIterator *it) {
    return dictSize(it->keys);
}

static void batchedObjectIteratorResetScan(batchedObjectIterator *it) {
    if (it->di != NULL) dictReleaseIterator(it->di);
    it->di = dictGetIterator(it->keys);
    it->emitted_msgs = 0;
}

static void addReplySlotsrestoreAsyncObjectCommand(client *c, robj *key, kvobj *val) {
    long long ttl = 0;
    long long expireat = getExpire(c->db, key->ptr, val);
    if (expireat != -1) {
        ttl = expireat - mstime();
        if (ttl < 1) ttl = 1;
    }

    rio payload;
    createDumpPayload(&payload, val, key, c->db->id, 0);
    sds buf = payload.io.buffer.ptr;

    addReplyArrayLen(c, 5);
    addReplyBulkCString(c, "SLOTSRESTORE-ASYNC");
    addReplyBulkCString(c, "object");
    addReplyBulk(c, key);
    addReplyBulkLongLong(c, ttl);
    addReplyBulkCBuffer(c, buf, sdslen(buf));

    sdsfree(buf);
}

static void addReplySlotsrestoreAsyncDeleteCommand(client *c, robj *key) {
    addReplyArrayLen(c, 3);
    addReplyBulkCString(c, "SLOTSRESTORE-ASYNC");
    addReplyBulkCString(c, "delete");
    addReplyBulk(c, key);
}

static int batchedObjectIteratorNext(client *c, batchedObjectIterator *it) {
    if (it->di == NULL) batchedObjectIteratorResetScan(it);
    if (it->maxbulks > 0 && it->emitted_msgs >= it->maxbulks) return 0;

    dictEntry *de;
    while ((de = dictNext(it->di)) != NULL) {
        robj *key = dictGetKey(de);
        kvobj *val = lookupKeyReadWithFlags(c->db, key, LOOKUP_NOTOUCH);
        if (val == NULL) continue;
        addReplySlotsrestoreAsyncDeleteCommand(c, key);
        addReplySlotsrestoreAsyncObjectCommand(c, key, val);
        incrRefCount(key);
        listAddNodeTail(it->removed_keys, key);
        it->emitted_msgs++;
        return 2;
    }
    return 0;
}

static int batchedObjectIteratorReplyAll(client *c, batchedObjectIterator *it) {
    int total = 0;
    batchedObjectIteratorResetScan(it);
    int emitted;
    while ((emitted = batchedObjectIteratorNext(c, it)) != 0) total += emitted;
    return total;
}

static slotsmgrtAsyncClient *getSlotsmgrtAsyncClient(int db) {
    serverAssert(db >= 0 && db < server.dbnum);
    return &server.slotsmgrt_cached_clients[db];
}

static int slotsmgrtAsyncClientIsActive(slotsmgrtAsyncClient *ac) {
    return ac->c != NULL && ac->batched_iter != NULL;
}

static void notifySlotsmgrtAsyncClient(slotsmgrtAsyncClient *ac, const char *errmsg) {
    if (ac->blocked_list == NULL) return;

    /* The first success reply reports migration counts from ac->batched_iter.
     * Callers must keep that iterator attached until this function returns. */
    int first = 1;
    while (listLength(ac->blocked_list) != 0) {
        listNode *head = listFirst(ac->blocked_list);
        client *blocked = listNodeValue(head);

        if (errmsg != NULL) {
            addReplyError(blocked, errmsg);
        } else if (first && ac->batched_iter != NULL) {
            batchedObjectIterator *it = ac->batched_iter;
            if (it->use_slot) {
                addReplyArrayLen(blocked, 2);
                addReplyLongLong(blocked, it->migrated);
                addReplyLongLong(blocked, kvstoreDictSize(ac->c->db->keys, it->slot));
            } else {
                addReplyLongLong(blocked, it->migrated);
            }
        } else {
            addReply(blocked, shared.ok);
        }
        first = 0;
        updateStatsOnUnblock(blocked, 0, 0, errmsg != NULL);

        blocked->slotsmgrt_flags &= ~CLIENT_SLOTSMGRT_ASYNC_NORMAL_CLIENT;
        blocked->slotsmgrt_fenceq = NULL;
        listDelNode(ac->blocked_list, head);
        if (blocked->flags & CLIENT_BLOCKED) unblockClient(blocked, 1);
    }
}

static void unlinkSlotsmgrtAsyncCachedClient(client *c, const char *errmsg) {
    slotsmgrtAsyncClient *ac = getSlotsmgrtAsyncClient(c->db->id);
    serverAssert(c->slotsmgrt_flags & CLIENT_SLOTSMGRT_ASYNC_CACHED_CLIENT);
    serverAssert(ac->c == c);

    notifySlotsmgrtAsyncClient(ac, errmsg);

    serverLog(LL_WARNING,
              "slotsmgrt_async: unlink client %s:%d (DB=%d): used=%d, sending_msgs=%ld, blocked_clients=%lu (%s)",
              ac->host ? ac->host : "?", ac->port, c->db->id, ac->used,
              ac->sending_msgs, ac->blocked_list ? listLength(ac->blocked_list) : 0,
              errmsg ? errmsg : "done");

    sdsfree(ac->host);
    freeBatchedObjectIterator(ac->batched_iter);
    if (ac->blocked_list != NULL) listRelease(ac->blocked_list);
    c->slotsmgrt_flags &= ~CLIENT_SLOTSMGRT_ASYNC_CACHED_CLIENT;
    memset(ac, 0, sizeof(*ac));
}

static int releaseSlotsmgrtAsyncClient(int db, const char *errmsg) {
    slotsmgrtAsyncClient *ac = getSlotsmgrtAsyncClient(db);
    if (ac->c == NULL) return 0;

    client *cached = ac->c;
    unlinkSlotsmgrtAsyncCachedClient(cached, errmsg);
    freeClient(cached);
    return 1;
}

static int createSlotsmgrtAsyncClient(int db, sds host, int port, long long timeout) {
    connection *conn = connCreate(server.el, connectionTypeTcp());
    if (conn == NULL) {
        serverLog(LL_WARNING, "slotsmgrt_async: create TCP connection object failed");
        return C_ERR;
    }

    if (connBlockingConnect(conn, host, port, timeout) == C_ERR) {
        serverLog(LL_WARNING, "slotsmgrt_async: connect %s:%d (DB=%d) failed: %s",
                  host, port, db, connGetLastError(conn));
        connClose(conn);
        return C_ERR;
    }

    client *cached = createClient(conn);
    if (selectDb(cached, db) != C_OK) {
        serverLog(LL_WARNING, "slotsmgrt_async: invalid DB index (DB=%d)", db);
        freeClient(cached);
        return C_ERR;
    }

    cached->slotsmgrt_flags |= CLIENT_SLOTSMGRT_ASYNC_CACHED_CLIENT;
    cached->flags |= CLIENT_INTERNAL;
    cached->authenticated = 1;
    cached->user = NULL;

    releaseSlotsmgrtAsyncClient(db, "interrupted: build new connection");

    slotsmgrtAsyncClient *ac = getSlotsmgrtAsyncClient(db);
    ac->c = cached;
    ac->used = 0;
    ac->host = sdsdup(host);
    ac->port = port;
    ac->timeout = timeout;
    ac->lastuse = mstime();
    ac->sending_msgs = 0;
    ac->batched_iter = NULL;
    ac->blocked_list = listCreate();

    serverLog(LL_WARNING, "slotsmgrt_async: create client %s:%d (DB=%d) OK", host, port, db);
    return C_OK;
}

static slotsmgrtAsyncClient *getOrCreateSlotsmgrtAsyncClient(int db, sds host, int port, long long timeout) {
    slotsmgrtAsyncClient *ac = getSlotsmgrtAsyncClient(db);
    if (ac->c != NULL && ac->port == port && sdscmp(ac->host, host) == 0) {
        ac->timeout = timeout;
        ac->lastuse = mstime();
        return ac;
    }
    return createSlotsmgrtAsyncClient(db, host, port, timeout) == C_OK ? getSlotsmgrtAsyncClient(db) : NULL;
}

static void unlinkSlotsmgrtAsyncNormalClient(client *c) {
    serverAssert(c->slotsmgrt_flags & CLIENT_SLOTSMGRT_ASYNC_NORMAL_CLIENT);
    if (c->slotsmgrt_fenceq != NULL) {
        listNode *node = listSearchKey(c->slotsmgrt_fenceq, c);
        if (node != NULL) listDelNode(c->slotsmgrt_fenceq, node);
    }
    c->slotsmgrt_flags &= ~CLIENT_SLOTSMGRT_ASYNC_NORMAL_CLIENT;
    c->slotsmgrt_fenceq = NULL;
}

static int getSlotsmgrtAsyncClientMigrationStatusOrBlock(client *c, robj *key, int block) {
    slotsmgrtAsyncClient *ac = getSlotsmgrtAsyncClient(c->db->id);
    if (!slotsmgrtAsyncClientIsActive(ac)) return 0;
    if (key != NULL && ac->batched_iter != NULL) {
        /* Wrapper checks are intentionally tag-aware so writes to co-tagged
         * keys are fenced while a tagged key from the group is migrating. */
        if (!batchedObjectIteratorContains(ac->batched_iter, key, 1)) return 0;
    }
    if (!block) return 1;

    if (c->slotsmgrt_flags & CLIENT_SLOTSMGRT_ASYNC_NORMAL_CLIENT) return -1;
    if (ac->blocked_list == NULL) ac->blocked_list = listCreate();

    c->slotsmgrt_flags |= CLIENT_SLOTSMGRT_ASYNC_NORMAL_CLIENT;
    c->slotsmgrt_fenceq = ac->blocked_list;
    listAddNodeTail(ac->blocked_list, c);
    blockClient(c, BLOCKED_SLOTSMGRT);
    return 1;
}

static int slotsmgrtAsyncParseTarget(client *c, sds *host, int *port, long long *timeout) {
    long long port_ll;
    if (getLongLongFromObject(c->argv[2], &port_ll) != C_OK ||
        !(port_ll >= 1 && port_ll < 65536))
    {
        addReplyErrorFormat(c, "invalid value of port (%s)", (char *)c->argv[2]->ptr);
        return C_ERR;
    }

    long long timeout_ll;
    if (getLongLongFromObject(c->argv[3], &timeout_ll) != C_OK ||
        !(timeout_ll >= 0 && timeout_ll <= INT_MAX))
    {
        addReplyErrorFormat(c, "invalid value of timeout (%s)", (char *)c->argv[3]->ptr);
        return C_ERR;
    }
    if (timeout_ll == 0) timeout_ll = SLOTSMGRT_ASYNC_DEFAULT_TIMEOUT_MS;

    *host = c->argv[1]->ptr;
    *port = (int)port_ll;
    *timeout = timeout_ll;
    return C_OK;
}

static int slotsmgrtAsyncParseNonNegative(client *c, robj *obj, const char *name, long long max, long long *val) {
    if (getLongLongFromObject(obj, val) != C_OK || *val < 0 || *val > max) {
        addReplyErrorFormat(c, "invalid value of %s (%s)", name, (char *)obj->ptr);
        return C_ERR;
    }
    return C_OK;
}

static int slotsmgrtAsyncParseSlot(client *c, robj *obj, int *slot) {
    long long val;
    if (getLongLongFromObject(obj, &val) != C_OK) {
        addReplyErrorFormat(c, "invalid slot number = %s", (char *)obj->ptr);
        return C_ERR;
    }
    if (val < 0 || val >= CODIS_SLOTS) {
        addReplyErrorFormat(c, "invalid slot number = %lld", val);
        return C_ERR;
    }
    *slot = (int)val;
    return C_OK;
}

static void slotsmgrtAsyncCollectSlotKeys(redisDb *db, batchedObjectIterator *it, int slot, long long numkeys, int usetag) {
    dict *d = kvstoreGetDict(db->keys, slot);
    if (d == NULL || dictSize(d) == 0) return;

    dictIterator *di = dictGetIterator(d);
    dictEntry *de;
    long long seen = 0;
    while ((de = dictNext(di)) != NULL && seen < numkeys) {
        kvobj *kv = dictGetKV(de);
        sds key_sds = kvobjGetKey(kv);
        robj *key = createStringObject(key_sds, sdslen(key_sds));
        if (usetag) {
            batchedObjectIteratorAddTagged(db, it, key);
        } else {
            batchedObjectIteratorAddDirect(it, key);
        }
        decrRefCount(key);
        seen++;
    }
    dictReleaseIterator(di);
}

static long long slotsmgrtAsyncMaxBufferLimit(long long maxbytes) {
    clientBufferLimitsConfig *config = &server.client_obuf_limits[CLIENT_TYPE_NORMAL];
    if (config->soft_limit_bytes != 0 && (long long)config->soft_limit_bytes < maxbytes) {
        maxbytes = config->soft_limit_bytes;
    }
    if (config->hard_limit_bytes != 0 && (long long)config->hard_limit_bytes < maxbytes) {
        maxbytes = config->hard_limit_bytes;
    }
    return maxbytes;
}

static int slotsmgrtAsyncSendPrelude(slotsmgrtAsyncClient *ac) {
    if (ac->used) return 0;

    client *c = ac->c;
    int msgs = 0;
    ac->used = 1;
    if (server.requirepass != NULL) {
        addReplyArrayLen(c, 2);
        addReplyBulkCString(c, "SLOTSRESTORE-ASYNC-AUTH");
        addReplyBulkCBuffer(c, server.requirepass, sdslen(server.requirepass));
        msgs++;
    }

    addReplyArrayLen(c, 2);
    addReplyBulkCString(c, "SLOTSRESTORE-ASYNC-SELECT");
    addReplyBulkLongLong(c, c->db->id);
    msgs++;
    return msgs;
}

static long slotsmgrtAsyncNextMessagesMicroseconds(slotsmgrtAsyncClient *ac, long atleast, long long usecs) {
    batchedObjectIterator *it = ac->batched_iter;
    long long deadline = ustime() + usecs;
    long msgs = slotsmgrtAsyncSendPrelude(ac);

    if (it->maxbulks > 0 && it->emitted_msgs >= it->maxbulks) {
        it->emitted_msgs = 0;
    }
    while (getClientOutputBufferMemoryUsage(ac->c) < (size_t)it->maxbytes) {
        int emitted = batchedObjectIteratorNext(ac->c, it);
        if (emitted == 0) break;
        msgs += emitted;
        if (msgs < atleast) continue;
        if (ustime() >= deadline) return msgs;
    }
    return msgs;
}

static batchedObjectIterator *slotsmgrtAsyncCreateIteratorFromCommand(client *c, int usetag, int usekey, long long timeout, long long *remaining) {
    long long maxbulks;
    if (slotsmgrtAsyncParseNonNegative(c, c->argv[4], "maxbulks", INT_MAX, &maxbulks) != C_OK) return NULL;
    if (maxbulks == 0) maxbulks = SLOTSMGRT_ASYNC_DEFAULT_MAXBULKS;
    if (maxbulks > SLOTSMGRT_ASYNC_MAX_MAXBULKS) maxbulks = SLOTSMGRT_ASYNC_MAX_MAXBULKS;

    long long maxbytes;
    if (slotsmgrtAsyncParseNonNegative(c, c->argv[5], "maxbytes", INT_MAX, &maxbytes) != C_OK) return NULL;
    if (maxbytes == 0) maxbytes = SLOTSMGRT_ASYNC_DEFAULT_MAXBYTES;
    if (maxbytes > SLOTSMGRT_ASYNC_MAX_MAXBYTES) maxbytes = SLOTSMGRT_ASYNC_MAX_MAXBYTES;
    maxbytes = slotsmgrtAsyncMaxBufferLimit(maxbytes);

    batchedObjectIterator *it = createBatchedObjectIterator(timeout, maxbulks, maxbytes);
    *remaining = 0;

    if (usekey) {
        it->use_slot = 0;
        for (int i = 6; i < c->argc; i++) {
            if (usetag) {
                batchedObjectIteratorAddTagged(c->db, it, c->argv[i]);
            } else {
                batchedObjectIteratorAddDirect(it, c->argv[i]);
            }
        }
        return it;
    }

    int slot;
    if (slotsmgrtAsyncParseSlot(c, c->argv[6], &slot) != C_OK) {
        freeBatchedObjectIterator(it);
        return NULL;
    }

    long long numkeys;
    if (slotsmgrtAsyncParseNonNegative(c, c->argv[7], "numkeys", INT_MAX, &numkeys) != C_OK) {
        freeBatchedObjectIterator(it);
        return NULL;
    }
    if (numkeys == 0) numkeys = 100;

    slotsmgrtAsyncCollectSlotKeys(c->db, it, slot, numkeys, usetag);
    it->use_slot = 1;
    it->slot = slot;
    *remaining = kvstoreDictSize(c->db->keys, slot);
    return it;
}

static void slotsmgrtAsyncGenericCommand(client *c, int usetag, int usekey) {
    sds host;
    int port;
    long long timeout;
    if (slotsmgrtAsyncParseTarget(c, &host, &port, &timeout) != C_OK) return;

    if (getSlotsmgrtAsyncClientMigrationStatusOrBlock(c, NULL, 0) != 0) {
        addReplyError(c, "the specified DB is being migrated");
        return;
    }
    if (c->slotsmgrt_flags & CLIENT_SLOTSMGRT_ASYNC_NORMAL_CLIENT) {
        addReplyError(c, "previous operation has not finished");
        return;
    }

    long long remaining = 0;
    batchedObjectIterator *it = slotsmgrtAsyncCreateIteratorFromCommand(c, usetag, usekey, timeout, &remaining);
    if (it == NULL) return;
    if (!batchedObjectIteratorHasKeys(it)) {
        freeBatchedObjectIterator(it);
        if (usekey) {
            addReplyLongLong(c, 0);
        } else {
            addReplyArrayLen(c, 2);
            addReplyLongLong(c, 0);
            addReplyLongLong(c, remaining);
        }
        return;
    }

    slotsmgrtAsyncClient *ac = getOrCreateSlotsmgrtAsyncClient(c->db->id, host, port, timeout);
    if (ac == NULL) {
        freeBatchedObjectIterator(it);
        addReplyErrorFormat(c, "create client to %s:%d failed", host, port);
        return;
    }

    serverAssert(ac->batched_iter == NULL);
    ac->timeout = timeout;
    ac->lastuse = mstime();
    ac->batched_iter = it;
    ac->sending_msgs = slotsmgrtAsyncNextMessagesMicroseconds(ac, 3, 500);

    int ret = getSlotsmgrtAsyncClientMigrationStatusOrBlock(c, NULL, 1);
    if (ret < 0) addReplyError(c, "previous operation has not finished (call fence again)");
    if (ac->sending_msgs == 0) {
        notifySlotsmgrtAsyncClient(ac, NULL);
        ac->batched_iter = NULL;
        freeBatchedObjectIterator(it);
    }
}

void slotsmgrtSlotAsyncCommand(client *c) {
    slotsmgrtAsyncGenericCommand(c, 0, 0);
}

void slotsmgrtTagSlotAsyncCommand(client *c) {
    slotsmgrtAsyncGenericCommand(c, 1, 0);
}

void slotsmgrtOneAsyncCommand(client *c) {
    slotsmgrtAsyncGenericCommand(c, 0, 1);
}

void slotsmgrtTagOneAsyncCommand(client *c) {
    slotsmgrtAsyncGenericCommand(c, 1, 1);
}

static void slotsmgrtAsyncDumpGenericCommand(client *c, int usetag) {
    long long timeout;
    if (slotsmgrtAsyncParseNonNegative(c, c->argv[1], "timeout", INT_MAX, &timeout) != C_OK) return;
    if (timeout == 0) timeout = SLOTSMGRT_ASYNC_DEFAULT_TIMEOUT_MS;

    long long maxbulks;
    if (slotsmgrtAsyncParseNonNegative(c, c->argv[2], "maxbulks", INT_MAX, &maxbulks) != C_OK) return;
    if (maxbulks == 0) maxbulks = SLOTSMGRT_ASYNC_DUMP_DEFAULT_MAXBULKS;

    batchedObjectIterator *it = createBatchedObjectIterator(timeout, maxbulks, INT_MAX);
    for (int i = 3; i < c->argc; i++) {
        if (usetag) {
            batchedObjectIteratorAddTagged(c->db, it, c->argv[i]);
        } else {
            batchedObjectIteratorAddDirect(it, c->argv[i]);
        }
    }

    void *replylen = addReplyDeferredLen(c);
    int total = batchedObjectIteratorReplyAll(c, it);
    setDeferredArrayLen(c, replylen, total);
    freeBatchedObjectIterator(it);
}

void slotsmgrtOneAsyncDumpCommand(client *c) {
    slotsmgrtAsyncDumpGenericCommand(c, 0);
}

void slotsmgrtTagOneAsyncDumpCommand(client *c) {
    slotsmgrtAsyncDumpGenericCommand(c, 1);
}

void slotsmgrtAsyncFenceCommand(client *c) {
    int ret = getSlotsmgrtAsyncClientMigrationStatusOrBlock(c, NULL, 1);
    if (ret == 0) {
        addReply(c, shared.ok);
    } else if (ret != 1) {
        addReplyError(c, "previous operation has not finished (call fence again)");
    }
}

void slotsmgrtAsyncCancelCommand(client *c) {
    addReplyLongLong(c, releaseSlotsmgrtAsyncClient(c->db->id, "interrupted: canceled"));
}

void slotsmgrtAsyncStatusCommand(client *c) {
    slotsmgrtAsyncClient *ac = getSlotsmgrtAsyncClient(c->db->id);
    if (ac->c == NULL) {
        addReplyNullArray(c);
        return;
    }

    addReplyArrayLen(c, 18);
    addReplyBulkCString(c, "host");
    addReplyBulkCString(c, ac->host);
    addReplyBulkCString(c, "port");
    addReplyBulkLongLong(c, ac->port);
    addReplyBulkCString(c, "used");
    addReplyBulkLongLong(c, ac->used);
    addReplyBulkCString(c, "timeout");
    addReplyBulkLongLong(c, ac->timeout);
    addReplyBulkCString(c, "lastuse");
    addReplyBulkLongLong(c, ac->lastuse);
    addReplyBulkCString(c, "since_lastuse");
    addReplyBulkLongLong(c, mstime() - ac->lastuse);
    addReplyBulkCString(c, "sending_msgs");
    addReplyBulkLongLong(c, ac->sending_msgs);
    addReplyBulkCString(c, "blocked_clients");
    addReplyBulkLongLong(c, ac->blocked_list ? listLength(ac->blocked_list) : 0);
    addReplyBulkCString(c, "batched_iterator");
    if (ac->batched_iter == NULL) {
        addReplyNullArray(c);
    } else {
        addReplyArrayLen(c, 10);
        addReplyBulkCString(c, "keys");
        addReplyBulkLongLong(c, batchedObjectIteratorKeyCount(ac->batched_iter));
        addReplyBulkCString(c, "timeout");
        addReplyBulkLongLong(c, ac->batched_iter->timeout);
        addReplyBulkCString(c, "maxbulks");
        addReplyBulkLongLong(c, ac->batched_iter->maxbulks);
        addReplyBulkCString(c, "maxbytes");
        addReplyBulkLongLong(c, ac->batched_iter->maxbytes);
        addReplyBulkCString(c, "emitted_msgs");
        addReplyBulkLongLong(c, ac->batched_iter->emitted_msgs);
    }
}

void slotsmgrtExecWrapperCommand(client *c) {
    addReplyArrayLen(c, 2);
    if (c->argc < 3) {
        addReplyLongLong(c, -1);
        addReplyError(c, "wrong number of arguments for SLOTSMGRT-EXEC-WRAPPER");
        return;
    }

    int wrapped_argc = c->argc - 2;
    robj **wrapped_argv = c->argv + 2;
    struct redisCommand *cmd = lookupCommand(wrapped_argv, wrapped_argc);
    if (cmd == NULL) {
        addReplyLongLong(c, -1);
        addReplyErrorFormat(c, "invalid command specified (%s)", (char *)c->argv[2]->ptr);
        return;
    }
    if ((cmd->arity > 0 && cmd->arity != wrapped_argc) ||
        (cmd->arity < 0 && wrapped_argc < -cmd->arity))
    {
        addReplyLongLong(c, -1);
        addReplyErrorFormat(c, "wrong number of arguments for command (%s)", (char *)c->argv[2]->ptr);
        return;
    }

    if (lookupKeyWrite(c->db, c->argv[1]) == NULL) {
        addReplyLongLong(c, 0);
        addReplyError(c, "the specified key doesn't exist");
        return;
    }

    if ((cmd->flags & CMD_WRITE) &&
        getSlotsmgrtAsyncClientMigrationStatusOrBlock(c, c->argv[1], 0) != 0)
    {
        addReplyLongLong(c, 1);
        addReplyError(c, "the specified key is being migrated");
        return;
    }

    addReplyLongLong(c, 2);
    robj **argv = zmalloc(sizeof(robj *) * wrapped_argc);
    for (int i = 0; i < wrapped_argc; i++) {
        argv[i] = wrapped_argv[i];
        incrRefCount(argv[i]);
    }
    replaceClientCommandVector(c, wrapped_argc, argv);
    c->cmd = c->lastcmd = c->realcmd = cmd;
    call(c, CMD_CALL_FULL & ~CMD_CALL_PROPAGATE);
}

void slotsmgrtAsyncCleanup(void) {
    for (int i = 0; i < server.dbnum; i++) {
        slotsmgrtAsyncClient *ac = getSlotsmgrtAsyncClient(i);
        if (ac->c == NULL) continue;

        long long elapsed = mstime() - ac->lastuse;
        long long timeout = slotsmgrtAsyncClientIsActive(ac) ? ac->timeout : SLOTSMGRT_ASYNC_IDLE_TIMEOUT_MS;
        if (elapsed > timeout) {
            releaseSlotsmgrtAsyncClient(i, slotsmgrtAsyncClientIsActive(ac) ?
                                        "interrupted: migration timeout" :
                                        "interrupted: idle timeout");
        }
    }
}

void slotsmgrtAsyncUnlinkClient(client *c) {
    if (c->slotsmgrt_flags & CLIENT_SLOTSMGRT_ASYNC_CACHED_CLIENT) {
        unlinkSlotsmgrtAsyncCachedClient(c, "interrupted: connection closed");
    }
    if (c->slotsmgrt_flags & CLIENT_SLOTSMGRT_ASYNC_NORMAL_CLIENT) {
        unlinkSlotsmgrtAsyncNormalClient(c);
    }
}

static int slotsrestoreReplyAck(client *c, int err_code, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    sds msg = sdscatvprintf(sdsempty(), fmt, ap);
    va_end(ap);

    addReplyArrayLen(c, 3);
    addReplyBulkCString(c, "SLOTSRESTORE-ASYNC-ACK");
    addReplyBulkLongLong(c, err_code);
    addReplyBulkSds(c, msg);

    if (err_code != 0) c->flags |= CLIENT_CLOSE_AFTER_REPLY;
    return err_code == 0 ? C_OK : C_ERR;
}

static int slotsrestoreAsyncPayloadExpectedType(const char *cmd) {
    if (!strcasecmp(cmd, "object")) return -1;
    if (!strcasecmp(cmd, "string")) return OBJ_STRING;
    if (!strcasecmp(cmd, "list")) return OBJ_LIST;
    if (!strcasecmp(cmd, "hash")) return OBJ_HASH;
    if (!strcasecmp(cmd, "dict")) return OBJ_SET;
    if (!strcasecmp(cmd, "zset")) return OBJ_ZSET;
    return -2;
}

static int slotsrestoreAsyncRestoreObjectPayload(client *c, robj *key, long long ttl, robj *payload_obj, int expected_type) {
    if (ttl < 0) {
        return slotsrestoreReplyAck(c, -1, "invalid TTL value (TTL=%s)", (char *)c->argv[3]->ptr);
    }

    KeyMetaSpec meta;
    keyMetaSpecInit(&meta);
    robj *val = NULL;

    if (ttl != 0) {
        keyMetaSpecAdd(&meta, KEY_META_ID_EXPIRE, commandTimeSnapshot() + ttl);
    }

    if (verifyDumpPayload((unsigned char *)payload_obj->ptr, sdslen(payload_obj->ptr), NULL) != C_OK) {
        slotsrestoreReplyAck(c, -1, "dump payload version or checksum are wrong");
        goto cleanup_err;
    }

    rio payload;
    rioInitWithBuffer(&payload, payload_obj->ptr);
    int type = rdbLoadType(&payload);
    if (rdbResolveKeyType(&payload, &type, c->db->id, &meta) == -1 ||
        (val = rdbLoadObject(type, &payload, key->ptr, c->db->id, NULL)) == NULL)
    {
        slotsrestoreReplyAck(c, -1, "bad data format");
        goto cleanup_err;
    }

    if (expected_type != -1 && val->type != expected_type) {
        slotsrestoreReplyAck(c, -1, "wrong payload type (expect=%d,got=%d)", expected_type, val->type);
        goto cleanup_err;
    }

    dbDelete(c->db, key);
    kvobj *kv = dbAddInternal(c->db, key, &val, NULL, &meta);
    val = NULL;

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

    meta.numMeta = 0;
    meta.metabits = 0;
    keyModified(c, c->db, key, NULL, 1);
    server.dirty++;
    keyMetaSpecCleanup(&meta);
    return slotsrestoreReplyAck(c, 0, "1");

cleanup_err:
    if (val != NULL) decrRefCount(val);
    keyMetaSpecCleanup(&meta);
    return C_ERR;
}

static int slotsrestoreAsyncHandle(client *c) {
    if (getSlotsmgrtAsyncClientMigrationStatusOrBlock(c, NULL, 0) != 0) {
        return slotsrestoreReplyAck(c, -1, "the specified DB is being migrated");
    }

    if (c->argc < 2) {
        return slotsrestoreReplyAck(c, -1, "wrong number of arguments (argc=%d,cmd=)", c->argc);
    }

    const char *cmd = c->argv[1]->ptr;
    if (c->argc < 3) {
        return slotsrestoreReplyAck(c, -1, "wrong number of arguments (argc=%d,cmd=%s)", c->argc, cmd);
    }

    robj *key = c->argv[2];

    if (!strcasecmp(cmd, "delete")) {
        if (c->argc != 3) {
            return slotsrestoreReplyAck(c, -1, "wrong number of arguments (argc=%d,cmd=%s)", c->argc, cmd);
        }
        int deleted = dbDelete(c->db, key);
        if (deleted) {
            keyModified(c, c->db, key, NULL, 1);
            server.dirty++;
        }
        return slotsrestoreReplyAck(c, 0, deleted ? "1" : "0");
    }

    if (c->argc < 4) {
        return slotsrestoreReplyAck(c, -1, "wrong number of arguments (argc=%d,cmd=%s)", c->argc, cmd);
    }

    long long ttl;
    if (getLongLongFromObject(c->argv[3], &ttl) != C_OK || ttl < 0) {
        return slotsrestoreReplyAck(c, -1, "invalid TTL value (TTL=%s)", (char *)c->argv[3]->ptr);
    }

    if (!strcasecmp(cmd, "expire")) {
        if (c->argc != 4) {
            return slotsrestoreReplyAck(c, -1, "wrong number of arguments (argc=%d,cmd=%s)", c->argc, cmd);
        }
        if (lookupKeyWrite(c->db, key) == NULL) {
            return slotsrestoreReplyAck(c, -1, "the specified key doesn't exist (%s)", (char *)key->ptr);
        }
        if (ttl != 0) {
            setExpire(c, c->db, key, commandTimeSnapshot() + ttl);
        } else {
            removeExpire(c->db, key);
        }
        keyModified(c, c->db, key, NULL, 1);
        server.dirty++;
        return slotsrestoreReplyAck(c, 0, "1");
    }

    int expected_type = slotsrestoreAsyncPayloadExpectedType(cmd);
    if (expected_type != -2) {
        if (c->argc != 5) {
            return slotsrestoreReplyAck(c, -1, "wrong number of arguments (argc=%d,cmd=%s)", c->argc, cmd);
        }
        return slotsrestoreAsyncRestoreObjectPayload(c, key, ttl, c->argv[4], expected_type);
    }

    return slotsrestoreReplyAck(c, -1, "unknown command (argc=%d,cmd=%s)", c->argc, cmd);
}

void slotsrestoreAsyncCommand(client *c) {
    slotsrestoreAsyncHandle(c);
}

static void slotsrestoreAsyncAuthReply(client *c, robj *username, robj *password) {
    robj *err = NULL;
    int result = ACLAuthenticateUser(c, username, password, &err);
    if (result == AUTH_OK) {
        slotsrestoreReplyAck(c, 0, "OK");
    } else if (result == AUTH_ERR) {
        slotsrestoreReplyAck(c, -1, "%s", err ? (char *)err->ptr : "invalid username-password pair or user is disabled");
    }
    if (err) decrRefCount(err);
}

void slotsrestoreAsyncAuthCommand(client *c) {
    redactClientCommandArgument(c, 1);
    if (DefaultUser->flags & USER_FLAG_NOPASS) {
        slotsrestoreReplyAck(c, -1, "Client sent AUTH, but no password is set");
        return;
    }
    slotsrestoreAsyncAuthReply(c, shared.default_username, c->argv[1]);
}

void slotsrestoreAsyncAuth2Command(client *c) {
    redactClientCommandArgument(c, 2);
    slotsrestoreAsyncAuthReply(c, c->argv[1], c->argv[2]);
}

void slotsrestoreAsyncSelectCommand(client *c) {
    long long db;
    if (getLongLongFromObject(c->argv[1], &db) != C_OK ||
        db < 0 || db > INT_MAX || selectDb(c, (int)db) != C_OK)
    {
        slotsrestoreReplyAck(c, -1, "invalid DB index (%s)", (char *)c->argv[1]->ptr);
        return;
    }
    slotsrestoreReplyAck(c, 0, "OK");
}

static void slotsmgrtAsyncDeleteSourceKeys(client *c, batchedObjectIterator *it) {
    unsigned long keycount = listLength(it->removed_keys);
    robj **propargv = keycount ? zmalloc(sizeof(robj *) * (keycount + 1)) : NULL;
    int propargc = 0;

    if (propargv != NULL) propargv[propargc++] = shared.del;

    while (listLength(it->removed_keys) != 0) {
        listNode *head = listFirst(it->removed_keys);
        robj *key = listNodeValue(head);
        if (dbSyncDelete(c->db, key)) {
            keyModified(c, c->db, key, NULL, 1);
            server.dirty++;
            it->migrated++;
            if (propargv != NULL) propargv[propargc++] = key;
        }
        listDelNode(it->removed_keys, head);
    }

    if (propargc > 1) {
        alsoPropagate(c->db->id, propargv, propargc, PROPAGATE_AOF | PROPAGATE_REPL);
        preventCommandPropagation(c);
    }
    zfree(propargv);
}

static void slotsmgrtAsyncComplete(client *c, slotsmgrtAsyncClient *ac) {
    batchedObjectIterator *it = ac->batched_iter;
    if (it == NULL) return;

    slotsmgrtAsyncDeleteSourceKeys(c, it);
    notifySlotsmgrtAsyncClient(ac, NULL);
    ac->batched_iter = NULL;
    freeBatchedObjectIterator(it);
}

static int slotsrestoreAsyncAckHandle(client *c) {
    slotsmgrtAsyncClient *ac = getSlotsmgrtAsyncClient(c->db->id);
    if (ac->c != c) {
        addReplyError(c, "invalid client, permission denied");
        return C_ERR;
    }

    long long errcode;
    if (getLongLongFromObject(c->argv[1], &errcode) != C_OK) {
        addReplyErrorFormat(c, "invalid errcode (%s)", (char *)c->argv[1]->ptr);
        return C_ERR;
    }

    const char *errmsg = c->argv[2]->ptr;
    if (errcode != 0) {
        serverLog(LL_WARNING, "slotsmgrt_async: ack[%lld] %s", errcode, errmsg ? errmsg : "(null)");
        unlinkSlotsmgrtAsyncCachedClient(c, errmsg ? errmsg : "interrupted: restore async ack error");
        return C_ERR;
    }

    if (ac->batched_iter == NULL) {
        addReplyError(c, "invalid iterator (NULL)");
        return C_ERR;
    }
    if (ac->sending_msgs == 0) {
        addReplyError(c, "invalid pending messages");
        return C_ERR;
    }

    ac->lastuse = mstime();
    ac->sending_msgs--;
    ac->sending_msgs += slotsmgrtAsyncNextMessagesMicroseconds(ac, 2, 10);
    if (ac->sending_msgs == 0) slotsmgrtAsyncComplete(c, ac);
    return C_OK;
}

void slotsrestoreAsyncAckCommand(client *c) {
    if (slotsrestoreAsyncAckHandle(c) != C_OK) {
        c->flags |= CLIENT_CLOSE_AFTER_REPLY;
    }
}
