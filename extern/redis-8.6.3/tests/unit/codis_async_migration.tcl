proc assert_slotsmgrt_async_ack {reply code {message {}}} {
    assert_equal SLOTSRESTORE-ASYNC-ACK [lindex $reply 0]
    assert_equal $code [lindex $reply 1]
    if {$message ne {}} {
        assert_equal $message [lindex $reply 2]
    }
}

proc assert_slotsmgrt_async_pair {result migrated remaining} {
    assert_equal $migrated [lindex $result 0]
    assert_equal $remaining [lindex $result 1]
}

proc slotsmgrt_async_status_field {status field} {
    set index [lsearch -exact $status $field]
    if {$index < 0} {
        return {}
    }
    return [lindex $status [expr {$index + 1}]]
}

proc assert_slotsmgrt_async_status_fields {status fields} {
    foreach field $fields {
        assert {[lsearch -exact $status $field] >= 0}
    }
}

set ::slotsmgrt_async_dummy_sockets {}

proc slotsmgrt_async_dummy_accept {chan addr port} {
    fconfigure $chan -blocking 0 -translation binary
    lappend ::slotsmgrt_async_dummy_sockets $chan
}

proc slotsmgrt_async_dummy_close_all {server_sock} {
    catch {close $server_sock}
    foreach chan $::slotsmgrt_async_dummy_sockets {
        catch {close $chan}
    }
    set ::slotsmgrt_async_dummy_sockets {}
}

start_server {tags {"codis"} overrides {codis-enabled yes}} {
    test "Codis async migration commands are discoverable" {
        foreach cmd {
            slotsmgrtslot-async
            slotsmgrttagslot-async
            slotsmgrtone-async
            slotsmgrttagone-async
            slotsmgrtone-async-dump
            slotsmgrttagone-async-dump
            slotsmgrt-async-fence
            slotsmgrt-async-cancel
            slotsmgrt-async-status
            slotsmgrt-exec-wrapper
            slotsrestore-async
            slotsrestore-async-auth
            slotsrestore-async-auth2
            slotsrestore-async-select
            slotsrestore-async-ack
        } {
            assert {[r command info $cmd] ne {}}
        }
    }

    test "SLOTSRESTORE-ASYNC restores object payload aliases" {
        r flushdb

        r set src:string value px 10000
        set dump [r dump src:string]
        assert_slotsmgrt_async_ack [r slotsrestore-async string dst:string 5000 $dump] 0 1
        assert_equal value [r get dst:string]
        set ttl [r pttl dst:string]
        assert {$ttl > 0 && $ttl <= 5000}

        r set src:object object-value
        set dump [r dump src:object]
        assert_slotsmgrt_async_ack [r slotsrestore-async object dst:object 0 $dump] 0 1
        assert_equal object-value [r get dst:object]

        r rpush src:list a b
        set dump [r dump src:list]
        assert_slotsmgrt_async_ack [r slotsrestore-async list dst:list 0 $dump] 0 1
        assert_equal {a b} [r lrange dst:list 0 -1]

        r hset src:hash f v
        set dump [r dump src:hash]
        assert_slotsmgrt_async_ack [r slotsrestore-async hash dst:hash 0 $dump] 0 1
        assert_equal v [r hget dst:hash f]

        r sadd src:dict a b
        set dump [r dump src:dict]
        assert_slotsmgrt_async_ack [r slotsrestore-async dict dst:dict 0 $dump] 0 1
        assert_equal 2 [r scard dst:dict]

        r zadd src:zset 1.5 a
        set dump [r dump src:zset]
        assert_slotsmgrt_async_ack [r slotsrestore-async zset dst:zset 0 $dump] 0 1
        assert_equal 1.5 [r zscore dst:zset a]

        r xadd src:stream * f v
        set dump [r dump src:stream]
        assert_slotsmgrt_async_ack [r slotsrestore-async object dst:stream 0 $dump] 0 1
        assert_equal 1 [r xlen dst:stream]
    }
}

start_server {tags {"codis"} overrides {codis-enabled yes}} {
    test "SLOTSRESTORE-ASYNC bad payload returns ACK error" {
        set reply [r slotsrestore-async object bad:key 0 INVALID_PAYLOAD]
        assert_slotsmgrt_async_ack $reply -1
        assert_match {*checksum*} [lindex $reply 2]
    }
}

start_server {tags {"codis" "auth"} overrides {codis-enabled yes requirepass pass}} {
    test "SLOTSRESTORE-ASYNC-AUTH and AUTH2 return ACK" {
        assert_slotsmgrt_async_ack [r slotsrestore-async-auth pass] 0 OK
        assert_equal PONG [r ping]

        set c [redis [srv 0 host] [srv 0 port] 0 $::tls]
        assert_slotsmgrt_async_ack [$c slotsrestore-async-auth2 default pass] 0 OK
        assert_equal PONG [$c ping]
        $c close
    }
}

start_server {tags {"codis"} overrides {codis-enabled yes}} {
    start_server {tags {"codis"} overrides {codis-enabled yes}} {
        set dst_host [srv 0 host]
        set dst_port [srv 0 port]

        test "SLOTSMGRTONE-ASYNC migrates key and TTL to target" {
            r -1 flushall
            r flushall
            r -1 set async:key value px 10000

            assert_equal 1 [r -1 slotsmgrtone-async $dst_host $dst_port 30000 10 65536 async:key]
            assert_equal 0 [r -1 exists async:key]
            assert_equal value [r get async:key]
            set ttl [r pttl async:key]
            assert {$ttl > 0 && $ttl <= 10000}
        }

        test "SLOTSMGRTSLOT-ASYNC migrates current DB slot keys" {
            r -1 flushall
            r flushall
            r -1 set slot:key slot-value
            set slot [lindex [r -1 slotshashkey slot:key] 0]

            set result [r -1 slotsmgrtslot-async $dst_host $dst_port 30000 10 65536 $slot 10]
            assert_slotsmgrt_async_pair $result 1 0
            assert_equal 0 [r -1 exists slot:key]
            assert_equal slot-value [r get slot:key]
        }

        test "SLOTSMGRTTAGONE-ASYNC migrates same tag keys" {
            r -1 flushall
            r flushall
            r -1 set user:{42}:a a
            r -1 set user:{42}:b b

            assert_equal 2 [r -1 slotsmgrttagone-async $dst_host $dst_port 30000 10 65536 user:{42}:a]
            assert_equal 0 [r -1 exists user:{42}:a user:{42}:b]
            assert_equal a [r get user:{42}:a]
            assert_equal b [r get user:{42}:b]
        }

        test "SLOTSMGRTTAGSLOT-ASYNC migrates hash-tag group from slot" {
            r -1 flushall
            r flushall
            r -1 set group:{7}:a a
            r -1 set group:{7}:b b
            set slot [lindex [r -1 slotshashkey group:{7}:a] 0]

            set result [r -1 slotsmgrttagslot-async $dst_host $dst_port 30000 1 65536 $slot 1]
            assert_slotsmgrt_async_pair $result 2 0
            assert_equal 0 [r -1 exists group:{7}:a group:{7}:b]
            assert_equal a [r get group:{7}:a]
            assert_equal b [r get group:{7}:b]
        }

        test "SLOTSMGRTONE-ASYNC-DUMP emits executable restore stream without deleting source" {
            r -1 flushall
            r flushall
            r -1 set dump:key dump-value
            set stream [r -1 slotsmgrtone-async-dump 30000 10 dump:key]
            assert_equal 2 [llength $stream]
            foreach cmd $stream {
                r {*}$cmd
            }
            assert_equal dump-value [r -1 get dump:key]
            assert_equal dump-value [r get dump:key]
        }

        test "SLOTSMGRTONE-ASYNC migrates large object with bounded maxbytes" {
            r -1 flushall
            r flushall
            set large [string repeat x 262144]
            r -1 set large:key $large

            assert_equal 1 [r -1 slotsmgrtone-async $dst_host $dst_port 30000 10 1024 large:key]
            assert_equal 0 [r -1 exists large:key]
            assert_equal 262144 [string length [r get large:key]]
        }

        test "SLOTSMGRTONE-ASYNC SELECT keeps DBs isolated" {
            r -1 flushall
            r flushall
            r -1 select 1
            r -1 set db:key db1-value

            assert_equal 1 [r -1 slotsmgrtone-async $dst_host $dst_port 30000 10 65536 db:key]
            assert_equal 0 [r -1 exists db:key]
            r select 1
            assert_equal db1-value [r get db:key]
            r select 0
            assert_equal {} [r get db:key]
            r select 9
            r -1 select 9
        }

        test "SLOTSMGRTONE-ASYNC propagates source deletion as DEL" {
            r -1 flushall
            r flushall
            r -1 set prop:key prop-value
            set repl [attach_to_replication_stream_on_connection -1]

            assert_equal 1 [r -1 slotsmgrtone-async $dst_host $dst_port 30000 10 65536 prop:key]
            assert_replication_stream $repl {
                {select *}
                {del prop:key}
            }
            close_replication_stream $repl
        } {} {needs:repl}

        test "SLOTSMGRTONE-ASYNC handles empty and unreachable targets without deleting source" {
            r -1 select 9
            r select 9
            r -1 flushall
            r flushall

            assert_equal 0 [r -1 slotsmgrtone-async $dst_host $dst_port 30000 10 65536 missing:key]
            assert_equal 0 [r exists missing:key]

            set slot [lindex [r -1 slotshashkey empty:key] 0]
            assert_equal {0 0} [r -1 slotsmgrtslot-async $dst_host $dst_port 30000 10 65536 $slot 10]

            r -1 set unreachable:key v
            catch {r -1 slotsmgrtone-async 127.0.0.1 1 10 10 65536 unreachable:key} err
            assert {
                [string match {*create client*failed*} $err] ||
                [string match {*connection closed*} $err]
            }
            assert_equal v [r -1 get unreachable:key]
            assert_equal 0 [r exists unreachable:key]
        }

        test "SLOTSMGRT-ASYNC-FENCE waits and duplicate async migration is rejected" {
            r -1 select 9
            r select 9
            r -1 flushall
            r flushall
            r -1 set fence:key v
            r -1 set duplicate:key v2
            r -1 slotsmgrt-async-cancel

            r client pause 10000 write
            set rd [redis_deferring_client -1]
            set fence [redis_deferring_client -1]
            set code [catch {
                $rd slotsmgrtone-async $dst_host $dst_port 30000 10 65536 fence:key
                wait_for_condition 50 100 {
                    [slotsmgrt_async_status_field [r -1 slotsmgrt-async-status] batched_iterator] ne {}
                } else {
                    fail "async migration did not become active"
                }

                assert_error {*specified DB is being migrated*} {
                    r -1 slotsmgrtone-async $dst_host $dst_port 30000 10 65536 duplicate:key
                }

                $fence slotsmgrt-async-fence
                wait_for_condition 50 100 {
                    [slotsmgrt_async_status_field [r -1 slotsmgrt-async-status] blocked_clients] == 2
                } else {
                    fail "async fence did not block"
                }

                r client unpause
                assert_equal 1 [$rd read]
                assert_equal OK [$fence read]
                assert_equal 0 [r -1 exists fence:key]
                assert_equal v [r get fence:key]
                assert_equal v2 [r -1 get duplicate:key]
            } err opts]
            catch {$rd close}
            catch {$fence close}
            r client unpause
            if {$code} {
                return -options $opts $err
            }
        }

        test "SLOTSMGRT-EXEC-WRAPPER blocks writes and allows reads during async migration" {
            r -1 select 9
            r select 9
            r -1 flushall
            r flushall
            r -1 set block:key v
            r -1 set other:key other-v
            r -1 slotsmgrt-async-cancel

            set dummy_sock [socket -server slotsmgrt_async_dummy_accept -myaddr 127.0.0.1 0]
            set dummy_port [lindex [fconfigure $dummy_sock -sockname] 2]
            set rd [redis_deferring_client -1]
            set raw {}
            set code [catch {
                $rd slotsmgrtone-async 127.0.0.1 $dummy_port 30000 10 65536 block:key
                wait_for_condition 50 100 {
                    [slotsmgrt_async_status_field [r -1 slotsmgrt-async-status] batched_iterator] ne {}
                } else {
                    fail "async migration did not become active"
                }

                assert_slotsmgrt_async_status_fields [r -1 slotsmgrt-async-status] {
                    host port used timeout lastuse since_lastuse sending_msgs blocked_clients batched_iterator
                }
                assert_equal {2 OK} [r -1 slotsmgrt-exec-wrapper other:key set other:key other-x]
                assert_equal other-x [r -1 get other:key]

                set raw [redis [srv -1 host] [srv -1 port] 0 $::tls {} 1]
                assert_equal {+OK} [$raw select 9]
                assert_equal {*2} [$raw slotsmgrt-exec-wrapper block:key set block:key x]
                assert_equal {:1} [$raw read]
                assert_match {-ERR*being migrated*} [$raw read]
                $raw close
                set raw {}

                assert_equal {2 v} [r -1 slotsmgrt-exec-wrapper block:key get block:key]
                assert_equal 1 [r -1 slotsmgrt-async-cancel]
                assert_error {*canceled*} {$rd read}
                assert_equal v [r -1 get block:key]
            } err opts]
            catch {$rd close}
            catch {$raw close}
            slotsmgrt_async_dummy_close_all $dummy_sock
            if {$code} {
                return -options $opts $err
            }
        }

        test "SLOTSMGRTONE-ASYNC timeout keeps source key" {
            r -1 select 9
            r select 9
            r -1 flushall
            r flushall
            r -1 set timeout:key v
            r -1 slotsmgrt-async-cancel

            set dummy_sock [socket -server slotsmgrt_async_dummy_accept -myaddr 127.0.0.1 0]
            set dummy_port [lindex [fconfigure $dummy_sock -sockname] 2]
            set rd [redis_deferring_client -1]
            set code [catch {
                $rd slotsmgrtone-async 127.0.0.1 $dummy_port 50 10 65536 timeout:key
                wait_for_condition 50 100 {
                    [slotsmgrt_async_status_field [r -1 slotsmgrt-async-status] batched_iterator] ne {}
                } else {
                    fail "async migration did not become active"
                }
                wait_for_condition 50 100 {
                    [r -1 slotsmgrt-async-status] eq {}
                } else {
                    fail "async migration did not timeout"
                }
                assert_error {*timeout*} {$rd read}
                assert_equal v [r -1 get timeout:key]
                assert_equal 0 [r exists timeout:key]
            } err opts]
            catch {$rd close}
            slotsmgrt_async_dummy_close_all $dummy_sock
            if {$code} {
                return -options $opts $err
            }
        }
    }
}

start_server {tags {"codis" "auth"} overrides {codis-enabled yes requirepass srcpass}} {
    start_server {tags {"codis" "auth"} overrides {codis-enabled yes requirepass dstpass}} {
        set dst_host [srv 0 host]
        set dst_port [srv 0 port]

        test "SLOTSMGRTONE-ASYNC target auth failure keeps source key" {
            r -1 auth srcpass
            r auth dstpass
            r -1 flushdb
            r flushdb
            r -1 set auth:key value

            catch {r -1 slotsmgrtone-async $dst_host $dst_port 30000 10 65536 auth:key} err
            assert_match {*invalid username-password*} $err
            assert_equal value [r -1 get auth:key]
            assert_equal 0 [r exists auth:key]
        }
    }
}
