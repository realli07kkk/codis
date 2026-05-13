proc codis_tag_assert {} {
    assert_equal OK [r debug codis-tagindex-assert]
}

start_server {tags {"codis external:skip needs:debug"} overrides {codis-enabled yes enable-debug-command yes save ""}} {
    test {codis-enabled starts without Redis Cluster mode} {
        assert_equal {codis-enabled yes} [r config get codis-enabled]
        assert_equal {cluster-enabled no} [r config get cluster-enabled]
    }

    test {SLOTSHASHKEY uses Codis CRC32 and hash tags} {
        assert_equal {362 899 899 0} [r slotshashkey alpha "{tag}:a" "{tag}:b" "{}abc"]
    }

    test {SLOTSINFO reports Codis slot counts in the current DB} {
        r select 0
        r flushall
        assert_equal OK [r set "{tag}:a" 1]
        assert_equal OK [r set "{tag}:b" 2]
        assert_equal OK [r set alpha 3]
        assert_equal {{899 2}} [r slotsinfo 899 1]
        assert_equal {{362 1}} [r slotsinfo 362 1]

        r select 1
        r flushdb
        assert_equal OK [r set "{tag}:c" 3]
        assert_equal {{899 1}} [r slotsinfo 899 1]

        r select 0
        assert_equal {{899 2}} [r slotsinfo 899 1]
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
    }
}

test {codis-enabled rejects Redis Cluster mode} {
    set status [catch {exec src/redis-server --port 0 --codis-enabled yes --cluster-enabled yes 2>@1} output]
    assert_equal 1 $status
    assert_match {*codis-enabled and cluster-enabled are mutually exclusive*} $output
} {} {external:skip}
