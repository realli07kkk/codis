proc codis_tag_assert {} {
    assert_equal OK [r debug codis-tagindex-assert]
}

proc codis_slotsscan_all {slot {count 2}} {
    set cursor 0
    set keys {}
    while 1 {
        set res [r slotsscan $slot $cursor count $count]
        set cursor [lindex $res 0]
        set keys [concat $keys [lindex $res 1]]
        if {$cursor == 0} {
            break
        }
    }
    return [lsort -unique $keys]
}

start_server {tags {"codis external:skip needs:debug"} overrides {codis-enabled yes enable-debug-command yes save ""}} {
    test {codis-enabled starts without Redis Cluster mode} {
        assert_equal {codis-enabled yes} [r config get codis-enabled]
        assert_equal {cluster-enabled no} [r config get cluster-enabled]
    }

    test {SLOTSHASHKEY uses Codis CRC32 and hash tags} {
        assert_equal {362 899 899 0} [r slotshashkey alpha "{tag}:a" "{tag}:b" "{}abc"]
        assert_equal {} [r slotshashkey]
    }

    test {Codis basic slot commands are discoverable} {
        set infos [r command info slotsscan slotsdel slotscheck]
        assert_equal 3 [llength $infos]
        foreach info $infos {
            assert {[llength $info] > 0}
        }
        assert {[lsearch -exact [lindex [lindex $infos 1] 2] write] >= 0}
        assert_equal 0 [lindex [lindex $infos 1] 3]
    }

    test {SLOTSINFO reports Codis slot counts in the current DB} {
        r select 0
        r flushall
        assert_equal OK [r set "{tag}:a" 1]
        assert_equal OK [r set "{tag}:b" 2]
        assert_equal OK [r set alpha 3]
        assert_equal {{899 2}} [r slotsinfo 899 1]
        assert_equal {{362 1}} [r slotsinfo 362 1]
        assert_equal {} [r slotsinfo 1023 999999]

        r select 1
        r flushdb
        assert_equal OK [r set "{tag}:c" 3]
        assert_equal {{899 1}} [r slotsinfo 899 1]

        r select 0
        assert_equal {{899 2}} [r slotsinfo 899 1]
    }

    test {SLOTSSCAN scans keys from one Codis slot in the current DB} {
        r select 0
        r flushall
        assert_equal OK [r set "{tag}:a" 1]
        assert_equal OK [r set "{tag}:b" 2]
        assert_equal OK [r set alpha 3]
        assert_equal [list "{tag}:a" "{tag}:b"] [codis_slotsscan_all 899 1]
        foreach key [codis_slotsscan_all 899 1] {
            assert_equal {899} [r slotshashkey $key]
        }

        set empty [r slotsscan 0 0 count 10]
        assert_equal 0 [lindex $empty 0]
        assert_equal {} [lindex $empty 1]

        r select 1
        r flushdb
        assert_equal OK [r set "{tag}:c" 3]
        assert_equal [list "{tag}:c"] [codis_slotsscan_all 899 1]

        r select 0
        assert_equal [list "{tag}:a" "{tag}:b"] [codis_slotsscan_all 899 1]
    }

    test {SLOTSSCAN rejects invalid arguments} {
        r select 0
        assert_error {*invalid slot number*} {r slotsscan -1 0}
        assert_error {*invalid slot number*} {r slotsscan 1024 0}
        assert_error {*invalid cursor*} {r slotsscan 899 bad}
        assert_error {*syntax*} {r slotsscan 899 0 count 0}
        assert_error {*syntax*} {r slotsscan 899 0 match *}
    }

    test {SLOTSDEL deletes requested Codis slots in the current DB} {
        r select 0
        r flushall
        assert_equal OK [r set "{tag}:a" 1]
        assert_equal OK [r set "{tag}:b" 2]
        assert_equal OK [r set alpha 3]
        assert_equal OK [r set "{}abc" 4]

        assert_equal {{899 0} {362 0}} [r slotsdel 899 362]
        assert_equal {} [r get "{tag}:a"]
        assert_equal {} [r get "{tag}:b"]
        assert_equal {} [r get alpha]
        assert_equal 4 [r get "{}abc"]
        assert_equal {{0 1}} [r slotsinfo 0 1]
        codis_tag_assert

        assert_equal OK [r set "{tag}:again" 1]
        assert_equal {{899 0} {899 0}} [r slotsdel 899 899]
        codis_tag_assert

        assert_equal OK [r set "{tag}:db0" 0]
        r select 1
        r flushdb
        assert_equal OK [r set "{tag}:db1" 1]
        assert_equal {{899 0}} [r slotsdel 899]
        assert_equal {} [r get "{tag}:db1"]
        codis_tag_assert

        r select 0
        assert_equal 0 [r get "{tag}:db0"]
        assert_equal {{899 1}} [r slotsinfo 899 1]
        codis_tag_assert
    }

    test {SLOTSCHECK validates Codis slot keyspace and tag index} {
        r select 0
        r flushall
        assert_equal OK [r set "{tag}:check" 1]
        assert_equal OK [r set plain 2]
        assert_equal OK [r slotscheck]
        codis_tag_assert
    }

    test {Codis tag index tracks key lifecycle operations} {
        r select 0
        r flushall
        codis_tag_assert

        assert_equal OK [r set "{tag}:a" 1]
        assert_equal OK [r set plain 1]
        codis_tag_assert

        assert_equal OK [r set "{tag}:a" 2]
        codis_tag_assert

        assert_equal 1 [r del "{tag}:a"]
        codis_tag_assert

        assert_equal OK [r set "{tag}:unlink" 1]
        assert_equal 1 [r unlink "{tag}:unlink"]
        codis_tag_assert

        assert_equal OK [r set "{tag}:rename" 1]
        assert_equal OK [r rename "{tag}:rename" "{tag}:renamed"]
        codis_tag_assert

        assert_equal OK [r set "{tag}:move" 1]
        assert_equal 1 [r move "{tag}:move" 1]
        codis_tag_assert
        r select 1
        codis_tag_assert
        r select 0

        assert_equal 1 [r copy "{tag}:renamed" "{tag}:copy" replace]
        codis_tag_assert

        assert_equal OK [r set "{tag}:expire" 1 px 20]
        after 50
        assert_equal {} [r get "{tag}:expire"]
        codis_tag_assert
    }

    test {Codis tag index survives flush and RDB reload} {
        r select 0
        r flushall
        assert_equal OK [r set "{tag}:reload" 1]
        assert_equal OK [r set plain 1]
        codis_tag_assert

        assert_equal OK [r save]
        assert_equal OK [r debug reload]
        codis_tag_assert

        assert_equal OK [r flushdb async]
        codis_tag_assert
        assert_equal OK [r flushall]
        codis_tag_assert
    } {} {needs:save}

    test {Codis tag index remains consistent after eviction} {
        r select 0
        r flushall
        wait_lazyfree_done r

        set old_policy [lindex [r config get maxmemory-policy] 1]
        set used [expr {[s used_memory] - [s mem_not_counted_for_evict]}]
        r config set maxmemory-policy volatile-random
        r config set maxmemory [expr {$used + 100*1024}]

        catch {r setex "{tag}:evict" 10000 x}
        catch {r setbit codis-big-key 1600000 0}
        catch {r getbit codis-big-key 0}

        r config set maxmemory-policy $old_policy
        r config set maxmemory 0
        codis_tag_assert
    } {} {needs:config-maxmemory}
}

start_server {tags {"codis external:skip"} overrides {save ""}} {
    test {codis-enabled defaults to no} {
        assert_equal {codis-enabled no} [r config get codis-enabled]
        assert_error {*codis mode is disabled*} {r slotsinfo}
        assert_error {*codis mode is disabled*} {r slotsscan 0 0}
        assert_error {*codis mode is disabled*} {r slotsdel 0}
        assert_error {*codis mode is disabled*} {r slotscheck}
        assert_equal {362} [r slotshashkey alpha]
    }
}

test {codis-enabled rejects Redis Cluster mode} {
    set status [catch {exec src/redis-server --port 0 --codis-enabled yes --cluster-enabled yes 2>@1} output]
    assert_equal 1 $status
    assert_match {*codis-enabled and cluster-enabled are mutually exclusive*} $output
} {} {external:skip}
