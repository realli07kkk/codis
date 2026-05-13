proc assert_slotsmgrt_pair {result migrated remaining} {
    assert_equal $migrated [lindex $result 0]
    assert_equal $remaining [lindex $result 1]
}

start_server {tags {"codis"} overrides {codis-enabled yes}} {
    set srv_host [srv 0 host]
    set srv_port [srv 0 port]

    test "SLOTSMGRTONE - invalid argument count" {
        catch {r slotsmgrtone 127.0.0.1 3000} err
        assert_match {*wrong number*} $err
    }

    test "SLOTSMGRTONE - non-existent key returns 0" {
        set result [r slotsmgrtone 127.0.0.1 $srv_port 3000 nonexistent]
        assert_equal 0 $result
    }

    test "SLOTSMGRTSLOT - empty slot returns zero" {
        set result [r slotsmgrtslot 127.0.0.1 $srv_port 3000 999]
        assert_slotsmgrt_pair $result 0 0
    }

    test "SLOTSMGRTSLOT - invalid slot number" {
        catch {r slotsmgrtslot 127.0.0.1 10000 3000 1024} err
        assert_match {*invalid slot number*} $err
    }

    test "SLOTSMGRTSLOT - invalid timeout" {
        catch {r slotsmgrtslot 127.0.0.1 10000 -1 0} err
        assert_match {*invalid timeout*} $err
    }

    test "SLOTSMGRTONE - invalid port" {
        catch {r slotsmgrtone 127.0.0.1 notaport 3000 k} err
        assert_match {*invalid port*} $err
    }

    test "SLOTSMGRTTAGONE - non-existent key returns 0" {
        set result [r slotsmgrttagone 127.0.0.1 $srv_port 3000 noexist]
        assert_equal 0 $result
    }

    test "SLOTSMGRTTAGSLOT - empty slot returns zero" {
        set result [r slotsmgrttagslot 127.0.0.1 $srv_port 3000 888]
        assert_slotsmgrt_pair $result 0 0
    }

    test "SLOTSMGRTONE - target unreachable" {
        r set k "v"
        catch {r slotsmgrtone 127.0.0.1 19999 3000 k} err
        assert_match {*IOERR*} $err
        assert_equal "v" [r get k]
    }

    test "SLOTSMGRTTAGONE - target unreachable" {
        r flushdb
        r set k "v"
        catch {r slotsmgrttagone 127.0.0.1 19999 3000 k} err
        assert_match {*IOERR*} $err
        assert_equal "v" [r get k]
    }
}

start_server {tags {"codis"} overrides {codis-enabled yes}} {
    start_server {tags {"codis"} overrides {codis-enabled yes}} {
        set dst_host [srv 0 host]
        set dst_port [srv 0 port]

        test "SLOTSMGRTONE - migrates key and TTL to target" {
            r -1 flushdb
            r flushdb
            r -1 set ttl:key "value" px 10000

            assert_equal 1 [r -1 slotsmgrtone $dst_host $dst_port 3000 ttl:key]
            assert_equal 0 [r -1 exists ttl:key]
            assert_equal "value" [r get ttl:key]
            set ttl [r pttl ttl:key]
            assert {$ttl > 0 && $ttl <= 10000}
        }

        test "SLOTSMGRTSLOT - migrates one key from a non-empty slot" {
            r -1 flushdb
            r flushdb
            r -1 set slot:key "slot-value"
            set slot [lindex [r -1 slotshashkey slot:key] 0]

            set result [r -1 slotsmgrtslot $dst_host $dst_port 3000 $slot]
            assert_slotsmgrt_pair $result 1 0
            assert_equal 0 [r -1 exists slot:key]
            assert_equal "slot-value" [r get slot:key]
        }

        test "SLOTSMGRTTAGONE - migrates all keys with the same hash tag" {
            r -1 flushdb
            r flushdb
            r -1 set user:{42}:a "a"
            r -1 set user:{42}:b "b"

            assert_equal 2 [r -1 slotsmgrttagone $dst_host $dst_port 3000 user:{42}:a]
            assert_equal 0 [r -1 exists user:{42}:a user:{42}:b]
            assert_equal "a" [r get user:{42}:a]
            assert_equal "b" [r get user:{42}:b]
        }

        test "SLOTSMGRTTAGSLOT - migrates hash-tag group from a non-empty slot" {
            r -1 flushdb
            r flushdb
            r -1 set order:{77}:a "a"
            r -1 set order:{77}:b "b"
            set slot [lindex [r -1 slotshashkey order:{77}:a] 0]

            set result [r -1 slotsmgrttagslot $dst_host $dst_port 3000 $slot]
            assert_slotsmgrt_pair $result 2 0
            assert_equal 0 [r -1 exists order:{77}:a order:{77}:b]
            assert_equal "a" [r get order:{77}:a]
            assert_equal "b" [r get order:{77}:b]
        }

        test "SLOTSMGRTONE - propagates source deletion as DEL" {
            r -1 flushdb
            r flushdb
            r -1 set prop:key "prop-value"
            set repl [attach_to_replication_stream_on_connection -1]

            assert_equal 1 [r -1 slotsmgrtone $dst_host $dst_port 3000 prop:key]
            assert_replication_stream $repl {
                {select *}
                {del prop:key}
            }
            close_replication_stream $repl
        } {} {needs:repl}
    }
}

start_server {tags {"codis" "auth"} overrides {codis-enabled yes requirepass srcpass}} {
    start_server {tags {"codis" "auth"} overrides {codis-enabled yes requirepass dstpass}} {
        set dst_host [srv 0 host]
        set dst_port [srv 0 port]

        test "SLOTSMGRTONE - target auth failure keeps source key" {
            r -1 auth srcpass
            r auth dstpass
            r -1 flushdb
            r flushdb
            r -1 set auth:key "value"

            catch {r -1 slotsmgrtone $dst_host $dst_port 3000 auth:key} err
            assert_match {*auth failed*} $err
            assert_equal "value" [r -1 get auth:key]
            assert_equal 0 [r exists auth:key]
        }
    }
}
