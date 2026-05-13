start_server {tags {"codis"} overrides {codis-enabled yes}} {
    test "SLOTSRESTORE - single key" {
        r set mykey "hello world"
        set dump [r dump mykey]
        r del mykey
        r slotsrestore mykey 0 $dump
        assert_equal "hello world" [r get mykey]
    }

    test "SLOTSRESTORE - key with TTL" {
        r set ttlkey "value"
        set dump [r dump ttlkey]
        r del ttlkey
        r slotsrestore ttlkey 5000 $dump
        assert_equal "value" [r get ttlkey]
        set ttl [r pttl ttlkey]
        assert {$ttl > 0 && $ttl <= 5000}
    }

    test "SLOTSRESTORE - overwrite existing key" {
        r set oldkey "oldval"
        r set newkey "newval"
        set dump [r dump newkey]
        r del newkey
        r slotsrestore oldkey 0 $dump
        assert_equal "newval" [r get oldkey]
    }

    test "SLOTSRESTORE - multiple keys" {
        r set a "1"
        r set b "2"
        set dump_a [r dump a]
        set dump_b [r dump b]
        r del a b
        r slotsrestore a 0 $dump_a b 0 $dump_b
        assert_equal "1" [r get a]
        assert_equal "2" [r get b]
    }

    test "SLOTSRESTORE - hash type" {
        r hset hkey f1 v1 f2 v2
        set dump [r dump hkey]
        r del hkey
        r slotsrestore hkey 0 $dump
        assert_equal "v1" [r hget hkey f1]
        assert_equal "v2" [r hget hkey f2]
    }

    test "SLOTSRESTORE - list type" {
        r lpush lkey a b c
        set dump [r dump lkey]
        r del lkey
        r slotsrestore lkey 0 $dump
        assert_equal {c b a} [r lrange lkey 0 -1]
    }

    test "SLOTSRESTORE - set type" {
        r sadd skey x y z
        set dump [r dump skey]
        r del skey
        r slotsrestore skey 0 $dump
        assert_equal 3 [r scard skey]
    }

    test "SLOTSRESTORE - zset type" {
        r zadd zkey 1 m1 2 m2
        set dump [r dump zkey]
        r del zkey
        r slotsrestore zkey 0 $dump
        assert_equal 2 [r zcard zkey]
        assert_equal 1 [r zscore zkey m1]
    }

    test "SLOTSRESTORE - stream type" {
        set id [r xadd streamkey * f v]
        set dump [r dump streamkey]
        r del streamkey
        r slotsrestore streamkey 0 $dump
        assert_equal $id [lindex [r xrange streamkey - +] 0 0]
        assert_equal "v" [lindex [r xrange streamkey - +] 0 1 1]
    }

    test "SLOTSRESTORE - invalid payload" {
        catch {r slotsrestore badkey 0 "INVALID_PAYLOAD"} err
        assert_match {*checksum*} $err
    }

    test "SLOTSRESTORE - invalid ttl" {
        r set vkey "val"
        set dump [r dump vkey]
        r del vkey
        catch {r slotsrestore vkey -1 $dump} err
        assert_match {*invalid ttl*} $err
    }

    test "SLOTSRESTORE - invalid argument count" {
        catch {r slotsrestore k1 0} err
        assert_match {*wrong number*} $err
    }
}
