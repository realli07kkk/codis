start_server {tags {"codis external:skip"} overrides {codis-enabled yes save ""}} {
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
