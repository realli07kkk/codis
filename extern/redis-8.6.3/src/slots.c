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
    if (val < 0 || val > CODIS_SLOTS) {
        addReplyErrorFormat(c, "invalid slot count = %lld", val);
        return C_ERR;
    }
    *count = (int)val;
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
