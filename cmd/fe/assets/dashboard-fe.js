'use strict';

var dashboard = angular.module('dashboard-fe', []);

dashboard.config(['$httpProvider', function ($httpProvider) {
    $httpProvider.defaults.useXDomain = true;
    delete $httpProvider.defaults.headers.common['X-Requested-With'];
}]);

dashboard.run(['$window', function ($window) {
    if ($window.Highcharts) {
        $window.Highcharts.setOptions({
            global: {useUTC: false},
            time: {useUTC: false},
            exporting: {enabled: false},
            credits: {enabled: false},
            lang: {
                thousandsSep: ',',
                noData: '暂无监控数据'
            }
        });
    }
}]);

function genXAuth(name) {
    return sha256('Codis-XAuth-[' + name + ']').substring(0, 32);
}

function isValidInput(value) {
    return value !== undefined && value !== null && String(value).trim() !== '' && value !== 'NA';
}

function humanSize(size) {
    if (size === undefined || size === null || size === '' || isNaN(Number(size))) {
        return '—';
    }
    var value = Number(size);
    var negative = value < 0;
    value = Math.abs(value);
    var units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    var index = 0;
    while (value >= 1024 && index < units.length - 1) {
        value /= 1024;
        index++;
    }
    var digits = index === 0 ? 0 : (value >= 100 ? 1 : 2);
    return (negative ? '-' : '') + value.toFixed(digits) + ' ' + units[index];
}

function padInt2Str(value, size) {
    var text = String(value);
    while (text.length < size) {
        text = '0' + text;
    }
    return text;
}

dashboard.factory('Format', [function () {
    function number(value, digits) {
        if (value === undefined || value === null || value === '' || isNaN(Number(value))) {
            return '—';
        }
        var numeric = Number(value);
        return numeric.toLocaleString('zh-CN', {
            minimumFractionDigits: digits || 0,
            maximumFractionDigits: digits || 0
        });
    }

    function date(value, withSeconds) {
        if (!value) {
            return '—';
        }
        var parsed = angular.isDate(value) ? value : new Date(value);
        if (isNaN(parsed.getTime())) {
            return String(value);
        }
        var text = parsed.getFullYear() + '-' + padInt2Str(parsed.getMonth() + 1, 2) + '-' +
            padInt2Str(parsed.getDate(), 2) + ' ' + padInt2Str(parsed.getHours(), 2) + ':' +
            padInt2Str(parsed.getMinutes(), 2);
        return withSeconds ? text + ':' + padInt2Str(parsed.getSeconds(), 2) : text;
    }

    return {
        number: number,
        bytes: humanSize,
        date: date,
        time: function (value) { return date(value, true).slice(11); },
        percent: function (value) { return value === undefined || value === null ? '—' : number(value, 1) + '%'; },
        placeholder: '—'
    };
}]);

dashboard.factory('Preferences', ['$window', function ($window) {
    var prefix = 'codis.console.';
    function read(key, fallback) {
        try {
            var raw = $window.localStorage.getItem(prefix + key);
            return raw === null ? angular.copy(fallback) : angular.fromJson(raw);
        } catch (ignore) {
            return angular.copy(fallback);
        }
    }
    function write(key, value) {
        try {
            $window.localStorage.setItem(prefix + key, angular.toJson(value));
        } catch (ignore) {
            // Storage can be unavailable in private or restricted browser contexts.
        }
        return value;
    }
    return {get: read, set: write};
}]);

dashboard.factory('Activity', ['Preferences', function (Preferences) {
    function list(product, target) {
        var items = Preferences.get('activities', []);
        return items.filter(function (item) {
            return (!product || item.product === product) && (!target || item.target === target);
        });
    }
    function add(product, action, target, result) {
        var items = Preferences.get('activities', []);
        items.unshift({
            id: Date.now() + '-' + Math.random().toString(16).slice(2),
            product: product,
            action: action,
            target: target || '集群',
            result: result || 'success',
            time: new Date().toISOString()
        });
        Preferences.set('activities', items.slice(0, 120));
    }
    return {list: list, add: add};
}]);

dashboard.factory('Router', ['$window', '$rootScope', '$timeout', function ($window, $rootScope, $timeout) {
    var current = {product: '', page: 'overview', id: ''};
    var scrollPositions = {};
    var knownPages = {
        overview: true,
        proxies: true,
        groups: true,
        slots: true,
        sentinels: true,
        security: true,
        analysis: true,
        recovery: true,
        'proxy-detail': true,
        'group-detail': true,
        'sentinel-detail': true
    };

    function decode(value) {
        try {
            return decodeURIComponent(value || '');
        } catch (ignore) {
            return value || '';
        }
    }

    function parse() {
        var hash = $window.location.hash || '';
        if (/^#[^/]/.test(hash) && hash.length > 1) {
            return {product: decode(hash.slice(1)), page: 'overview', id: ''};
        }
        var parts = hash.replace(/^#\/?/, '').split('/');
        var product = decode(parts.shift());
        var page = decode(parts.shift()) || 'overview';
        var id = decode(parts.join('/'));
        if (page === 'proxies' && id) {
            page = 'proxy-detail';
        } else if (page === 'groups' && id) {
            page = 'group-detail';
        } else if (page === 'sentinels' && id) {
            page = 'sentinel-detail';
        }
        if (!knownPages[page]) {
            page = 'overview';
            id = '';
        }
        return {product: product, page: page, id: id};
    }

    function publish() {
        var next = parse();
        if (current.product && (current.product !== next.product || current.page !== next.page || current.id !== next.id)) {
            scrollPositions[current.product + '/' + current.page + '/' + current.id] = $window.scrollY;
        }
        current = next;
        $rootScope.$applyAsync(function () {
            $rootScope.$broadcast('route:changed', angular.copy(current));
        });
        $timeout(function () {
            var key = current.product + '/' + current.page + '/' + current.id;
            $window.scrollTo(0, scrollPositions[key] || 0);
        }, 60);
    }

    function go(product, page, id) {
        var next = '#/' + encodeURIComponent(product || '') + '/' + encodeURIComponent(page || 'overview');
        if (id !== undefined && id !== null && id !== '') {
            next += '/' + encodeURIComponent(id);
        }
        if ($window.location.hash === next) {
            publish();
        } else {
            $window.location.hash = next;
        }
    }

    angular.element($window).on('hashchange', publish);
    current = parse();

    return {
        current: current,
        parse: parse,
        go: go,
        refresh: publish,
        basePage: function (page) {
            var detailPages = {
                'proxy-detail': 'proxies',
                'group-detail': 'groups',
                'sentinel-detail': 'sentinels'
            };
            return detailPages[page] || page || 'overview';
        }
    };
}]);

dashboard.factory('Api', ['$http', '$window', '$injector', function ($http, $window, $injector) {
    var params = new URLSearchParams($window.location.search);
    var mock = params.get('mock') === '1';

    function request(method, product, path, data, config) {
        if (mock) {
            return $injector.get('MockApi').request(method, product, path, data, config);
        }
        var resolved = path.replace('{xauth}', product ? genXAuth(product) : '');
        var url = resolved;
        if (product) {
            url += (url.indexOf('?') >= 0 ? '&' : '?') + 'forward=' + encodeURIComponent(product);
        }
        var options = angular.extend({}, config || {}, {
            method: method,
            url: url,
            data: data
        });
        return $http(options).then(function (response) {
            return response.data;
        });
    }

    function errorMessage(response) {
        if (!response) {
            return '请求失败，请稍后重试';
        }
        if (response.status === -1) {
            return '无法连接 Dashboard，请检查网络或服务状态';
        }
        if (response.status === 403) {
            return '当前账号没有访问此集群的权限';
        }
        var data = response.data;
        if (data) {
            if (typeof data === 'string') {
                if (/<\s*(!doctype|html|head|body|title)\b/i.test(data)) {
                    return 'Dashboard 或反向代理返回了不可解析的错误响应（HTTP ' + (response.status || 'unknown') + '）；请检查 Dashboard 日志和反向代理状态';
                }
                return data;
            }
            return data.Cause || data.cause || data.message || data.error || angular.toJson(data);
        }
        return '请求失败（HTTP ' + (response.status || 'unknown') + '）';
    }

    return {
        request: request,
        list: function () {
            return mock ? $injector.get('MockApi').list() : request('GET', '', '/list');
        },
        overview: function (product) {
            return request('GET', product, '/topom');
        },
        stats: function (product) {
            return request('GET', product, '/api/topom/stats/{xauth}');
        },
        segment: function (value) {
            return encodeURIComponent(value === undefined || value === null ? '' : String(value));
        },
        errorMessage: errorMessage,
        isForbidden: function (response) { return response && response.status === 403; },
        isMock: function () { return mock; },
        scenario: function () { return params.get('scenario') || 'normal'; }
    };
}]);

dashboard.factory('ClusterStore', ['Api', '$q', '$rootScope', function (Api, $q, $rootScope) {
    var cache = {};
    var sequences = {};

    function emptyState(product) {
        return {
            product: product,
            model: {},
            config: {},
            raw: {},
            proxies: [],
            groups: [],
            sentinels: [],
            slots: [],
            slotActions: [],
            slotAction: {},
            sentinelMasters: {},
            sentinelOutOfSync: false,
            totals: {
                qps: null,
                sessions: null,
                memory: null,
                keys: null,
                resources: 0,
                healthy: 0,
                issues: 0
            },
            issues: [],
            histories: {},
            loading: true,
            refreshing: false,
            error: null,
            forbidden: false,
            updatedAt: null
        };
    }

    function state(product) {
        if (!cache[product]) {
            cache[product] = emptyState(product);
            sequences[product] = 0;
        }
        return cache[product];
    }

    function proxyStatus(wrapper) {
        if (!wrapper) { return 'PENDING'; }
        if (wrapper.timeout) { return 'TIMEOUT'; }
        if (wrapper.error) { return 'ERROR'; }
        return wrapper.stats && wrapper.stats.online ? 'HEALTHY' : 'PENDING';
    }

    function serverStatus(wrapper) {
        if (!wrapper) { return 'PENDING'; }
        if (wrapper.timeout) { return 'TIMEOUT'; }
        if (wrapper.error) { return 'ERROR'; }
        return 'HEALTHY';
    }

    function numeric(value) {
        if (value === undefined || value === null || value === '' || isNaN(Number(value))) {
            return null;
        }
        return Number(value);
    }

    function dbKeys(stats) {
        var total = 0;
        var found = false;
        angular.forEach(stats || {}, function (value, key) {
            if (/^db\d+$/.test(key) && typeof value === 'string') {
                var match = value.match(/keys=(\d+)/);
                if (match) {
                    total += Number(match[1]);
                    found = true;
                }
            }
        });
        return found ? total : null;
    }

    function normalizeProxies(raw, target) {
        var models = raw.proxy && raw.proxy.models ? raw.proxy.models : [];
        var wrappers = raw.proxy && raw.proxy.stats ? raw.proxy.stats : {};
        var qps = 0;
        var sessions = 0;
        var hasQps = false;
        var hasSessions = false;
        var proxies = models.map(function (model) {
            var wrapper = wrappers[model.token];
            var stats = wrapper && wrapper.stats ? wrapper.stats : {};
            var ops = stats.ops || {};
            var sessionStats = stats.sessions || {};
            var currentQps = numeric(ops.qps);
            var alive = numeric(sessionStats.alive);
            if (currentQps !== null) {
                qps += currentQps;
                hasQps = true;
            }
            if (alive !== null) {
                sessions += alive;
                hasSessions = true;
            }
            return angular.extend({}, model, {
                name: model.token || model.proxy_addr,
                status: proxyStatus(wrapper),
                qps: currentQps,
                sessions: alive,
                totalSessions: numeric(sessionStats.total),
                commands: numeric(ops.total),
                fails: numeric(ops.fails),
                redisErrors: ops.redis ? numeric(ops.redis.errors) : null,
                switched: !!(stats.sentinels && stats.sentinels.switched),
                primaryOnly: !!(stats.backend && stats.backend.primary_only),
                proxyLink: model.admin_addr ? model.admin_addr + '/proxy' : '',
                statsLink: model.admin_addr ? model.admin_addr + '/proxy/stats' : ''
            });
        });
        target.totals.qps = hasQps ? qps : null;
        target.totals.sessions = hasSessions ? sessions : null;
        return proxies;
    }

    function normalizeGroups(raw, target) {
        var models = raw.group && raw.group.models ? raw.group.models : [];
        var wrappers = raw.group && raw.group.stats ? raw.group.stats : {};
        var memory = 0;
        var keys = 0;
        var hasMemory = false;
        var hasKeys = false;
        var groups = models.map(function (model) {
            var promoting = model.promoting || {};
            var group = angular.extend({}, model, {
                name: 'Group-' + model.id,
                servers: [],
                runids: {},
                isPromoting: !!promoting.state,
                promotingIndex: promoting.index || 0,
                memory: null,
                keys: null,
                status: 'HEALTHY'
            });
            var groupMemory = 0;
            var groupKeys = 0;
            var hasGroupMemory = false;
            var hasGroupKeys = false;
            (model.servers || []).forEach(function (server, index) {
                var wrapper = wrappers[server.server];
                var stats = wrapper && wrapper.stats ? wrapper.stats : {};
                var usedMemory = numeric(stats.used_memory);
                var maxMemory = numeric(stats.maxmemory);
                var keyCount = dbKeys(stats);
                var master = stats.master_addr ? stats.master_addr + ':' + (stats.master_link_status || 'unknown') : 'NO:ONE';
                var normalized = angular.extend({}, server, {
                    name: server.server,
                    status: serverStatus(wrapper),
                    memory: usedMemory,
                    maxMemory: maxMemory,
                    keys: keyCount,
                    master: master,
                    runId: stats.run_id || '',
                    isPrimary: index === 0,
                    masterHealthy: index === 0 ? master === 'NO:ONE' : master === model.servers[0].server + ':up',
                    canRemove: !promoting.state && (index !== 0 || model.servers.length <= 1),
                    canPromote: !promoting.state && index !== 0,
                    isPromoting: !!promoting.state && index === (promoting.index || 0),
                    actionState: server.action && server.action.state ? server.action.state : ''
                });
                if (stats.run_id) {
                    group.runids[stats.run_id] = server.server;
                }
                if (index === 0) {
                    if (usedMemory !== null) {
                        groupMemory = usedMemory;
                        memory += usedMemory;
                        hasMemory = true;
                        hasGroupMemory = true;
                    }
                    if (keyCount !== null) {
                        groupKeys = keyCount;
                        keys += keyCount;
                        hasKeys = true;
                        hasGroupKeys = true;
                    }
                }
                if (normalized.status !== 'HEALTHY' || !normalized.masterHealthy || model.out_of_sync) {
                    group.status = normalized.status === 'HEALTHY' ? 'WARNING' : normalized.status;
                }
                group.servers.push(normalized);
            });
            group.memory = hasGroupMemory ? groupMemory : null;
            group.keys = hasGroupKeys ? groupKeys : null;
            group.canRemove = group.servers.length === 0;
            return group;
        });
        target.totals.memory = hasMemory ? memory : null;
        target.totals.keys = hasKeys ? keys : null;
        return groups;
    }

    function normalizeSentinels(raw, groups, product, target) {
        var ha = raw.sentinels || {};
        var model = ha.model || {};
        var wrappers = ha.stats || {};
        var sentinels = (model.servers || []).map(function (address) {
            var wrapper = wrappers[address];
            var stats = wrapper && wrapper.stats ? wrapper.stats : {};
            var item = {
                name: address,
                server: address,
                status: serverStatus(wrapper),
                masters: null,
                mastersDown: null,
                slaves: null,
                sentinels: null,
                averageSlaves: null,
                averageSentinels: null,
                runidError: ''
            };
            if (item.status === 'HEALTHY') {
                var masters = numeric(stats.sentinel_masters) || 0;
                var activeMasters = 0;
                var down = 0;
                var slaves = 0;
                var sentinelCount = 0;
                for (var index = 0; index < masters; index++) {
                    var record = stats['master' + index];
                    if (!record) { continue; }
                    var fields = {};
                    record.split(',').forEach(function (pair) {
                        var parts = pair.split('=');
                        if (parts.length === 2) { fields[parts[0]] = parts[1]; }
                    });
                    if (!fields.name || fields.name.lastIndexOf(product + '-') !== 0) { continue; }
                    activeMasters++;
                    if (fields.status !== 'ok') { down++; }
                    slaves += numeric(fields.slaves) || 0;
                    sentinelCount += numeric(fields.sentinels) || 0;
                }
                item.masters = activeMasters;
                item.mastersDown = down;
                item.slaves = slaves;
                item.sentinels = sentinelCount;
                item.averageSlaves = activeMasters ? slaves / activeMasters : 0;
                item.averageSentinels = activeMasters ? sentinelCount / activeMasters : 0;
                if (down > 0) { item.status = 'WARNING'; }
                if (wrapper.sentinel) {
                    groups.forEach(function (group) {
                        var observed = wrapper.sentinel[product + '-' + group.id] || {};
                        var runids = {};
                        if (observed.master) {
                            runids[observed.master.runid] = observed.master.ip + ':' + observed.master.port;
                        }
                        (observed.slaves || []).forEach(function (server) {
                            runids[server.runid] = server.ip + ':' + server.port;
                        });
                        angular.forEach(runids, function (addressValue, runid) {
                            if (group.runids[runid] === undefined) {
                                item.runidError = 'Group-' + group.id + ' 多出的节点 ' + addressValue;
                            }
                        });
                        angular.forEach(group.runids, function (addressValue, runid) {
                            if (runids[runid] === undefined) {
                                item.runidError = 'Group-' + group.id + ' 缺失的节点 ' + addressValue;
                            }
                        });
                    });
                }
                if (item.runidError) { item.status = 'WARNING'; }
            }
            return item;
        });
        target.sentinelMasters = ha.masters || {};
        target.sentinelOutOfSync = !!model.out_of_sync;
        return sentinels;
    }

    function applyHA(target) {
        if (!target.sentinels.length) { return; }
        target.groups.forEach(function (group) {
            var haMaster = target.sentinelMasters[group.id];
            var found = false;
            group.haWarning = '';
            group.servers.forEach(function (server) {
                if (haMaster === undefined) {
                    server.haStatus = 'unknown';
                } else if (server.server === haMaster) {
                    server.haStatus = server.isPrimary ? 'master' : 'real-master';
                    found = true;
                    if (!server.isPrimary) { group.status = 'WARNING'; }
                } else {
                    server.haStatus = server.isPrimary ? 'not-master' : 'replica';
                    if (server.isPrimary) { group.status = 'WARNING'; }
                }
            });
            if (haMaster !== undefined && !found) {
                group.haWarning = haMaster;
                group.status = 'WARNING';
            }
        });
    }

    function appendHistory(target, key, metric, value, time) {
        var id = key + ':' + metric;
        if (!target.histories[id]) { target.histories[id] = []; }
        var points = target.histories[id];
        points.push([time, value === undefined ? null : value]);
        if (points.length > 180) { points.shift(); }
    }

    function recordHistory(target) {
        var time = Date.now();
        ['qps', 'sessions', 'memory', 'keys'].forEach(function (metric) {
            appendHistory(target, 'cluster', metric, target.totals[metric], time);
        });
        target.proxies.forEach(function (proxy) {
            appendHistory(target, 'proxy:' + proxy.token, 'qps', proxy.qps, time);
            appendHistory(target, 'proxy:' + proxy.token, 'sessions', proxy.sessions, time);
            appendHistory(target, 'proxy:' + proxy.token, 'fails', proxy.fails, time);
        });
        target.groups.forEach(function (group) {
            appendHistory(target, 'group:' + group.id, 'memory', group.memory, time);
            appendHistory(target, 'group:' + group.id, 'keys', group.keys, time);
        });
    }

    function normalize(product, raw, target) {
        target.raw = raw || {};
        target.proxies = normalizeProxies(raw || {}, target);
        target.groups = normalizeGroups(raw || {}, target);
        target.sentinels = normalizeSentinels(raw || {}, target.groups, product, target);
        applyHA(target);
        target.slots = raw.slots || [];
        target.slotAction = raw.slot_action || {};
        target.slotActions = target.slots.filter(function (slot) {
            return slot.action && slot.action.state;
        });
        target.issues = [];
        target.proxies.forEach(function (proxy) {
            if (proxy.status !== 'HEALTHY') {
                target.issues.push({type: 'proxy', id: proxy.token, name: proxy.token, status: proxy.status, description: 'Proxy 状态需要检查'});
            }
        });
        target.groups.forEach(function (group) {
            group.servers.forEach(function (server) {
                if (server.status !== 'HEALTHY' || !server.masterHealthy) {
                    target.issues.push({type: 'group', id: group.id, name: server.server, status: server.status === 'HEALTHY' ? 'WARNING' : server.status, description: 'Group-' + group.id + ' 复制或连接状态异常'});
                }
            });
        });
        target.sentinels.forEach(function (sentinel) {
            if (sentinel.status !== 'HEALTHY') {
                target.issues.push({type: 'sentinel', id: sentinel.server, name: sentinel.server, status: sentinel.status, description: sentinel.runidError || 'Sentinel 监控状态需要检查'});
            }
        });
        var all = target.proxies.concat(target.sentinels);
        target.groups.forEach(function (group) { all = all.concat(group.servers); });
        target.totals.resources = all.length;
        target.totals.healthy = all.filter(function (item) { return item.status === 'HEALTHY'; }).length;
        target.totals.issues = target.issues.length;
        recordHistory(target);
        target.updatedAt = new Date();
        target.loading = false;
        target.refreshing = false;
        target.error = null;
        target.forbidden = false;
        $rootScope.$broadcast('cluster:updated', {product: product, state: target});
        return target;
    }

    function load(product, force) {
        var target = state(product);
        if (target.updatedAt && !force) {
            return $q.when(target);
        }
        var sequence = ++sequences[product];
        if (target.updatedAt) {
            target.refreshing = true;
        } else {
            target.loading = true;
        }
        target.error = null;
        return Api.overview(product).then(function (overview) {
            if (sequence !== sequences[product]) { return target; }
            target.model = overview.model || {};
            target.config = overview.config || {};
            return normalize(product, overview.stats || {}, target);
        }).catch(function (response) {
            if (sequence === sequences[product]) {
                target.loading = false;
                target.refreshing = false;
                target.error = Api.errorMessage(response);
                target.forbidden = Api.isForbidden(response);
            }
            return $q.reject(response);
        });
    }

    function refresh(product) {
        var target = state(product);
        var sequence = ++sequences[product];
        target.refreshing = true;
        target.error = null;
        return Api.stats(product).then(function (raw) {
            if (sequence !== sequences[product]) { return target; }
            return normalize(product, raw, target);
        }).catch(function (response) {
            if (sequence === sequences[product]) {
                target.refreshing = false;
                target.error = Api.errorMessage(response);
                target.forbidden = Api.isForbidden(response);
            }
            return $q.reject(response);
        });
    }

    function history(product, key, metric) {
        var target = state(product);
        return target.histories[key + ':' + metric] || [];
    }

    return {
        state: state,
        load: load,
        refresh: refresh,
        history: history,
        clear: function (product) { delete cache[product]; }
    };
}]);
