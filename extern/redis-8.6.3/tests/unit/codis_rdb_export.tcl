proc codis_rdb_http_request {request} {
    set s [socket [srv 0 host] [srv 0 port]]
    fconfigure $s -translation binary -encoding binary -blocking 0
    puts -nonewline $s $request
    flush $s
    return [codis_rdb_http_read_all $s]
}

proc codis_rdb_http_read_all {s {timeout 5000}} {
    set response ""
    set deadline [expr {[clock milliseconds] + $timeout}]
    while {1} {
        append response [read $s]
        if {[eof $s]} {
            close $s
            return $response
        }
        if {[clock milliseconds] > $deadline} {
            close $s
            error "timeout reading HTTP response"
        }
        after 10
    }
}

proc codis_rdb_http_open {request} {
    set s [socket [srv 0 host] [srv 0 port]]
    fconfigure $s -translation binary -encoding binary -blocking 0
    puts -nonewline $s $request
    flush $s
    return $s
}

proc codis_rdb_export_raw_request {{auth secret} {path /codis/rdb/latest}} {
    set request "GET $path HTTP/1.1\r\n"
    append request "Host: localhost\r\n"
    if {$auth ne ""} {
        append request "X-Codis-RDB-Auth: $auth\r\n"
    }
    append request "\r\n"
    return $request
}

proc codis_rdb_http_status {response} {
    if {![regexp {^HTTP/1\.1 ([0-9]+)} $response -> status]} {
        error "missing HTTP status in response: $response"
    }
    return $status
}

proc codis_rdb_http_body {response} {
    set sep [string first "\r\n\r\n" $response]
    if {$sep < 0} {
        error "missing HTTP header terminator"
    }
    return [string range $response [expr {$sep + 4}] end]
}

proc codis_rdb_http_header {response name} {
    set sep [string first "\r\n\r\n" $response]
    if {$sep < 0} {
        error "missing HTTP header terminator"
    }
    set head [string range $response 0 [expr {$sep - 1}]]
    foreach line [split $head "\r\n"] {
        set colon [string first ":" $line]
        if {$colon < 0} continue
        set line_name [string tolower [string range $line 0 [expr {$colon - 1}]]]
        if {$line_name eq [string tolower $name]} {
            return [string trim [string range $line [expr {$colon + 1}] end]]
        }
    }
    return ""
}

proc codis_rdb_write_file {path data} {
    set fd [open $path wb]
    fconfigure $fd -translation binary -encoding binary
    puts -nonewline $fd $data
    close $fd
}

proc codis_rdb_export_request {{auth secret} {path /codis/rdb/latest}} {
    return [codis_rdb_http_request [codis_rdb_export_raw_request $auth $path]]
}

test {codis-rdb-export-enabled requires non-empty auth} {
    set status [catch {exec src/redis-server --port 0 --save "" --codis-rdb-export-enabled yes 2>@1} output]
    assert_equal 1 $status
    assert_match {*codis-rdb-export-auth must be set when codis-rdb-export-enabled is yes*} $output
} {} {external:skip}

start_server {tags {"codis_rdb_export network external:skip tls:skip"} overrides {save ""}} {
    test {Codis RDB export defaults to disabled and keeps Redis protocol intact} {
        assert_equal {codis-rdb-export-enabled no} [r config get codis-rdb-export-enabled]
        assert_equal {codis-rdb-export-auth {}} [r config get codis-rdb-export-auth]
        assert_equal {codis-rdb-export-rate-limit 0} [r config get codis-rdb-export-rate-limit]

        set response [codis_rdb_export_request secret]
        assert_equal 404 [codis_rdb_http_status $response]

        assert_equal PONG [r ping]
        r set codis-rdb-export-key value
        assert_equal value [r get codis-rdb-export-key]

        reconnect
        r write "GET codis-rdb-export-key\r\n"
        r flush
        assert_equal value [r read]
    }

    test {Codis RDB export rate limit config is modifiable} {
        assert_equal OK [r config set codis-rdb-export-rate-limit 1mb]
        assert_equal {codis-rdb-export-rate-limit 1048576} [r config get codis-rdb-export-rate-limit]

        assert_equal OK [r config set codis-rdb-export-rate-limit 0]
        assert_equal {codis-rdb-export-rate-limit 0} [r config get codis-rdb-export-rate-limit]

        assert_error {*argument must be a memory value*} {
            r config set codis-rdb-export-rate-limit -1
        }
    }
}

start_server {tags {"codis_rdb_export network external:skip tls:skip"} overrides {save "" codis-rdb-export-enabled yes codis-rdb-export-auth secret}} {
    test {Codis RDB export rejects missing or wrong auth before file selection} {
        set dir [file normalize [dict get [srv 0 config] dir]]
        codis_rdb_write_file [file join $dir dump.rdb] "not-an-rdb"

        set missing [codis_rdb_export_request ""]
        assert_equal 403 [codis_rdb_http_status $missing]

        set wrong [codis_rdb_export_request wrong]
        assert_equal 403 [codis_rdb_http_status $wrong]
    }

    test {Codis RDB export returns 404 when dbfilename RDB is missing} {
        set dir [file normalize [dict get [srv 0 config] dir]]
        catch {file delete -force [file join $dir dump.rdb]}
        codis_rdb_write_file [file join $dir other.rdb] "REDIS0001-other"

        set response [codis_rdb_export_request secret]
        assert_equal 404 [codis_rdb_http_status $response]
    }

    test {Codis RDB export rejects query-string auth and non-exact path} {
        set dir [file normalize [dict get [srv 0 config] dir]]
        codis_rdb_write_file [file join $dir dump.rdb] "REDIS0001-body"

        reconnect
        r write "GET /codis/rdb/latest?auth=secret HTTP/1.1\r\n"
        r flush
        assert_error "*wrong*arguments*get*" {r read}
    }

    test {Codis RDB export does not override Redis POST cross-protocol guard} {
        set response [codis_rdb_http_request "POST /codis/rdb/latest HTTP/1.1\r\nHost: localhost\r\n\r\n"]
        assert_equal "" $response
    }

    test {Codis RDB export rejects symlink and non-RDB dbfilename candidates} {
        set dir [file normalize [dict get [srv 0 config] dir]]
        set dump [file join $dir dump.rdb]
        set target [file join $dir target.rdb]

        catch {file delete -force $dump $target}
        codis_rdb_write_file $target "REDIS0001-target"
        file link -symbolic $dump $target
        set response [codis_rdb_export_request secret]
        assert_equal 404 [codis_rdb_http_status $response]

        catch {file delete -force $dump $target}
        codis_rdb_write_file $dump "not-an-rdb"
        set response [codis_rdb_export_request secret]
        assert_equal 404 [codis_rdb_http_status $response]
    }

    test {Codis RDB export streams current dbfilename RDB without changing lastsave} {
        set dir [file normalize [dict get [srv 0 config] dir]]
        set dump [file join $dir dump.rdb]
        set other [file join $dir other.rdb]
        set body "REDIS0001-codis-rdb-export-body"

        codis_rdb_write_file $other "REDIS0001-newer-other"
        after 20
        codis_rdb_write_file $dump $body

        set before [r lastsave]
        set response [codis_rdb_export_request secret]
        set after [r lastsave]

        assert_equal 200 [codis_rdb_http_status $response]
        assert_equal [string length $body] [codis_rdb_http_header $response Content-Length]
        assert_match {attachment; filename="dump.rdb"} [codis_rdb_http_header $response Content-Disposition]
        assert_not_equal "" [codis_rdb_http_header $response X-Codis-RDB-Mtime]
        assert_equal $body [codis_rdb_http_body $response]
        assert_equal $before $after

        assert_equal PONG [r ping]
    }

    test {Codis RDB export rate limiting does not block normal commands} {
        set dir [file normalize [dict get [srv 0 config] dir]]
        set dump [file join $dir dump.rdb]
        set body "REDIS0001[string repeat X [expr {160*1024 - 9}]]"

        codis_rdb_write_file $dump $body
        assert_equal OK [r config set codis-rdb-export-rate-limit 64kb]
        assert_equal {codis-rdb-export-rate-limit 65536} [r config get codis-rdb-export-rate-limit]

        set start [clock milliseconds]
        set s [codis_rdb_http_open [codis_rdb_export_raw_request secret]]
        after 100

        assert_equal PONG [r ping]
        r set codis-rdb-export-rate-limit-key value
        assert_equal value [r get codis-rdb-export-rate-limit-key]

        set response [codis_rdb_http_read_all $s 6000]
        set elapsed [expr {[clock milliseconds] - $start}]
        assert_equal 200 [codis_rdb_http_status $response]
        assert_equal [string length $body] [codis_rdb_http_header $response Content-Length]
        assert_equal $body [codis_rdb_http_body $response]
        assert_morethan_equal $elapsed 1200

        assert_equal OK [r config set codis-rdb-export-rate-limit 0]
    }

    test {Codis RDB export rate limit budget is shared by concurrent downloads} {
        set dir [file normalize [dict get [srv 0 config] dir]]
        set dump [file join $dir dump.rdb]
        set body "REDIS0001[string repeat Y [expr {96*1024 - 9}]]"

        codis_rdb_write_file $dump $body
        assert_equal OK [r config set codis-rdb-export-rate-limit 64kb]

        set request [codis_rdb_export_raw_request secret]
        set start [clock milliseconds]
        set s1 [codis_rdb_http_open $request]
        set s2 [codis_rdb_http_open $request]

        set response1 [codis_rdb_http_read_all $s1 7000]
        set response2 [codis_rdb_http_read_all $s2 7000]
        set elapsed [expr {[clock milliseconds] - $start}]

        assert_equal 200 [codis_rdb_http_status $response1]
        assert_equal 200 [codis_rdb_http_status $response2]
        assert_equal $body [codis_rdb_http_body $response1]
        assert_equal $body [codis_rdb_http_body $response2]
        assert_morethan_equal $elapsed 1500

        assert_equal OK [r config set codis-rdb-export-rate-limit 0]
    }

    test {Codis RDB export CONFIG SET decrease clamps active token budget} {
        set dir [file normalize [dict get [srv 0 config] dir]]
        set dump [file join $dir dump.rdb]
        set warmup "REDIS0001[string repeat W [expr {1024 - 9}]]"
        set body "REDIS0001[string repeat Z [expr {48*1024 - 9}]]"

        assert_equal OK [r config set codis-rdb-export-rate-limit 1mb]
        codis_rdb_write_file $dump $warmup
        set warmup_response [codis_rdb_export_request secret]
        assert_equal 200 [codis_rdb_http_status $warmup_response]
        assert_equal $warmup [codis_rdb_http_body $warmup_response]

        assert_equal OK [r config set codis-rdb-export-rate-limit 16kb]
        codis_rdb_write_file $dump $body

        set start [clock milliseconds]
        set response [codis_rdb_export_request secret]
        set elapsed [expr {[clock milliseconds] - $start}]

        assert_equal 200 [codis_rdb_http_status $response]
        assert_equal $body [codis_rdb_http_body $response]
        assert_morethan_equal $elapsed 1200

        assert_equal OK [r config set codis-rdb-export-rate-limit 0]
    }
}

start_server {tags {"codis_rdb_export network external:skip tls:skip"} overrides {save "" codis-rdb-export-enabled yes codis-rdb-export-auth secret io-threads 2}} {
    test {Codis RDB export is handed back to main thread under IO threads} {
        set dir [file normalize [dict get [srv 0 config] dir]]
        set dump [file join $dir dump.rdb]
        set body "REDIS0001-codis-rdb-export-iothread-body"

        codis_rdb_write_file $dump $body

        set response [codis_rdb_export_request secret]
        assert_equal 200 [codis_rdb_http_status $response]
        assert_equal $body [codis_rdb_http_body $response]
        assert_equal PONG [r ping]

        r config set dbfilename other.rdb
        codis_rdb_write_file [file join $dir other.rdb] "REDIS0001-other-body"
        set response [codis_rdb_export_request secret]
        assert_equal 200 [codis_rdb_http_status $response]
        assert_equal "REDIS0001-other-body" [codis_rdb_http_body $response]
    }
}
