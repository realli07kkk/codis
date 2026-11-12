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

    function dispatch(method, product, path, data) {
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
                return routes[i][2](values, data);
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
        var delay = normalizedMethod === 'GET' ? (normalizedPath === '/topom' || normalizedPath === '/list' ? 500 : 250) : 900;
        return delayed(function () {
            guard(normalizedMethod, normalizedPath);
            return dispatch(normalizedMethod, product, normalizedPath, data);
        }, delay);
    };
}]);
