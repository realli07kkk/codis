'use strict';

dashboard.service('MockApi', ['$q', '$timeout', '$window', function ($q, $timeout, $window) {
    var products = ['codis-demo', 'codis-order', 'codis-cache'];
    var params = new URLSearchParams($window.location.search || '');
    var scenario = params.get('scenario') || 'normal';
    var clusters = {};
    var pitrStates = [
        'pending', 'validating', 'checking_prereq', 'checking_feasibility',
        'shutting_down', 'snapshotting', 'truncating', 'restarting',
        'waiting_load', 'resyncing_replicas', 'succeeded'
    ];
    var pitrStepNames = [
        'validate', 'prereq', 'feasibility', 'shutdown', 'snapshot',
        'truncate', 'restart', 'wait-load', 'resync'
    ];

    function timestamp() {
        return new Date().toISOString();
    }

    function readable(size) {
        var value = Number(size) || 0;
        var units = ['B', 'KB', 'MB', 'GB', 'TB'];
        var index = 0;
        while (value >= 1024 && index < units.length - 1) {
            value /= 1024;
            index++;
        }
        return value.toFixed(index === 0 ? 0 : 2) + ' ' + units[index];
    }

    function failure(status, cause) {
        return {status: status, data: {Cause: cause}};
    }

    function fail(status, cause) {
        throw failure(status, cause);
    }

    function delayed(handler, delay) {
        var deferred = $q.defer();
        $timeout(function () {
            try {
                deferred.resolve(angular.copy(handler()));
            } catch (error) {
                deferred.reject(error && error.data ? error : failure(500, String(error)));
            }
        }, delay);
        return deferred.promise;
    }

    function integer(value, label, minimum, maximum) {
        var text = String(value === undefined || value === null ? '' : value);
        if (!/^-?\d+$/.test(text)) {
            fail(800, 'invalid ' + label);
        }
        var parsed = Number(text);
        if ((minimum !== undefined && parsed < minimum) || (maximum !== undefined && parsed > maximum)) {
            fail(800, 'invalid ' + label);
        }
        return parsed;
    }

    function flag(value) {
        if (value === true || value === false) {
            return value;
        }
        if (String(value) !== '0' && String(value) !== '1') {
            fail(800, 'expected flag value 0 or 1');
        }
        return String(value) === '1';
    }

    function queryInt(query, name, fallback) {
        var raw = query ? query.get(name) : null;
        if (raw === null || raw === undefined || raw === '') {
            return fallback;
        }
        return integer(raw, name);
    }

    function decode(value) {
        try {
            return decodeURIComponent(value || '');
        } catch (ignore) {
            return value || '';
        }
    }

    function canonicalPath(path) {
        var value = String(path || '/').split('?')[0].split('#')[0] || '/';
        value = value.replace(/\/(?:\{xauth\}|[0-9a-f]{32})(?=\/|$)/i, '/{xauth}');
        if (value.length > 1) {
            value = value.replace(/\/+$/, '');
        }
        return value.charAt(0) === '/' ? value : '/' + value;
    }

    function match(path, pattern) {
        var actual = path.split('/');
        var expected = pattern.split('/');
        if (actual.length !== expected.length) {
            return null;
        }
        var values = {};
        for (var i = 0; i < expected.length; i++) {
            if (expected[i].charAt(0) === ':') {
                values[expected[i].slice(1)] = decode(actual[i]);
            } else if (actual[i] !== expected[i]) {
                return null;
            }
        }
        return values;
    }

    function makeGroup(state, id) {
        return {id: id, servers: [], promoting: {}, out_of_sync: false};
    }

    function makeServer(address, datacenter) {
        return {
            server: address,
            datacenter: datacenter || '',
            action: {},
            replica_group: false
        };
    }

    function seedPitrJob(state, id, group, failed) {
        var created = new Date(Date.now() - 3600000).toISOString();
        var names = failed ? ['validate', 'prereq'] : pitrStepNames;
        var steps = names.map(function (name, index) {
            return {
                name: name,
                status: failed && index === names.length - 1 ? 'failed' : 'done',
                started_at: created,
                finished_at: created,
                error: failed && index === names.length - 1 ? 'appendonly or aof-timestamp-enabled is disabled' : ''
            };
        });
        return {
            id: id,
            product_name: state.product,
            group_id: group.id,
            server_addr: group.servers[0].server,
            truncate_ts: Math.floor(Date.now() / 1000) - 3600,
            aof_dir: '/var/lib/codis/' + state.product + '/appendonlydir',
            aof_manifest: '/var/lib/codis/' + state.product + '/appendonlydir/appendonly.aof.manifest',
            last_segment_file: 'appendonly.aof.2.incr.aof',
            state: failed ? 'failed' : 'succeeded',
            steps: steps,
            created_at: created,
            updated_at: created,
            error: failed ? 'PITR prerequisite check failed' : '',
            _progress: failed ? 2 : pitrStates.length - 1
        };
    }

    function createCluster(product, productIndex) {
        var state = {
            product: product,
            productIndex: productIndex,
            tick: 0,
            proxySequence: 0,
            groupSequence: 0,
            pitrSequence: 0,
            rdbSequence: 0,
            proxies: [],
            groups: [],
            sentinels: [],
            slots: [],
            proxyIssues: {},
            serverIssues: {},
            proxyMetrics: {},
            groupMetrics: {},
            slotAction: {disabled: false, interval: 1000},
            acl: {revision: 0, enabled: false, sync_status: 'not_configured', users: []},
            qps: {revision: 0, limit: 0, enabled: false, sync_status: 'not_configured'},
            pitrJobs: [],
            migrationJobs: [],
            migrationSequence: 0,
            rdbJobs: {}
        };
        if (scenario === 'empty') {
            return state;
        }

        for (var i = 0; i < 4; i++) {
            var token = 'proxy-' + (productIndex + 1) + '-00' + (i + 1);
            var host = '10.' + (20 + productIndex) + '.0.' + (10 + i);
            state.proxies.push({
                id: i + 1,
                token: token,
                start_time: timestamp(),
                admin_addr: host + ':' + (11080 + i),
                proto_type: 'tcp4',
                proxy_addr: host + ':' + (11000 + i),
                product_name: product,
                pid: 4200 + i,
                hostname: 'codis-proxy-' + (i + 1),
                datacenter: i % 2 ? 'az-b' : 'az-a'
            });
            state.proxyMetrics[token] = {
                commands: 1800000 + i * 173000,
                fails: 12 + i * 3,
                redisErrors: 2 + i
            };
            if (i === 2) {
                state.proxyIssues[token] = 'error';
            } else if (i === 3) {
                state.proxyIssues[token] = 'pending';
            }
        }
        state.proxySequence = 4;

        for (var gid = 1; gid <= 3; gid++) {
            var group = makeGroup(state, gid);
            for (var serverIndex = 0; serverIndex < 2; serverIndex++) {
                var address = '10.' + (20 + productIndex) + '.' + gid + '.' + (20 + serverIndex) + ':6379';
                group.servers.push(makeServer(address, serverIndex ? 'az-b' : 'az-a'));
                if (gid === 2 && serverIndex === 1) {
                    state.serverIssues[address] = 'replica-down';
                }
                if (gid === 3 && serverIndex === 0) {
                    state.serverIssues[address] = 'collection';
                }
            }
            state.groups.push(group);
            state.groupMetrics[gid] = {
                memory: (512 + gid * 192 + productIndex * 64) * 1024 * 1024,
                keys: 86000 + gid * 37000 + productIndex * 9000
            };
        }
        state.groupSequence = 3;

        state.sentinels = [
            '10.' + (20 + productIndex) + '.0.30:26379',
            '10.' + (20 + productIndex) + '.0.31:26379'
        ];
        state.sentinelOutOfSync = false;

        for (var sid = 0; sid < 1024; sid++) {
            state.slots.push({id: sid, group_id: Math.floor(sid * 3 / 1024) + 1, action: {}});
        }
        for (var migrating = 120; migrating < 128; migrating++) {
            state.slots[migrating].action = {index: 0, state: 'migrating', target_id: 2};
        }
        for (var pending = 700; pending < 712; pending++) {
            state.slots[pending].action = {index: 0, state: 'pending', target_id: 1};
        }

        state.acl = {
            revision: 3,
            enabled: true,
            sync_status: 'ready',
            updated_at: new Date(Date.now() - 300000).toISOString(),
            users: [
                {name: 'default', enabled: true, password_count: 1, rules: ['~*', '+@all']},
                {name: 'app', enabled: true, db: 0, password_count: 1, rules: ['~app:*', '+get', '+set', '+del', '+expire']},
                {name: 'readonly', enabled: true, password_count: 1, rules: ['~*', '+get', '+mget', '+exists', '+ttl']}
            ]
        };
        state.qps = {revision: 2, limit: 24000 + productIndex * 4000, enabled: true, sync_status: 'ready'};
        state.pitrJobs = [
            seedPitrJob(state, 'pitr-' + (productIndex + 1) + '-001', state.groups[0], false),
            seedPitrJob(state, 'pitr-' + (productIndex + 1) + '-002', state.groups[1], true)
        ];
        state.pitrSequence = 2;
        state.migrationJobs = seedMigrationJobs(state);
        state.migrationSequence = state.migrationJobs.length;
        return state;
    }

    products.forEach(function (product, index) {
        clusters[product] = createCluster(product, index);
    });

    function cluster(product) {
        if (!clusters[product]) {
            fail(404, 'unknown product ' + product);
        }
        return clusters[product];
    }

    function requireGroup(state, id) {
        var wanted = integer(id, 'group id', 1, 9999);
        for (var i = 0; i < state.groups.length; i++) {
            if (state.groups[i].id === wanted) {
                return state.groups[i];
            }
        }
        fail(800, 'group ' + wanted + ' not found');
    }

    function requireProxy(state, token) {
        for (var i = 0; i < state.proxies.length; i++) {
            if (state.proxies[i].token === token) {
                return state.proxies[i];
            }
        }
        fail(800, 'proxy ' + token + ' not found');
    }

    function findServer(state, address) {
        for (var i = 0; i < state.groups.length; i++) {
            var servers = state.groups[i].servers;
            for (var j = 0; j < servers.length; j++) {
                if (servers[j].server === address) {
                    return {group: state.groups[i], server: servers[j], index: j};
                }
            }
        }
        return null;
    }

    function ensureSlots(state) {
        if (state.slots.length === 1024) {
            return;
        }
        var existing = {};
        state.slots.forEach(function (slot) {
            existing[slot.id] = slot;
        });
        state.slots = [];
        for (var i = 0; i < 1024; i++) {
            state.slots.push(existing[i] || {id: i, group_id: 0, action: {}});
        }
    }

    function requireSlot(state, id) {
        ensureSlots(state);
        var sid = integer(id, 'slot id', 0, 1023);
        return state.slots[sid];
    }

    function overview(state) {
        return {
            model: {
                product_name: state.product,
                admin_addr: '10.' + (20 + state.productIndex) + '.0.2:18080',
                start_time: new Date(Date.now() - 86400000).toISOString()
            },
            config: {
                product_name: state.product,
                coordinator_name: 'filesystem',
                coordinator_addr: '/tmp/codis-' + state.product,
                migration_method: 'semi-async',
                migration_parallel_slots: 4,
                pitr_enabled: true,
                rdb_analysis_workspace: '/tmp/codis-rdb-analysis'
            },
            stats: buildStats(state)
        };
    }

    function buildStats(state) {
        state.tick++;
        var raw = {
            proxy: {models: state.proxies, stats: {}},
            group: {models: state.groups, stats: {}},
            sentinels: {
                model: {servers: state.sentinels, out_of_sync: !!state.sentinelOutOfSync},
                stats: {},
                masters: {}
            },
            slots: state.slots,
            slot_action: {
                disabled: state.slotAction.disabled,
                interval: state.slotAction.interval,
                failed: false,
                remain: 0,
                progress: {status: 'idle', remain: 0}
            }
        };

        state.proxies.forEach(function (proxy, index) {
            var issue = state.proxyIssues[proxy.token];
            if (issue === 'error') {
                raw.proxy.stats[proxy.token] = {error: 'mock proxy admin connection refused'};
                return;
            }
            var pending = issue === 'pending';
            var wave = Math.sin((state.tick + index * 2) / 3) * 850 + Math.cos((state.tick + index) / 5) * 320;
            var qps = pending ? 0 : Math.max(100, Math.round(5200 + index * 1150 + state.productIndex * 400 + wave));
            var alive = pending ? 0 : Math.max(1, Math.round(180 + index * 45 + Math.sin((state.tick + index) / 4) * 24));
            var metrics = state.proxyMetrics[proxy.token];
            metrics.commands += qps * 3;
            if (!pending && (state.tick + index) % 7 === 0) {
                metrics.fails += 1 + index;
                metrics.redisErrors += index % 2;
            }
            var stats = {
                online: !pending,
                ops: {qps: qps, total: metrics.commands, fails: metrics.fails, redis: {errors: metrics.redisErrors}},
                sessions: {alive: alive, total: alive + (pending ? 0 : 24 + index * 3)},
                sentinels: {switched: index === 1 && state.tick % 11 === 0},
                backend: {primary_only: false},
                qps_limit: {
                    revision: state.qps.revision,
                    limit: state.qps.limit,
                    enabled: state.qps.enabled,
                    accepted: qps * 3,
                    rejected: state.qps.enabled && qps > state.qps.limit ? qps - state.qps.limit : 0
                }
            };
            if (scenario === 'partial') {
                if (index === 0) {
                    delete stats.ops.qps;
                }
                if (index === 1) {
                    delete stats.sessions.alive;
                }
                if (index === 2) {
                    stats.ops.redis = null;
                }
            }
            raw.proxy.stats[proxy.token] = {timeout: false, error: '', stats: stats};
        });

        state.groups.forEach(function (group) {
            var metrics = state.groupMetrics[group.id];
            if (!metrics) {
                metrics = state.groupMetrics[group.id] = {memory: 536870912, keys: 1000};
            }
            var memory = Math.max(0, Math.round(metrics.memory * (1 + Math.sin((state.tick + group.id) / 6) * 0.018)));
            var keys = Math.max(0, Math.round(metrics.keys + state.tick * (group.id + 1) + Math.sin(state.tick / 4) * 18));
            group.servers.forEach(function (server, serverIndex) {
                var issue = state.serverIssues[server.server];
                if (issue === 'collection') {
                    raw.group.stats[server.server] = {error: 'mock Redis stats collection failed'};
                    return;
                }
                var stats = {
                    used_memory: memory + serverIndex * 3145728,
                    maxmemory: 8589934592,
                    run_id: 'run-' + state.productIndex + '-' + group.id + '-' + serverIndex + '-' + server.server.replace(/\D/g, ''),
                    db0: 'keys=' + keys + ',expires=' + Math.floor(keys * 0.18) + ',avg_ttl=3600000',
                    db1: 'keys=' + Math.floor(keys * 0.04) + ',expires=0,avg_ttl=0'
                };
                if (serverIndex > 0) {
                    stats.master_addr = group.servers[0].server;
                    stats.master_link_status = issue === 'replica-down' ? 'down' : 'up';
                }
                if (scenario === 'partial') {
                    if (group.id === 1 && serverIndex === 0) {
                        delete stats.used_memory;
                    }
                    if (group.id === 2 && serverIndex === 0) {
                        delete stats.db0;
                    }
                    if (serverIndex === 1) {
                        delete stats.maxmemory;
                    }
                }
                raw.group.stats[server.server] = {timeout: false, error: '', stats: stats};
            });
        });

        state.sentinels.forEach(function (address, sentinelIndex) {
            var stats = {sentinel_masters: state.groups.length};
            state.groups.forEach(function (group, groupIndex) {
                var down = sentinelIndex === 1 && groupIndex === 1;
                stats['master' + groupIndex] = 'name=' + state.product + '-' + group.id +
                    ',status=' + (down ? 's_down' : 'ok') + ',slaves=' + Math.max(0, group.servers.length - 1) +
                    ',sentinels=' + state.sentinels.length;
                if (group.servers.length) {
                    raw.sentinels.masters[group.id] = group.servers[0].server;
                }
            });
            raw.sentinels.stats[address] = {timeout: false, error: '', stats: stats};
        });

        var actions = state.slots.filter(function (slot) {
            return slot.action && slot.action.state;
        });
        raw.slot_action.remain = actions.length;
        raw.slot_action.progress = {
            status: state.slotAction.disabled ? 'disabled' : (actions.length ? 'migrating' : 'idle'),
            remain: actions.length
        };
        return raw;
    }

    function createProxy(state, address) {
        if (!address) {
            fail(800, 'missing proxy address');
        }
        for (var i = 0; i < state.proxies.length; i++) {
            if (state.proxies[i].proxy_addr === address) {
                fail(800, 'proxy ' + address + ' already exists');
            }
        }
        var sequence = ++state.proxySequence;
        var token = 'proxy-' + (state.productIndex + 1) + '-' + String(sequence).padStart(4, '0');
        var parts = address.split(':');
        var port = parts.length > 1 ? integer(parts.pop(), 'proxy port', 1, 65535) : 11000;
        var admin = parts.join(':') + ':' + (port + 1000 > 65535 ? port - 1000 : port + 1000);
        state.proxies.push({
            id: sequence,
            token: token,
            start_time: timestamp(),
            admin_addr: admin,
            proto_type: 'tcp4',
            proxy_addr: address,
            product_name: state.product,
            pid: 5000 + sequence,
            hostname: 'codis-proxy-' + sequence,
            datacenter: 'az-a'
        });
        state.proxyIssues[token] = 'pending';
        state.proxyMetrics[token] = {commands: 0, fails: 0, redisErrors: 0};
        return {token: token};
    }

    function removeProxy(state, token) {
        requireProxy(state, token);
        state.proxies = state.proxies.filter(function (proxy) {
            return proxy.token !== token;
        });
        delete state.proxyIssues[token];
        delete state.proxyMetrics[token];
        return {};
    }

    function createGroup(state, id) {
        var gid = integer(id, 'group id', 1, 9999);
        for (var i = 0; i < state.groups.length; i++) {
            if (state.groups[i].id === gid) {
                fail(800, 'group ' + gid + ' already exists');
            }
        }
        var group = makeGroup(state, gid);
        state.groups.push(group);
        state.groups.sort(function (a, b) { return a.id - b.id; });
        state.groupMetrics[gid] = {memory: 536870912, keys: 0};
        state.groupSequence = Math.max(state.groupSequence, gid);
        return {id: gid};
    }

    function removeGroup(state, id) {
        var group = requireGroup(state, id);
        if (group.servers.length) {
            fail(800, 'group ' + group.id + ' is not empty');
        }
        state.groups = state.groups.filter(function (item) { return item.id !== group.id; });
        delete state.groupMetrics[group.id];
        state.slots.forEach(function (slot) {
            if (slot.group_id === group.id) {
                slot.group_id = 0;
            }
            if (slot.action && slot.action.target_id === group.id) {
                slot.action = {};
            }
        });
        return {};
    }

    function resyncGroup(state, group) {
        group.out_of_sync = false;
        group.promoting = {};
        group.servers.forEach(function (server) {
            server.action = {};
            delete state.serverIssues[server.server];
        });
        return {};
    }

    function addGroupServer(state, gid, address, datacenter) {
        var group = requireGroup(state, gid);
        if (!address) {
            fail(800, 'missing server address');
        }
        if (findServer(state, address)) {
            fail(800, 'server ' + address + ' already exists');
        }
        group.servers.push(makeServer(address, datacenter));
        group.out_of_sync = false;
        return {};
    }

    function deleteGroupServer(state, gid, address) {
        var group = requireGroup(state, gid);
        var index = -1;
        group.servers.forEach(function (server, serverIndex) {
            if (server.server === address) {
                index = serverIndex;
            }
        });
        if (index < 0) {
            fail(800, 'server ' + address + ' not found in group ' + group.id);
        }
        if (index === 0 && group.servers.length > 1) {
            fail(800, 'cannot remove master while replicas remain');
        }
        group.servers.splice(index, 1);
        delete state.serverIssues[address];
        return {};
    }

    function promoteGroupServer(state, gid, address) {
        var group = requireGroup(state, gid);
        var index = -1;
        group.servers.forEach(function (server, serverIndex) {
            if (server.server === address) {
                index = serverIndex;
            }
        });
        if (index < 0) {
            fail(800, 'server ' + address + ' not found in group ' + group.id);
        }
        if (index > 0) {
            group.servers.unshift(group.servers.splice(index, 1)[0]);
        }
        group.promoting = {};
        group.out_of_sync = false;
        return {};
    }

    function setReplicaGroup(state, gid, address, value) {
        var group = requireGroup(state, gid);
        var found = findServer(state, address);
        if (!found || found.group.id !== group.id) {
            fail(800, 'server ' + address + ' not found in group ' + group.id);
        }
        found.server.replica_group = flag(value);
        return {};
    }

    function setSyncAction(state, address, active) {
        var found = findServer(state, address);
        if (!found) {
            fail(800, 'server ' + address + ' not found');
        }
        found.server.action = active ? {index: 0, state: 'pending'} : {};
        return {};
    }

    function createSlotsSome(state, from, to, count) {
        var source = requireGroup(state, from).id;
        var target = requireGroup(state, to).id;
        var wanted = integer(count, 'slots number', 1, 1024);
        if (source === target) {
            fail(800, 'source and target groups must differ');
        }
        ensureSlots(state);
        var candidates = state.slots.filter(function (slot) {
            return slot.group_id === source && (!slot.action || !slot.action.state);
        });
        if (candidates.length < wanted) {
            fail(800, 'only ' + candidates.length + ' slots are available for migration');
        }
        candidates.slice(0, wanted).forEach(function (slot) {
            slot.action = {index: 0, state: 'pending', target_id: target};
        });
        return {count: wanted};
    }

    function createSlotsRange(state, beg, end, gid) {
        var first = integer(beg, 'slot begin', 0, 1023);
        var last = integer(end, 'slot end', 0, 1023);
        var target = requireGroup(state, gid).id;
        if (first > last) {
            fail(800, 'slot begin must not exceed slot end');
        }
        ensureSlots(state);
        for (var i = first; i <= last; i++) {
            state.slots[i].action = {index: 0, state: 'pending', target_id: target};
        }
        return {count: last - first + 1};
    }

    function rebalancePlan(state) {
        var groups = state.groups.map(function (group) { return group.id; }).sort(function (a, b) { return a - b; });
        var plan = [];
        for (var i = 0; i < 1024; i++) {
            plan.push(groups.length ? groups[Math.floor(i * groups.length / 1024)] : 0);
        }
        return plan;
    }

    function applyRebalance(state, plan) {
        ensureSlots(state);
        for (var i = 0; i < 1024; i++) {
            state.slots[i].group_id = plan[i];
            state.slots[i].action = {};
        }
        return plan;
    }

    function aclView(state) {
        return {
            revision: state.acl.revision,
            enabled: state.acl.enabled,
            sync_status: state.acl.sync_status,
            users: state.acl.users,
            updated_at: state.acl.updated_at || ''
        };
    }

    function replaceACL(state, data) {
        if (!data || !Array.isArray(data.users)) {
            fail(800, 'ACL update requires a users array');
        }
        var previous = {};
        state.acl.users.forEach(function (user) { previous[user.name] = user; });
        var names = {};
        var users = data.users.map(function (input) {
            var name = String(input.name || '').trim();
            if (!name || names[name] || !Array.isArray(input.rules)) {
                fail(800, 'invalid or duplicate ACL user ' + name);
            }
            names[name] = true;
            var old = previous[name] || {};
            var user = {
                name: name,
                enabled: !!input.enabled,
                password_count: (old.password_count || 0) + (input.new_password ? 1 : 0),
                rules: input.rules.slice()
            };
            if (!input.new_password && !old.password_count && !input.password_hashes) {
                user.password_count = 0;
            } else if (!input.new_password) {
                user.password_count = old.password_count || (input.password_hashes ? input.password_hashes.length : 1);
            }
            if (input.db !== undefined && input.db !== null && input.db !== '') {
                user.db = integer(input.db, 'ACL user db', 0);
            }
            return user;
        });
        state.acl = {
            revision: state.acl.revision + 1,
            enabled: !!data.enabled,
            sync_status: 'ready',
            users: users,
            updated_at: timestamp()
        };
        return aclView(state);
    }

    function saveQPSLimit(state, data) {
        if (!data) {
            fail(800, 'missing proxy QPS limit request');
        }
        var limit = integer(data.limit, 'proxy QPS limit', 0);
        state.qps = {
            revision: state.qps.revision + 1,
            limit: limit,
            enabled: limit > 0,
            sync_status: 'ready',
            updated_at: timestamp()
        };
        return state.qps;
    }

    function addSentinel(state, address) {
        if (!address) {
            fail(800, 'missing sentinel address');
        }
        if (state.sentinels.indexOf(address) >= 0) {
            fail(800, 'sentinel ' + address + ' already exists');
        }
        state.sentinels.push(address);
        state.sentinelOutOfSync = false;
        return {};
    }

    function deleteSentinel(state, address) {
        var index = state.sentinels.indexOf(address);
        if (index < 0) {
            fail(800, 'sentinel ' + address + ' not found');
        }
        state.sentinels.splice(index, 1);
        return {};
    }

    function terminalPitr(job) {
        return job.state === 'succeeded' || job.state === 'failed' || job.state === 'cancelled';
    }

    function finishRunningPitrStep(job, status) {
        if (!job.steps.length) {
            return;
        }
        var step = job.steps[job.steps.length - 1];
        if (step.status === 'running') {
            step.status = status || 'done';
            step.finished_at = timestamp();
        }
    }

    function advancePitr(job) {
        if (terminalPitr(job)) {
            return;
        }
        job._progress = Math.min(job._progress + 1, pitrStates.length - 1);
        finishRunningPitrStep(job);
        job.state = pitrStates[job._progress];
        job.updated_at = timestamp();
        if (job.state === 'succeeded') {
            return;
        }
        var name = pitrStepNames[job._progress - 1];
        job.steps.push({name: name, status: 'running', started_at: timestamp(), finished_at: null, error: ''});
    }

    function publicPitrJob(job) {
        return {
            id: job.id,
            product_name: job.product_name,
            group_id: job.group_id,
            server_addr: job.server_addr,
            truncate_ts: job.truncate_ts,
            aof_dir: job.aof_dir || '',
            aof_manifest: job.aof_manifest || '',
            last_segment_file: job.last_segment_file || '',
            last_segment_ts_range: job.last_segment_ts_range || null,
            snapshot_dir: job.snapshot_dir || '',
            restart_kicked: !!job.restart_kicked,
            state: job.state,
            steps: job.steps,
            created_at: job.created_at,
            updated_at: job.updated_at,
            error: job.error || ''
        };
    }

    function createPitr(state, data) {
        if (!data || !data.server_addr) {
            fail(800, 'missing server_addr');
        }
        var found = findServer(state, data.server_addr);
        if (!found) {
            fail(800, 'server ' + data.server_addr + ' is not in the current product');
        }
        var active = state.pitrJobs.some(function (job) { return !terminalPitr(job); });
        if (active) {
            fail(800, 'too many running PITR jobs');
        }
        var id = 'pitr-' + (state.productIndex + 1) + '-' + String(++state.pitrSequence).padStart(3, '0');
        var truncateTs = integer(data.truncate_ts, 'truncate_ts', 1);
        var job = {
            id: id,
            product_name: state.product,
            group_id: found.group.id,
            server_addr: data.server_addr,
            truncate_ts: truncateTs,
            aof_dir: '/var/lib/codis/' + state.product + '/appendonlydir',
            aof_manifest: '/var/lib/codis/' + state.product + '/appendonlydir/appendonly.aof.manifest',
            last_segment_file: 'appendonly.aof.3.incr.aof',
            last_segment_ts_range: {min: truncateTs, max: truncateTs + 60},
            snapshot_dir: '',
            restart_kicked: false,
            state: 'pending',
            steps: [],
            created_at: timestamp(),
            updated_at: timestamp(),
            error: '',
            _progress: 0
        };
        state.pitrJobs.unshift(job);
        return {job_id: id, id: id};
    }

    function requirePitr(state, id) {
        for (var i = 0; i < state.pitrJobs.length; i++) {
            if (state.pitrJobs[i].id === id) {
                return state.pitrJobs[i];
            }
        }
        fail(800, 'PITR job ' + id + ' not found');
    }

    function cancelPitr(state, id) {
        var job = requirePitr(state, id);
        if (!terminalPitr(job)) {
            finishRunningPitrStep(job, 'skipped');
            job.state = 'cancelled';
            job.updated_at = timestamp();
        }
        return {};
    }

    function removePitr(state, id) {
        requirePitr(state, id);
        state.pitrJobs = state.pitrJobs.filter(function (job) { return job.id !== id; });
        return {};
    }

    function terminalMigration(job) {
        return job.state === 'succeeded' || job.state === 'failed' || job.state === 'stopped';
    }

    function migrationFilter() {
        return {
            allow_key_prefix: [],
            block_key_prefix: [],
            allow_db: [],
            block_db: [],
            allow_command: [],
            block_command: ['FLUSHALL', 'FLUSHDB']
        };
    }

    function migrationSlotRanges() {
        return [[0, 340], [341, 682], [683, 1023]];
    }

    function migrationStaticEndpoint(state) {
        var shards = migrationSlotRanges().map(function (range, index) {
            var group = state.groups[index];
            var master = group && group.servers.length ? group.servers[0].server : '';
            return {address: master, slots: [{start: range[0], end: range[1]}]};
        });
        return {
            kind: 'static',
            address: '',
            username: '',
            has_password: false,
            tls: false,
            shards: shards
        };
    }

    function migrationEndpoint(kind, address) {
        return {
            kind: kind,
            address: address,
            username: '',
            has_password: false,
            tls: false,
            shards: []
        };
    }

    function migrationStats(overrides) {
        return angular.extend({
            read_count: 0,
            write_count: 0,
            read_ops: 0,
            write_ops: 0,
            consistent: false,
            rdb_received_bytes: 0,
            rdb_total_bytes: 0,
            aof_lag_bytes: 0,
            shards: 3,
            consistent_shards: 0,
            phase: ''
        }, overrides || {});
    }

    // Mock 的 worker 日志按任务缓存并只追加，保证 offset / next_offset 分页与真实文件一致。
    function migrationLogText(job) {
        if (!job._log) {
            job._log = renderMigrationLog(job);
        }
        return job._log;
    }

    // 真实 worker 在启动后片刻才创建 shake.log，Manager.Logs 在此之前回退读取 console.log。
    // 新建任务的前两次日志请求按 console.log 返回，之后才切换到 shake.log。
    var migrationConsoleLogPolls = 2;

    function migrationConsoleLog(job) {
        if (!job._consoleLog) {
            job._consoleLog = [
                '2026-11-23 19:00:00.010 INFO  codis-shake worker started, job_id=' + job.id,
                '2026-11-23 19:00:00.015 INFO  config loaded from ' + (job.dir || '') + '/spec.json',
                '2026-11-23 19:00:00.210 INFO  redis-shake v3.1.0 starting, stdout/stderr captured to console.log',
                '2026-11-23 19:00:00.480 WARN  shake.log not created yet, waiting for the worker'
            ].join('\n') + '\n';
        }
        return job._consoleLog;
    }

    function renderMigrationLog(job) {
        var source = job.source && job.source.shards && job.source.shards.length ?
            (job.source.shards.length + ' shards') : (job.source && job.source.address ? job.source.address : 'unknown');
        var target = job.target && job.target.shards && job.target.shards.length ?
            (job.target.shards.length + ' shards') : (job.target && job.target.address ? job.target.address : 'unknown');
        var lines = [
            '2026-11-23 19:00:01.120 INFO  redis-shake v3.1.0 starting',
            '2026-11-23 19:00:01.140 INFO  job_id=' + job.id + ' mode=' + job.mode + ' name=' + job.name,
            '2026-11-23 19:00:01.980 INFO  [reader] connect ' + source,
            '2026-11-23 19:00:02.120 INFO  [writer] connect ' + target,
            '2026-11-23 19:00:02.480 INFO  [reader] bgsave triggered, waiting rdb snapshot',
            '2026-11-23 19:02:41.660 INFO  [writer] restore pipeline flushed, entries=1180000',
            '2026-11-23 19:02:42.030 INFO  [coordinator] phase=' + (job.phase || '—') + ', consistent_shards=' +
                (job.stats ? job.stats.consistent_shards : 0) + '/' + (job.stats ? job.stats.shards : 0)
        ];
        if (job.state === 'failed') {
            lines.push('2026-11-23 19:12:44.918 ERROR [writer] receive reply failed. cmd=[RESTORE ...], error=[BUSYKEY Target key name already exists.]');
        }
        return lines.join('\n') + '\n';
    }

    function seedMigrationJobs(state) {
        var prefix = 'dm-' + (state.productIndex + 1) + '-';
        var now = Date.now();
        var created = function (minutes) {
            return new Date(now - minutes * 60000).toISOString();
        };
        var staticEndpoint = migrationStaticEndpoint(state);
        var jobs = [];

        jobs.push({
            id: prefix + '001',
            name: '订单 Redis → Codis 集群',
            mode: 'sync',
            source: migrationEndpoint('standalone', '10.0.20.1:6379'),
            target: staticEndpoint,
            filter: migrationFilter(),
            restore_behavior: 'rewrite',
            empty_db_before_sync: false,
            state: 'running',
            phase: 'receiving rdb',
            stats: migrationStats({
                read_count: 1250000,
                write_count: 1180000,
                read_ops: 41230.5,
                write_ops: 39880.2,
                rdb_received_bytes: 2147483648,
                rdb_total_bytes: 4294967296,
                aof_lag_bytes: 73400320,
                consistent_shards: 1,
                phase: 'receiving rdb'
            }),
            reader: {
                name: 'redis-shake',
                role: 'reader',
                status: 'running',
                address: '10.0.20.1:6379',
                mode: 'sync',
                progress: {rdb_received: 2147483648, rdb_total: 4294967296}
            },
            writer: {
                name: 'redis-shake',
                role: 'writer',
                status: 'running',
                target: '3 shards',
                qps: 39880.2,
                pipeline_count_limit: 1024,
                target_redis_max_qps: 300000
            },
            pid: 51234,
            created_at: created(20),
            started_at: created(19),
            finished_at: '',
            updated_at: created(0),
            error: '',
            exit_code: 0,
            dir: '/tmp/codis-data-migration/jobs/' + prefix + '001',
            log_file: '/tmp/codis-data-migration/jobs/' + prefix + '001/shake.log',
            console_tail: '',
            _phase: 0
        });

        jobs.push({
            id: prefix + '002',
            name: '用户集群 SCAN 迁出',
            mode: 'scan',
            source: staticEndpoint,
            target: migrationEndpoint('standalone', '10.0.30.9:6380'),
            filter: migrationFilter(),
            restore_behavior: 'skip',
            empty_db_before_sync: false,
            state: 'succeeded',
            phase: 'draining',
            stats: migrationStats({
                read_count: 1284506,
                write_count: 1284506,
                read_ops: 0,
                write_ops: 0,
                consistent: true,
                shards: 3,
                consistent_shards: 3,
                phase: 'draining'
            }),
            reader: {
                name: 'redis-shake',
                role: 'reader',
                status: 'done',
                mode: 'scan',
                scanned: 1284506,
                shards: 3
            },
            writer: {
                name: 'redis-shake',
                role: 'writer',
                status: 'done',
                address: '10.0.30.9:6380',
                restored: 1284506
            },
            pid: 51288,
            created_at: created(65),
            started_at: created(64),
            finished_at: created(30),
            updated_at: created(30),
            error: '',
            exit_code: 0,
            dir: '/tmp/codis-data-migration/jobs/' + prefix + '002',
            log_file: '/tmp/codis-data-migration/jobs/' + prefix + '002/shake.log',
            console_tail: '',
            _phase: 0
        });

        jobs.push({
            id: prefix + '003',
            name: '缓存集群 → Codis',
            mode: 'sync',
            source: migrationEndpoint('cluster', '10.0.40.0:6379'),
            target: staticEndpoint,
            filter: migrationFilter(),
            restore_behavior: 'rewrite',
            empty_db_before_sync: true,
            state: 'failed',
            phase: 'syncing aof',
            stats: migrationStats({
                read_count: 842100,
                write_count: 839640,
                read_ops: 42160.7,
                write_ops: 41980.4,
                shards: 3,
                consistent_shards: 2,
                phase: 'syncing aof'
            }),
            reader: {
                name: 'redis-shake',
                role: 'reader',
                status: 'running',
                address: '10.0.40.0:6379',
                mode: 'sync'
            },
            writer: {
                name: 'redis-shake',
                role: 'writer',
                status: 'failed',
                target: '3 shards',
                last_error: 'BUSYKEY Target key name already exists.'
            },
            pid: 51342,
            created_at: created(180),
            started_at: created(179),
            finished_at: created(120),
            updated_at: created(120),
            error: 'receive reply failed. cmd=[RESTORE ...], error=[BUSYKEY Target key name already exists.]',
            exit_code: 1,
            dir: '/tmp/codis-data-migration/jobs/' + prefix + '003',
            log_file: '/tmp/codis-data-migration/jobs/' + prefix + '003/shake.log',
            console_tail: '2026-11-23 19:12:44.918 ERROR [writer] receive reply failed. cmd=[RESTORE "order:864210:state" 0 "..."] error=[BUSYKEY Target key name already exists.]\n' +
                'panic: restore pipeline aborted\n\n' +
                'goroutine 1 [running]:\n' +
                'main.runShake()\n' +
                '\t/opt/redis-shake/cmd/redis-shake/main.go:118 +0x2f4\n',
            _phase: 0
        });

        jobs.push({
            id: prefix + '004',
            name: '历史同步任务',
            mode: 'sync',
            source: migrationEndpoint('standalone', '10.0.20.11:6379'),
            target: staticEndpoint,
            filter: migrationFilter(),
            restore_behavior: 'rewrite',
            empty_db_before_sync: false,
            state: 'stopped',
            phase: 'draining',
            stats: migrationStats({
                read_count: 402180,
                write_count: 402180,
                read_ops: 0,
                write_ops: 0,
                consistent: true,
                shards: 3,
                consistent_shards: 3,
                phase: 'draining'
            }),
            reader: {
                name: 'redis-shake',
                role: 'reader',
                status: 'stopped',
                address: '10.0.20.11:6379',
                mode: 'sync'
            },
            writer: {
                name: 'redis-shake',
                role: 'writer',
                status: 'stopped',
                target: '3 shards'
            },
            pid: 51002,
            created_at: created(480),
            started_at: created(479),
            finished_at: created(360),
            updated_at: created(360),
            error: '',
            exit_code: 0,
            dir: '/tmp/codis-data-migration/jobs/' + prefix + '004',
            log_file: '/tmp/codis-data-migration/jobs/' + prefix + '004/shake.log',
            console_tail: '',
            _phase: 0
        });

        return jobs;
    }

    function publicMigrationJob(job, withConsoleTail) {
        var stats = angular.extend({}, job.stats || {});
        // 后端对 scan / rdb（one-shot）不返回 aof_lag_bytes。
        if (job.mode === 'scan' || job.mode === 'rdb') {
            delete stats.aof_lag_bytes;
        }
        var view = {
            id: job.id,
            name: job.name,
            mode: job.mode,
            source: job.source,
            target: job.target,
            filter: job.filter,
            restore_behavior: job.restore_behavior,
            empty_db_before_sync: !!job.empty_db_before_sync,
            target_redis_max_qps: job.target_redis_max_qps === undefined ? 300000 : job.target_redis_max_qps,
            pipeline_count_limit: job.pipeline_count_limit === undefined ? 1024 : job.pipeline_count_limit,
            state: job.state,
            phase: job.phase,
            stats: stats,
            reader: job.reader || {},
            writer: job.writer || {},
            pid: job.pid,
            created_at: job.created_at,
            started_at: job.started_at || '',
            finished_at: job.finished_at || '',
            updated_at: job.updated_at,
            error: job.error || '',
            exit_code: job.exit_code || 0,
            dir: job.dir || '',
            log_file: job.log_file || '',
            console_file: job.console_file || (job.dir ? job.dir + '/console.log' : '')
        };
        if (withConsoleTail) {
            view.console_tail = job.console_tail || '';
        }
        return view;
    }

    function advanceMigration(job) {
        if (terminalMigration(job)) {
            return;
        }
        if (job.state === 'pending' || job.state === 'starting') {
            job.state = 'running';
            job.started_at = job.started_at || timestamp();
        }
        if (job.state !== 'running') {
            return;
        }
        if (job._phase < 0) {
            // 新建任务先展示一次 hand shaking，再进入全量阶段。
            job._phase = 0;
            job.updated_at = timestamp();
            return;
        }
        var phases = ['receiving rdb', 'syncing rdb', 'syncing aof'];
        job.phase = phases[job._phase % phases.length];
        job._phase++;
        job.stats = job.stats || migrationStats({});
        job.stats.phase = job.phase;
        job.stats.read_count = (Number(job.stats.read_count) || 0) + 96000 + job._phase * 2400;
        job.stats.write_count = (Number(job.stats.write_count) || 0) + 92000 + job._phase * 2100;
        job.stats.read_ops = (Number(job.stats.read_ops) || 38000) + 480 + (job._phase % 4) * 90;
        job.stats.write_ops = (Number(job.stats.write_ops) || 36000) + 460 + (job._phase % 3) * 85;
        job.stats.rdb_received_bytes = Math.min(
            Number(job.stats.rdb_total_bytes) || 0,
            (Number(job.stats.rdb_received_bytes) || 0) + 268435456
        );
        job.stats.aof_lag_bytes = Math.max(0, (Number(job.stats.aof_lag_bytes) || 0) - 12582912);
        job.stats.consistent = job.stats.aof_lag_bytes === 0;
        job.stats.consistent_shards = job.stats.consistent ?
            (Number(job.stats.shards) || 3) :
            Math.min((Number(job.stats.shards) || 3) - 1, (Number(job.stats.consistent_shards) || 0) + 1);
        job._log = migrationLogText(job) + timestamp() + ' INFO  [coordinator] phase=' + job.phase +
            ', read_count=' + job.stats.read_count + ', write_count=' + job.stats.write_count + '\n';
        job.updated_at = timestamp();
    }

    function requireMigration(state, id) {
        for (var i = 0; i < state.migrationJobs.length; i++) {
            if (state.migrationJobs[i].id === id) {
                return state.migrationJobs[i];
            }
        }
        fail(800, '数据迁移任务 ' + id + ' 不存在');
    }

    function isHostPort(value) {
        return /^(\[[^\]]+\]|[^:\s]+):\d+$/.test(String(value === undefined || value === null ? '' : value).trim());
    }

    // 与 pkg/migration.ValidateSlotCoverage 一致：范围合法、不重叠、完整覆盖 0-1023。
    function migrationSlotCoverageError(shards) {
        var owner = [];
        for (var sid = 0; sid < 1024; sid++) {
            owner.push(-1);
        }
        for (var i = 0; i < shards.length; i++) {
            if (!shards[i].slots.length) {
                return 'shard ' + shards[i].address + ' has no slot range';
            }
            for (var r = 0; r < shards[i].slots.length; r++) {
                var range = shards[i].slots[r];
                for (var slot = range.start; slot <= range.end; slot++) {
                    if (owner[slot] !== -1) {
                        return 'slot ' + slot + ' is owned by both ' + shards[owner[slot]].address +
                            ' and ' + shards[i].address;
                    }
                    owner[slot] = i;
                }
            }
        }
        for (var missing = 0; missing < owner.length; missing++) {
            if (owner[missing] === -1) {
                return 'slot ' + missing + ' is not covered by any shard';
            }
        }
        return '';
    }

    function validateMigrationSide(endpoint, label, isTarget) {
        if (!endpoint || typeof endpoint !== 'object') {
            fail(800, label + '配置不能为空');
        }
        if (endpoint.kind === 'codis') {
            var scope = endpoint.codis_scope || 'current';
            if (scope === 'current') {
                return;
            }
            if (scope !== 'manual') {
                fail(800, label + '的 codis_scope 必须是 current 或 manual');
            }
            if (!Array.isArray(endpoint.shards) || !endpoint.shards.length) {
                fail(800, label + '需要填写至少一个 Codis 分片');
            }
            var seen = {};
            var shards = endpoint.shards.map(function (shard, index) {
                var address = String(shard && shard.address || '').trim();
                if (!address) {
                    fail(800, label + '分片缺少地址');
                }
                if (!isHostPort(address)) {
                    fail(800, label + '分片 ' + index + ': ' + address + ' 不是 host:port');
                }
                if (seen[address]) {
                    fail(800, label + ': 分片地址重复 ' + address);
                }
                seen[address] = true;
                if (!isTarget) {
                    return {address: address, slots: []};
                }
                if (shard.slot_start === undefined || shard.slot_start === null ||
                    shard.slot_end === undefined || shard.slot_end === null) {
                    fail(800, label + '分片需要 slot_start 与 slot_end');
                }
                var start = integer(shard.slot_start, 'slot_start', 0, 1023);
                var end = integer(shard.slot_end, 'slot_end', 0, 1023);
                if (start > end) {
                    fail(800, label + '分片的 slot_start 不能大于 slot_end');
                }
                return {address: address, slots: [{start: start, end: end}]};
            });
            if (isTarget) {
                var coverage = migrationSlotCoverageError(shards);
                if (coverage) {
                    fail(800, label + ': ' + coverage);
                }
            }
            return;
        }
        if (endpoint.kind !== 'standalone' && endpoint.kind !== 'cluster') {
            fail(800, label + '类型必须是 standalone、cluster 或 codis');
        }
        if (!String(endpoint.address || '').trim()) {
            fail(800, label + '地址不能为空');
        }
        if (!isHostPort(endpoint.address)) {
            fail(800, label + '地址 ' + endpoint.address + ' 不是 host:port');
        }
    }

    function migrationJobEndpoint(state, endpoint) {
        if (endpoint.kind === 'codis') {
            var scope = endpoint.codis_scope || 'current';
            if (scope === 'current') {
                return migrationStaticEndpoint(state);
            }
            return {
                kind: 'static',
                address: '',
                username: endpoint.username || '',
                has_password: !!endpoint.password,
                tls: !!endpoint.tls,
                shards: (endpoint.shards || []).map(function (shard) {
                    var view = {address: String(shard.address || '')};
                    // 与后端一致：源端 manual 分片只有 address；目标端才带 slot 范围。
                    if (shard.slot_start !== undefined && shard.slot_start !== null &&
                        shard.slot_end !== undefined && shard.slot_end !== null) {
                        view.slots = [{start: Number(shard.slot_start), end: Number(shard.slot_end)}];
                    }
                    return view;
                })
            };
        }
        return {
            kind: endpoint.kind,
            address: String(endpoint.address || ''),
            username: endpoint.username || '',
            has_password: !!endpoint.password,
            tls: !!endpoint.tls,
            shards: []
        };
    }

    function createMigration(state, data) {
        if (!data || typeof data !== 'object') {
            fail(800, '请求内容不能为空');
        }
        var name = String(data.name === undefined || data.name === null ? '' : data.name).trim();
        if (!name) {
            fail(800, '任务名称不能为空');
        }
        if (name.length > 64) {
            fail(800, '任务名称不能超过 64 个字符');
        }
        var mode = String(data.mode === undefined || data.mode === null ? '' : data.mode).trim().toLowerCase() || 'sync';
        if (['sync', 'scan', 'rdb'].indexOf(mode) < 0) {
            fail(800, '迁移模式必须是 sync、scan 或 rdb');
        }
        var restore = String(data.restore_behavior || 'rewrite').trim().toLowerCase() || 'rewrite';
        if (['rewrite', 'skip', 'panic'].indexOf(restore) < 0) {
            fail(800, 'restore_behavior 必须是 rewrite、skip 或 panic');
        }
        validateMigrationSide(data.source, '源端', false);
        validateMigrationSide(data.target, '目标端', true);
        if (data.source.kind !== 'codis' && data.target.kind !== 'codis' &&
            String(data.source.address || '').trim().toLowerCase() ===
            String(data.target.address || '').trim().toLowerCase()) {
            fail(800, '源端与目标端地址相同，无法迁移');
        }
        var sequence = ++state.migrationSequence;
        var id = 'dm-' + (state.productIndex + 1) + '-' + String(sequence).padStart(3, '0');
        var source = migrationJobEndpoint(state, data.source);
        var target = migrationJobEndpoint(state, data.target);
        var now = timestamp();
        var qps = Number(data.target_redis_max_qps) || 0;
        var pipeline = Number(data.pipeline_count_limit) || 0;
        var job = {
            id: id,
            name: name,
            mode: mode,
            source: source,
            target: target,
            filter: data.filter || migrationFilter(),
            restore_behavior: restore,
            empty_db_before_sync: !!data.empty_db_before_sync,
            target_redis_max_qps: qps,
            pipeline_count_limit: pipeline,
            state: 'running',
            phase: 'hand shaking',
            stats: migrationStats({
                rdb_total_bytes: 4294967296,
                shards: Math.max(1, target.shards.length || 1),
                phase: 'hand shaking'
            }),
            reader: {
                name: 'redis-shake',
                role: 'reader',
                status: 'starting',
                source: source.address || (source.shards.length + ' shards')
            },
            writer: {
                name: 'redis-shake',
                role: 'writer',
                status: 'starting',
                target: target.address || (target.shards.length + ' shards'),
                target_redis_max_qps: qps || 300000,
                pipeline_count_limit: pipeline || 1024
            },
            pid: 52000 + sequence,
            created_at: now,
            started_at: now,
            finished_at: '',
            updated_at: now,
            error: '',
            exit_code: 0,
            dir: '/tmp/codis-data-migration/jobs/' + id,
            log_file: '/tmp/codis-data-migration/jobs/' + id + '/shake.log',
            console_file: '/tmp/codis-data-migration/jobs/' + id + '/console.log',
            console_tail: '',
            _phase: -1,
            // 新建任务模拟 shake.log 尚未落盘，前几次日志请求由 console.log 兜底。
            _logPolls: 0
        };
        if (mode !== 'sync') {
            delete job.stats.aof_lag_bytes;
        }
        state.migrationJobs.unshift(job);
        return {id: id};
    }

    function stopMigration(state, id) {
        var job = requireMigration(state, id);
        if (!terminalMigration(job)) {
            job.state = 'stopped';
            job.phase = 'draining';
            job.stats = job.stats || migrationStats({});
            job.stats.phase = 'draining';
            job._log = migrationLogText(job) + timestamp() + ' WARN  [coordinator] stop requested, draining pipeline\n';
            job.pid = 0;
            job.exit_code = 0;
            job.finished_at = timestamp();
            job.updated_at = timestamp();
        }
        return 'OK';
    }

    function removeMigration(state, id) {
        var job = requireMigration(state, id);
        if (!terminalMigration(job)) {
            fail(800, '只能删除已结束（succeeded / failed / stopped）的迁移任务');
        }
        state.migrationJobs = state.migrationJobs.filter(function (item) { return item.id !== id; });
        return 'OK';
    }

    function migrationLog(state, id, query) {
        var job = requireMigration(state, id);
        // 与真实 Manager.Logs 一致：shake.log 已存在则优先读它，否则回退 console.log，
        // 新建任务的前 migrationConsoleLogPolls 次请求命中 console.log。
        var consoleVisible = typeof job._logPolls === 'number' &&
            job._logPolls < migrationConsoleLogPolls;
        if (typeof job._logPolls === 'number') {
            job._logPolls++;
        }
        var file = consoleVisible ? 'console.log' : 'shake.log';
        var text = consoleVisible ? migrationConsoleLog(job) : migrationLogText(job);
        var size = text.length;
        var maxBytes = queryInt(query, 'max_bytes', 0);
        if (maxBytes <= 0) {
            maxBytes = 65536;
        }
        maxBytes = Math.min(maxBytes, 1048576);
        var offset = queryInt(query, 'offset', 0);
        if (offset < 0 || offset > size) {
            // 与 Dashboard 一致：offset 越界时从头重放，并把 offset 归零返回。
            offset = 0;
        }
        var end = Math.min(size, offset + maxBytes);
        return {
            file: file,
            text: text.slice(offset, end),
            offset: offset,
            next_offset: end,
            size: size,
            truncated: end < size
        };
    }

    function migrationTopology(state) {
        var groups = state.groups.map(function (group) {
            var slots = [];
            var start = null;
            var previous = null;
            state.slots.forEach(function (slot) {
                if (slot.group_id !== group.id) {
                    return;
                }
                if (start === null) {
                    start = slot.id;
                } else if (slot.id !== previous + 1) {
                    slots.push({start: start, end: previous});
                    start = slot.id;
                }
                previous = slot.id;
            });
            if (start !== null) {
                slots.push({start: start, end: previous});
            }
            return {
                group_id: group.id,
                master: group.servers.length ? group.servers[0].server : '',
                slot_count: slots.reduce(function (total, range) {
                    return total + range.end - range.start + 1;
                }, 0),
                slots: slots
            };
        });
        var known = {};
        state.groups.forEach(function (group) { known[group.id] = true; });
        var unassigned = state.slots.filter(function (slot) { return !known[slot.group_id]; }).length;
        var partial = scenario === 'partial';
        var missingMaster = groups.filter(function (group) {
            return !String(group.master || '').trim();
        }).length;
        return {
            groups: groups,
            unassigned_slots: unassigned,
            slot_actions: partial ? 8 : 0,
            complete: !partial && unassigned === 0 && groups.length > 0 && missingMaster === 0,
            online: true
        };
    }

    function rdbOptions(options) {
        options = options || {};
        return {
            top_n: integer(options.top_n === undefined ? 20 : options.top_n, 'top_n', 1, 100),
            prefix_separators: Array.isArray(options.prefix_separators) && options.prefix_separators.length ? options.prefix_separators.slice() : [':'],
            max_depth: integer(options.max_depth === undefined ? 3 : options.max_depth, 'max_depth', 0, 16),
            regex: options.regex || '',
            include_expired: !!options.include_expired
        };
    }

    function startRDB(state, source, options, fileSize) {
        var active = Object.keys(state.rdbJobs).some(function (id) {
            var status = state.rdbJobs[id].status;
            return status === 'queued' || status === 'running';
        });
        if (active) {
            fail(800, 'too many running RDB analysis jobs');
        }
        var id = 'rdb-' + (state.productIndex + 1) + '-' + String(++state.rdbSequence).padStart(3, '0');
        state.rdbJobs[id] = {
            id: id,
            created_at: timestamp(),
            updated_at: timestamp(),
            status: 'queued',
            source: source,
            options: rdbOptions(options),
            file_size: fileSize || 268435456,
            bytes_read: 0,
            objects_read: 0,
            db_count: 0,
            total_size: 0,
            error: '',
            _polls: 0,
            _result: null
        };
        return {id: id};
    }

    function makeRDBResult(job) {
        var total = job.total_size;
        var typeSummary = [
            {name: 'string', size: Math.round(total * 0.42), size_readable: readable(total * 0.42), key_count: 734208},
            {name: 'hash', size: Math.round(total * 0.25), size_readable: readable(total * 0.25), key_count: 216084},
            {name: 'list', size: Math.round(total * 0.13), size_readable: readable(total * 0.13), key_count: 98420},
            {name: 'zset', size: Math.round(total * 0.12), size_readable: readable(total * 0.12), key_count: 142608},
            {name: 'set', size: Math.round(total * 0.08), size_readable: readable(total * 0.08), key_count: 93000}
        ];
        var dbSummary = [
            {name: 'db0', db: 0, size: Math.round(total * 0.68), size_readable: readable(total * 0.68), key_count: 873456},
            {name: 'db1', db: 1, size: Math.round(total * 0.22), size_readable: readable(total * 0.22), key_count: 282640},
            {name: 'db2', db: 2, size: Math.round(total * 0.10), size_readable: readable(total * 0.10), key_count: 128224}
        ];
        var prefixSummary = [
            {name: 'order', size: Math.round(total * 0.38), size_readable: readable(total * 0.38), key_count: 402180},
            {name: 'user', size: Math.round(total * 0.27), size_readable: readable(total * 0.27), key_count: 318420},
            {name: 'session', size: Math.round(total * 0.20), size_readable: readable(total * 0.20), key_count: 391800},
            {name: 'cache', size: Math.round(total * 0.15), size_readable: readable(total * 0.15), key_count: 171920}
        ];
        var bigKeys = [
            {db: 0, key: 'order:20261112:items', type: 'list', size: 18874368, size_readable: readable(18874368), element_count: 148203, encoding: 'listpack'},
            {db: 0, key: 'user:10086:profile', type: 'hash', size: 12582912, size_readable: readable(12582912), element_count: 48210, encoding: 'hashtable'},
            {db: 1, key: 'cache:hot:ranking', type: 'zset', size: 8388608, size_readable: readable(8388608), element_count: 68210, encoding: 'skiplist'},
            {db: 2, key: 'session:batch:7642', type: 'set', size: 5242880, size_readable: readable(5242880), element_count: 38420, encoding: 'hashtable'}
        ];
        var hotKeys = [
            {db: 0, key: 'order:864210:state', type: 'string', size: 2048, size_readable: readable(2048), element_count: 1, encoding: 'raw', frequency: 248},
            {db: 0, key: 'user:10086:session', type: 'string', size: 4096, size_readable: readable(4096), element_count: 1, encoding: 'raw', frequency: 192},
            {db: 1, key: 'cache:hot:ranking', type: 'zset', size: 8388608, size_readable: readable(8388608), element_count: 68210, encoding: 'skiplist', frequency: 127},
            {db: 2, key: 'session:current', type: 'hash', size: 65536, size_readable: readable(65536), element_count: 86, encoding: 'listpack', frequency: 93}
        ];
        var flamegraph = {
            name: 'root', value: total, children: dbSummary.map(function (db, dbIndex) {
                return {
                    name: db.name,
                    value: db.size,
                    children: prefixSummary.map(function (prefix, prefixIndex) {
                        return {
                            name: prefix.name,
                            value: Math.round(db.size * [0.42, 0.28, 0.18, 0.12][prefixIndex]),
                            children: [{
                                name: typeSummary[(dbIndex + prefixIndex) % typeSummary.length].name,
                                value: Math.round(db.size * [0.42, 0.28, 0.18, 0.12][prefixIndex] * 0.7)
                            }]
                        };
                    })
                };
            })
        };
        return {
            type_summary: typeSummary,
            db_summary: dbSummary,
            prefix_summary: prefixSummary,
            top_big_keys: bigKeys,
            top_hot_keys: hotKeys,
            flamegraph: flamegraph
        };
    }

    function advanceRDB(job) {
        if (job.status === 'done' || job.status === 'error' || job.status === 'canceled') {
            return;
        }
        job._polls++;
        job.status = 'running';
        var progress = Math.min(1, job._polls / 5);
        job.bytes_read = Math.min(job.file_size, Math.floor(job.file_size * progress));
        job.objects_read = Math.floor(1284320 * progress);
        job.db_count = progress < 0.2 ? 0 : (progress < 0.6 ? 2 : 3);
        job.total_size = Math.floor(187654320 * progress);
        job.updated_at = timestamp();
        if (progress === 1) {
            job.status = 'done';
            job._result = makeRDBResult(job);
        }
    }

    function requireRDB(state, id) {
        if (!state.rdbJobs[id]) {
            fail(800, 'RDB analysis job ' + id + ' not found');
        }
        return state.rdbJobs[id];
    }

    function publicRDBJob(job) {
        var result = job._result || {
            type_summary: [], db_summary: [], prefix_summary: [],
            top_big_keys: [], top_hot_keys: [], flamegraph: null
        };
        var progress = job.file_size ? Math.min(100, Math.floor(job.bytes_read * 100 / job.file_size)) : 0;
        return {
            id: job.id,
            created_at: job.created_at,
            updated_at: job.updated_at,
            status: job.status,
            source: job.source,
            options: job.options,
            file_size: job.file_size,
            bytes_read: job.bytes_read,
            objects_read: job.objects_read,
            db_count: job.db_count,
            total_size: job.total_size,
            progress: progress,
            error: job.error || '',
            summary: {
                source: job.source,
                status: job.status,
                progress: progress,
                file_size: job.file_size,
                file_size_readable: readable(job.file_size),
                bytes_read: job.bytes_read,
                bytes_read_readable: readable(job.bytes_read),
                key_count: job.objects_read,
                objects_read: job.objects_read,
                db_count: job.db_count,
                total_size: job.total_size,
                total_size_readable: readable(job.total_size),
                size_readable: readable(job.total_size)
            },
            type_summary: result.type_summary,
            db_summary: result.db_summary,
            prefix_summary: result.prefix_summary,
            top_big_keys: result.top_big_keys,
            top_hot_keys: result.top_hot_keys,
            type_stats: result.type_summary,
            db_stats: result.db_summary,
            prefix_stats: result.prefix_summary,
            big_keys: result.top_big_keys,
            hot_keys: result.top_hot_keys,
            flamegraph: result.flamegraph
        };
    }

    function cancelRDB(state, id) {
        var job = requireRDB(state, id);
        if (job.status !== 'done' && job.status !== 'error' && job.status !== 'canceled') {
            job.status = 'canceled';
            job.updated_at = timestamp();
        }
        return {};
    }

    function removeRDB(state, id) {
        requireRDB(state, id);
        delete state.rdbJobs[id];
        return {};
    }

    function dispatch(method, product, path, data, query) {
        if (method === 'GET' && path === '/list') {
            return products.slice();
        }
        var state = cluster(product);
        var routes = [
            ['GET', '/topom', function () { return overview(state); }],
            ['GET', '/api/topom/stats/{xauth}', function () { return buildStats(state); }],
            ['GET', '/api/topom/slots/{xauth}', function () { return state.slots; }],
            ['GET', '/api/topom/xping/{xauth}', function () { return 'PONG'; }],
            ['PUT', '/api/topom/reload/{xauth}', function () { return overview(state); }],

            ['PUT', '/api/topom/proxy/create/{xauth}/:address', function (p) { return createProxy(state, p.address); }],
            ['PUT', '/api/topom/proxy/remove/{xauth}/:token/:force', function (p) { flag(p.force); return removeProxy(state, p.token); }],
            ['PUT', '/api/topom/proxy/remove/{xauth}/:token', function (p) { return removeProxy(state, p.token); }],
            ['PUT', '/api/topom/proxy/reinit/{xauth}/:token', function (p) {
                requireProxy(state, p.token);
                delete state.proxyIssues[p.token];
                return {};
            }],
            ['PUT', '/api/topom/proxy/online/{xauth}/:address', function (p) {
                for (var i = 0; i < state.proxies.length; i++) {
                    if (state.proxies[i].proxy_addr === p.address) {
                        delete state.proxyIssues[state.proxies[i].token];
                        return {};
                    }
                }
                var created = createProxy(state, p.address);
                delete state.proxyIssues[created.token];
                return created;
            }],

            ['PUT', '/api/topom/group/create/{xauth}/:gid', function (p) { return createGroup(state, p.gid); }],
            ['PUT', '/api/topom/group/remove/{xauth}/:gid', function (p) { return removeGroup(state, p.gid); }],
            ['PUT', '/api/topom/group/resync/{xauth}/:gid', function (p) { return resyncGroup(state, requireGroup(state, p.gid)); }],
            ['PUT', '/api/topom/group/resync-all/{xauth}', function () {
                state.groups.forEach(function (group) { resyncGroup(state, group); });
                return {};
            }],
            ['PUT', '/api/topom/group/add/{xauth}/:gid/:address/:datacenter', function (p) { return addGroupServer(state, p.gid, p.address, p.datacenter); }],
            ['PUT', '/api/topom/group/add/{xauth}/:gid/:address', function (p) { return addGroupServer(state, p.gid, p.address, ''); }],
            ['PUT', '/api/topom/group/del/{xauth}/:gid/:address', function (p) { return deleteGroupServer(state, p.gid, p.address); }],
            ['PUT', '/api/topom/group/promote/{xauth}/:gid/:address', function (p) { return promoteGroupServer(state, p.gid, p.address); }],
            ['PUT', '/api/topom/group/replica-groups/{xauth}/:gid/:address/:value', function (p) { return setReplicaGroup(state, p.gid, p.address, p.value); }],
            ['PUT', '/api/topom/group/replica-groups-all/{xauth}/:value', function (p) {
                var enabled = flag(p.value);
                state.groups.forEach(function (group) {
                    group.servers.forEach(function (server) { server.replica_group = enabled; });
                });
                return {};
            }],
            ['PUT', '/api/topom/group/action/create/{xauth}/:address', function (p) { return setSyncAction(state, p.address, true); }],
            ['PUT', '/api/topom/group/action/remove/{xauth}/:address', function (p) { return setSyncAction(state, p.address, false); }],

            ['PUT', '/api/topom/sentinels/add/{xauth}/:address', function (p) { return addSentinel(state, p.address); }],
            ['PUT', '/api/topom/sentinels/del/{xauth}/:address/:force', function (p) { flag(p.force); return deleteSentinel(state, p.address); }],
            ['PUT', '/api/topom/sentinels/del/{xauth}/:address', function (p) { return deleteSentinel(state, p.address); }],
            ['PUT', '/api/topom/sentinels/resync-all/{xauth}', function () { state.sentinelOutOfSync = false; return {}; }],

            ['PUT', '/api/topom/slots/action/create-some/{xauth}/:from/:to/:count', function (p) { return createSlotsSome(state, p.from, p.to, p.count); }],
            ['PUT', '/api/topom/slots/action/create-range/{xauth}/:beg/:end/:gid', function (p) { return createSlotsRange(state, p.beg, p.end, p.gid); }],
            ['PUT', '/api/topom/slots/action/create/{xauth}/:sid/:gid', function (p) {
                var slot = requireSlot(state, p.sid);
                slot.action = {index: 0, state: 'pending', target_id: requireGroup(state, p.gid).id};
                return {};
            }],
            ['PUT', '/api/topom/slots/action/remove/{xauth}/:sid', function (p) { requireSlot(state, p.sid).action = {}; return {}; }],
            ['PUT', '/api/topom/slots/action/disabled/{xauth}/:value', function (p) { state.slotAction.disabled = flag(p.value); return {}; }],
            ['PUT', '/api/topom/slots/action/interval/{xauth}/:value', function (p) {
                state.slotAction.interval = integer(p.value, 'slot action interval', 0);
                return {};
            }],
            ['PUT', '/api/topom/slots/rebalance/{xauth}/:confirm', function (p) {
                var confirmed = flag(p.confirm);
                var plan = rebalancePlan(state);
                return confirmed ? applyRebalance(state, plan) : plan;
            }],
            ['PUT', '/api/topom/slots/assign/{xauth}/offline', function (p, body) { return assignSlots(state, body, true); }],
            ['PUT', '/api/topom/slots/assign/{xauth}', function (p, body) { return assignSlots(state, body, false); }],

            ['GET', '/api/topom/acl/{xauth}', function () { return aclView(state); }],
            ['PUT', '/api/topom/acl/{xauth}', function (p, body) { return replaceACL(state, body); }],
            ['PUT', '/api/topom/acl/sync/{xauth}', function () { state.acl.sync_status = 'ready'; return aclView(state); }],
            ['GET', '/api/topom/proxy/qps-limit/{xauth}', function () { return state.qps; }],
            ['PUT', '/api/topom/proxy/qps-limit/{xauth}', function (p, body) { return saveQPSLimit(state, body); }],

            ['GET', '/api/topom/pitr/jobs/{xauth}', function () {
                state.pitrJobs.forEach(advancePitr);
                return state.pitrJobs.map(publicPitrJob);
            }],
            ['PUT', '/api/topom/pitr/create/{xauth}', function (p, body) { return createPitr(state, body); }],
            ['PUT', '/api/topom/pitr/cancel/{xauth}/:id', function (p) { return cancelPitr(state, p.id); }],
            ['PUT', '/api/topom/pitr/remove/{xauth}/:id', function (p) { return removePitr(state, p.id); }],
            ['GET', '/api/topom/pitr/{xauth}/:id', function (p) {
                var job = requirePitr(state, p.id);
                advancePitr(job);
                return publicPitrJob(job);
            }],

            ['GET', '/api/topom/data-migration/jobs/{xauth}', function () {
                state.migrationJobs.forEach(advanceMigration);
                return state.migrationJobs.map(function (job) { return publicMigrationJob(job, false); });
            }],
            ['PUT', '/api/topom/data-migration/create/{xauth}', function (p, body) { return createMigration(state, body); }],
            ['GET', '/api/topom/data-migration/get/{xauth}/:id', function (p) {
                return publicMigrationJob(requireMigration(state, p.id), true);
            }],
            ['PUT', '/api/topom/data-migration/stop/{xauth}/:id', function (p) { return stopMigration(state, p.id); }],
            ['PUT', '/api/topom/data-migration/remove/{xauth}/:id', function (p) { return removeMigration(state, p.id); }],
            ['GET', '/api/topom/data-migration/log/{xauth}/:id', function (p, body, query) {
                return migrationLog(state, p.id, query);
            }],
            ['GET', '/api/topom/data-migration/topology/codis/{xauth}', function () { return migrationTopology(state); }],

            ['PUT', '/api/topom/rdb-analysis/start/{xauth}', function (p, body) {
                if (!body || !body.path) { fail(800, 'missing RDB analysis path'); }
                return startRDB(state, 'workspace:' + body.path, body.options, 268435456);
            }],
            ['POST', '/api/topom/rdb-analysis/upload/{xauth}', function () {
                return startRDB(state, 'upload:browser-upload.rdb', null, 268435456);
            }],
            ['PUT', '/api/topom/rdb-analysis/remote-fetch/{xauth}', function (p, body) {
                if (!body || !findServer(state, body.server_addr)) { fail(800, 'server_addr is not in the current product'); }
                return startRDB(state, 'remote:' + body.server_addr, body.options, 314572800);
            }],
            ['PUT', '/api/topom/rdb-analysis/cancel/{xauth}/:id', function (p) { return cancelRDB(state, p.id); }],
            ['PUT', '/api/topom/rdb-analysis/remove/{xauth}/:id', function (p) { return removeRDB(state, p.id); }],
            ['GET', '/api/topom/rdb-analysis/{xauth}/:id', function (p) {
                var job = requireRDB(state, p.id);
                advanceRDB(job);
                return publicRDBJob(job);
            }]
        ];

        for (var i = 0; i < routes.length; i++) {
            if (routes[i][0] !== method) {
                continue;
            }
            var values = match(path, routes[i][1]);
            if (values) {
                return routes[i][2](values, data, query);
            }
        }
        fail(404, 'unsupported Mock API route: ' + method + ' ' + path);
    }

    function assignSlots(state, mappings, offline) {
        if (!Array.isArray(mappings)) {
            fail(800, 'slot assignment requires an array');
        }
        mappings.forEach(function (mapping) {
            var slot = requireSlot(state, mapping.id);
            slot.group_id = offline ? 0 : requireGroup(state, mapping.group_id).id;
            slot.action = {};
        });
        return {};
    }

    function guard(method, path) {
        if (scenario === 'error') {
            fail(500, 'mock dashboard request failed (scenario=error)');
        }
        if (scenario === 'forbidden') {
            fail(403, 'current account is forbidden by scenario=forbidden');
        }
        var preview = /\/slots\/rebalance\/\{xauth\}\/0$/.test(path);
        if (scenario === 'submit-error' && method !== 'GET' && !preview) {
            fail(500, 'mock submit failed; no state was changed (scenario=submit-error)');
        }
    }

    this.list = function () {
        return delayed(function () {
            guard('GET', '/list');
            return products.slice();
        }, 500);
    };

    this.request = function (method, product, path, data, config) {
        var normalizedMethod = String(method || 'GET').toUpperCase();
        var normalizedPath = canonicalPath(path);
        var query = new URLSearchParams(String(path || '').split('#')[0].split('?')[1] || '');
        var delay = normalizedMethod === 'GET' ? (normalizedPath === '/topom' || normalizedPath === '/list' ? 500 : 250) : 900;
        return delayed(function () {
            guard(normalizedMethod, normalizedPath);
            return dispatch(normalizedMethod, product, normalizedPath, data, query);
        }, delay);
    };
}]);
