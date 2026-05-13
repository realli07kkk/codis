#include "server.h"

void codisSlotsBuildHarnessMarker(void) {
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

void slotshashkeyCommand(client *c) {
    addReplyArrayLen(c, c->argc - 1);
    for (int i = 1; i < c->argc; i++) {
        sds key = c->argv[i]->ptr;
        addReplyLongLong(c, codisHashSlot(key, sdslen(key)));
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
        if (kvstoreDictSize(c->db->keys, i) != 0) non_empty++;
    }

    addReplyArrayLen(c, non_empty);
    for (int i = beg; i < end; i++) {
        unsigned long size = kvstoreDictSize(c->db->keys, i);
        if (size == 0) continue;
        addReplyArrayLen(c, 2);
        addReplyLongLong(c, i);
        addReplyLongLong(c, size);
    }
}
