'use strict';

(function () {
    var statusOptions = [
        {value: 'HEALTHY', label: 'HEALTHY'},
        {value: 'WARNING', label: 'WARNING'},
        {value: 'PENDING', label: 'PENDING'},
        {value: 'TIMEOUT', label: 'TIMEOUT'},
        {value: 'ERROR', label: 'ERROR'}
    ];

    function present(value) {
        return value !== undefined && value !== null && value !== '';
    }

    function hostPortError(value, label) {
        var text = String(value === undefined || value === null ? '' : value).trim();
        var match;
        if (!text) {
            return '请输入' + label;
        }
        if (text.charAt(0) === '[') {
            match = text.match(/^\[([0-9A-Fa-f:.]+)\]:(\d+)$/);
            if (!match || match[1].indexOf(':') < 0) {
                return label + '必须是有效的 [IPv6]:port';
            }
        } else {
            match = text.match(/^([^:\s]+):(\d+)$/);
            if (!match || !match[1] || /[\/\\?#]/.test(match[1])) {
                return label + '必须是有效的 host:port';
            }
        }
        var port = Number(match[2]);
        if (!/^\d+$/.test(match[2]) || port < 1 || port > 65535 || Math.floor(port) !== port) {
            return label + '的端口必须是 1 到 65535 的整数';
        }
        return '';
    }

    dashboard.factory('ResourceActions', ['Api', 'Activity', 'ClusterStore', 'Confirm', 'Toast', '$q', '$window',
        function (Api, Activity, ClusterStore, Confirm, Toast, $q, $window) {
            function isCurrent(owner, product, sequence, resourceId) {
                return owner && owner.resourceContext && owner.resourceContext(product, resourceId) &&
                    (sequence === undefined || owner._mutationSequence === sequence);
            }

            function refresh(product) {
                ClusterStore.refresh(product).catch(angular.noop);
            }

            function execute(owner, product, request) {
                if (!owner || owner.submitting || !product || !request || !request.path) {
                    return $q.when(false);
                }
                var resourceId = owner.resourceId;
                var sequence = owner._mutationSequence = (owner._mutationSequence || 0) + 1;
                owner.submitting = true;
                owner.operationError = '';
                if (request.errorField) {
                    owner[request.errorField] = '';
                }
                return Api.request('PUT', product, request.path).then(function (data) {
                    Activity.add(product, request.action || '更新资源', request.target || '集群', 'success');
                    Toast.success(request.successMessage || '操作成功，正在刷新集群数据');
                    if (isCurrent(owner, product, sequence, resourceId)) {
                        if (angular.isFunction(request.close)) {
                            request.close(owner);
                        }
                        if (angular.isFunction(request.onSuccess)) {
                            request.onSuccess(data);
                        }
                    }
                    refresh(product);
                    return true;
                }, function (response) {
                    var message = Api.errorMessage(response);
                    if (isCurrent(owner, product, sequence, resourceId)) {
                        owner[request.errorField || 'operationError'] = message;
                    }
                    Toast.error(message);
                    return false;
                }).finally(function () {
                    if (isCurrent(owner, product, sequence, resourceId)) {
                        owner.submitting = false;
                    }
                });
            }

            function confirmAndRun(owner, product, confirmation, request) {
                if (!owner || owner.submitting || owner.confirming || !product) {
                    return $q.when(false);
                }
                var resourceId = owner.resourceId;
                owner.confirming = true;
                owner.operationError = '';
                return Confirm.open(confirmation).then(function () {
                    if (!owner.resourceContext(product, resourceId)) {
                        return false;
                    }
                    owner.confirming = false;
                    return execute(owner, product, request);
                }, function () {
                    if (owner.resourceContext(product, resourceId)) {
                        owner.confirming = false;
                    }
                    return false;
                });
            }

            function confirmEach(owner, product, rows, optionsFor) {
                var accepted = [];
                var index = 0;
                var resourceId = owner.resourceId;
                if (!rows.length) {
                    return $q.when(accepted);
                }
                owner.confirming = true;
                function next() {
                    if (!owner.resourceContext(product, resourceId) || index >= rows.length) {
                        if (owner.resourceContext(product, resourceId)) {
                            owner.confirming = false;
                        }
                        return $q.when(accepted);
                    }
                    var row = rows[index++];
                    return Confirm.open(optionsFor(row, index, rows.length)).then(function () {
                        accepted.push(row);
                        return next();
                    }, function () {
                        return next();
                    });
                }
                return next().finally(function () {
                    if (owner.resourceContext(product, resourceId)) {
                        owner.confirming = false;
                    }
                });
            }

            function batch(owner, product, jobs, options) {
                options = options || {};
                if (!jobs.length || owner.submitting) {
                    return $q.when([]);
                }
                var resourceId = owner.resourceId;
                var sequence = owner._mutationSequence = (owner._mutationSequence || 0) + 1;
                owner.submitting = true;
                owner.operationError = '';
                var requests = jobs.map(function (job) {
                    return Api.request('PUT', product, job.path).then(function () {
                        return {ok: true, job: job};
                    }, function (response) {
                        return {ok: false, job: job, response: response};
                    });
                });
                return $q.all(requests).then(function (results) {
                    var succeeded = results.filter(function (result) { return result.ok; });
                    var failed = results.filter(function (result) { return !result.ok; });
                    results.forEach(function (result) {
                        Activity.add(product, result.job.action, result.job.target, result.ok ? 'success' : 'error');
                    });
                    if (succeeded.length) {
                        Toast.success(options.successMessage || ('已完成 ' + succeeded.length + ' 项操作，正在刷新集群数据'));
                        refresh(product);
                    }
                    if (failed.length) {
                        var firstMessage = Api.errorMessage(failed[0].response);
                        var message = '失败 ' + failed.length + ' 项' + (succeeded.length ? '，成功 ' + succeeded.length + ' 项' : '') + '：' + firstMessage;
                        if (isCurrent(owner, product, sequence, resourceId)) {
                            owner.operationError = message;
                        }
                        Toast.error(message);
                    }
                    if (isCurrent(owner, product, sequence, resourceId) && angular.isFunction(options.onComplete)) {
                        options.onComplete(results);
                    }
                    return results;
                }).finally(function () {
                    if (isCurrent(owner, product, sequence, resourceId)) {
                        owner.submitting = false;
                    }
                });
            }

            function haConflicts(state, groups) {
                var conflicts = [];
                (groups || (state && state.groups) || []).forEach(function (group) {
                    var realMaster = null;
                    (group.servers || []).forEach(function (server) {
                        if (server.haStatus === 'real-master') {
                            realMaster = server.server;
                        }
                    });
                    if (realMaster) {
                        conflicts.push({
                            group: group.id,
                            logical_master: group.servers && group.servers.length ? group.servers[0].server : '—',
                            ha_real_master: realMaster
                        });
                    }
                });
                return conflicts;
            }

            function safeHttpUrl(value, suffix) {
                if (!present(value)) {
                    return '';
                }
                var raw = String(value).trim();
                if (!/^https?:\/\//i.test(raw)) {
                    if (raw.indexOf('://') >= 0) {
                        return '';
                    }
                    raw = 'http://' + raw;
                }
                raw = raw.replace(/\/+$/, '') + (suffix || '');
                try {
                    var parsed = new $window.URL(raw);
                    if ((parsed.protocol !== 'http:' && parsed.protocol !== 'https:') || !parsed.hostname) {
                        return '';
                    }
                    return parsed.href;
                } catch (ignore) {
                    return '';
                }
            }

            function openExternal(url) {
                var safe = safeHttpUrl(url);
                if (!safe) {
                    Toast.error('仅允许打开有效的 http/https 链接');
                    return false;
                }
                $window.open(safe, '_blank', 'noopener,noreferrer');
                return true;
            }

            return {
                execute: execute,
                confirmAndRun: confirmAndRun,
                confirmEach: confirmEach,
                batch: batch,
                haConflicts: haConflicts,
                safeHttpUrl: safeHttpUrl,
                openExternal: openExternal,
                hostPortError: hostPortError
            };
        }
    ]);

    var listTemplate = `
        <div class="resources-page">
            <page-header title="{{$ctrl.title}}" description="{{$ctrl.description}}">
                <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'group'"
                    ng-click="$ctrl.openForm('add-server')" ng-disabled="$ctrl.busy() || !$ctrl.rows.length">
                    <ui-icon name="plus" size="16"></ui-icon>添加 Server
                </button>
                <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'group'"
                    ng-click="$ctrl.resyncAllGroups()" ng-disabled="$ctrl.busy() || !$ctrl.rows.length">
                    <ui-icon name="refresh" size="16"></ui-icon>Resync All
                </button>
                <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'group'"
                    ng-click="$ctrl.replicaAll(1)" ng-disabled="$ctrl.busy() || !$ctrl.rows.length">启用全部 Replica Group</button>
                <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'group'"
                    ng-click="$ctrl.replicaAll(0)" ng-disabled="$ctrl.busy() || !$ctrl.rows.length">停用全部 Replica Group</button>
                <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'sentinel'"
                    ng-click="$ctrl.resyncSentinels()" ng-disabled="$ctrl.busy() || !$ctrl.rows.length">
                    <ui-icon name="refresh" size="16"></ui-icon>Resync All Sentinels
                </button>
                <button class="button button-primary" type="button" ng-click="$ctrl.openForm('create')" ng-disabled="$ctrl.busy()">
                    <ui-icon name="plus" size="16"></ui-icon>{{$ctrl.createLabel}}
                </button>
            </page-header>

            <loading-skeleton ng-if="$ctrl.initialLoading()" variant="page"></loading-skeleton>
            <div ng-if="!$ctrl.initialLoading()">
                <div class="inline-alert inline-alert--error" ng-if="$ctrl.cluster.updatedAt && $ctrl.cluster.error" role="alert">
                    <ui-icon name="alert" size="18"></ui-icon>
                    <div>
                        <strong>后台刷新失败，已保留上次成功加载的数据</strong>
                        <p>{{$ctrl.cluster.error}}</p>
                        <button class="button button-secondary" type="button" ng-click="$ctrl.retry()">重试</button>
                    </div>
                </div>
                <div class="inline-alert inline-alert--error" ng-if="$ctrl.operationError" role="alert">
                    <ui-icon name="alert" size="18"></ui-icon><span>{{$ctrl.operationError}}</span>
                </div>

                <div class="metrics-grid" ng-if="$ctrl.cluster.updatedAt">
                    <metric-card ng-repeat="metric in $ctrl.metrics track by metric.key" metric="metric"></metric-card>
                </div>

                <section class="data-table-card" aria-label="{{$ctrl.title}}列表">
                    <div class="table-toolbar">
                        <filter-bar filters="$ctrl.filters" status-options="$ctrl.statusOptions"
                            type-options="$ctrl.typeOptions" result-count="$ctrl.filteredRows.length"
                            refreshing="$ctrl.cluster.refreshing || $ctrl.submitting"
                            search-placeholder="{{$ctrl.searchPlaceholder}}"
                            on-change="$ctrl.filtersChanged()" on-refresh="$ctrl.refresh(true)"></filter-bar>
                    </div>
                    <div class="batch-bar" ng-if="$ctrl.selected.length">
                        <span>已选择 <strong>{{$ctrl.selected.length}}</strong> 项</span>
                        <div class="batch-bar__actions">
                            <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'proxy' || $ctrl.kind === 'group'"
                                ng-click="$ctrl.batchAction($ctrl.kind === 'proxy' ? 'reinit' : 'resync')" ng-disabled="$ctrl.busy()">
                                <ui-icon name="refresh" size="15"></ui-icon>批量 {{$ctrl.kind === 'proxy' ? 'Reinit' : 'Resync'}}
                            </button>
                            <button class="button button-danger" type="button" ng-if="$ctrl.kind === 'proxy' || $ctrl.kind === 'sentinel'"
                                ng-click="$ctrl.batchAction('remove')" ng-disabled="$ctrl.busy()">
                                <ui-icon name="trash" size="15"></ui-icon>批量移除
                            </button>
                        </div>
                    </div>

                    <codis-data-table ng-if="$ctrl.filteredRows.length || $ctrl.tableError()"
                        rows="$ctrl.filteredRows" columns="$ctrl.columns" loading="false" error="$ctrl.tableError()"
                        selected="$ctrl.selected" page="$ctrl.page" page-size="$ctrl.pageSize"
                        sort-key="$ctrl.sortKey" sort-direction="$ctrl.sortDirection" selectable="true"
                        row-key="id" empty-title="暂无资源" empty-description="当前没有可显示的资源。"
                        on-retry="$ctrl.retry()" on-open="$ctrl.openDetail(row)"
                        on-row-action="$ctrl.rowAction(row, action)"></codis-data-table>
                    <empty-state ng-if="!$ctrl.tableError() && !$ctrl.rows.length" icon="database"
                        title="{{$ctrl.emptyTitle}}" description="{{$ctrl.emptyDescription}}"
                        action-label="{{$ctrl.createLabel}}" on-action="$ctrl.openForm('create')"></empty-state>
                    <empty-state ng-if="!$ctrl.tableError() && $ctrl.rows.length && !$ctrl.filteredRows.length"
                        icon="search" title="没有匹配的资源" description="请调整关键词、状态或类型筛选条件。"
                        action-label="清除筛选" on-action="$ctrl.clearFilters()"></empty-state>
                </section>
            </div>

            <resource-drawer open="$ctrl.formOpen" title="$ctrl.formTitle()" subtitle="$ctrl.formSubtitle()"
                width="560" submitting="$ctrl.submitting" submit-disabled="$ctrl.submitting"
                submit-label="{{$ctrl.submitLabel()}}" show-submit="true" error="$ctrl.drawerError"
                on-close="$ctrl.closeForm()" on-submit="$ctrl.submitForm()">
                <div ng-switch="$ctrl.formMode">
                    <div ng-switch-when="create-proxy" class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.address}">
                        <label class="form-label" for="proxy-create-admin-addr">admin_addr <span class="required">*</span></label>
                        <input id="proxy-create-admin-addr" class="form-control" type="text"
                            ng-model="$ctrl.editor.admin_addr" ng-disabled="$ctrl.submitting"
                            ng-change="$ctrl.clearFieldError('address')" placeholder="proxy.example.com:11080"
                            autocomplete="off" spellcheck="false" required>
                        <p class="form-hint">请输入 Proxy 管理地址；IPv6 使用 [address]:port。</p>
                        <p class="form-error" ng-if="$ctrl.fieldErrors.address" role="alert">{{$ctrl.fieldErrors.address}}</p>
                    </div>
                    <div ng-switch-when="create-group" class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.gid}">
                        <label class="form-label" for="group-create-gid">Group ID <span class="required">*</span></label>
                        <input id="group-create-gid" class="form-control" type="text" inputmode="numeric"
                            ng-model="$ctrl.editor.gid" ng-disabled="$ctrl.submitting"
                            ng-change="$ctrl.clearFieldError('gid')" placeholder="1 - 9999" autocomplete="off" required>
                        <p class="form-hint">GID 必须是 1 到 9999 的整数，创建后不可修改。</p>
                        <p class="form-error" ng-if="$ctrl.fieldErrors.gid" role="alert">{{$ctrl.fieldErrors.gid}}</p>
                    </div>
                    <div ng-switch-when="add-server" class="form-grid">
                        <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.gid}">
                            <label class="form-label" for="group-add-gid">gid <span class="required">*</span></label>
                            <input id="group-add-gid" class="form-control" type="text" inputmode="numeric"
                                ng-model="$ctrl.editor.gid" ng-readonly="$ctrl.editor.lockGid" ng-disabled="$ctrl.submitting"
                                ng-change="$ctrl.clearFieldError('gid')" autocomplete="off" required>
                            <p class="form-error" ng-if="$ctrl.fieldErrors.gid" role="alert">{{$ctrl.fieldErrors.gid}}</p>
                        </div>
                        <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.address}">
                            <label class="form-label" for="group-add-address">server_addr <span class="required">*</span></label>
                            <input id="group-add-address" class="form-control" type="text"
                                ng-model="$ctrl.editor.server_addr" ng-disabled="$ctrl.submitting"
                                ng-change="$ctrl.clearFieldError('address')" placeholder="redis.example.com:6379"
                                autocomplete="off" spellcheck="false" required>
                            <p class="form-error" ng-if="$ctrl.fieldErrors.address" role="alert">{{$ctrl.fieldErrors.address}}</p>
                        </div>
                        <div class="form-group form-group--full" ng-class="{'has-error': $ctrl.fieldErrors.datacenter}">
                            <label class="form-label" for="group-add-datacenter">datacenter（可选）</label>
                            <input id="group-add-datacenter" class="form-control" type="text"
                                ng-model="$ctrl.editor.datacenter" ng-disabled="$ctrl.submitting"
                                ng-change="$ctrl.clearFieldError('datacenter')"
                                placeholder="留空则不提交 datacenter 路径段" autocomplete="off">
                            <p class="form-hint">仅填写数据中心名称，不要包含斜杠。</p>
                            <p class="form-error" ng-if="$ctrl.fieldErrors.datacenter" role="alert">{{$ctrl.fieldErrors.datacenter}}</p>
                        </div>
                    </div>
                    <div ng-switch-when="add-sentinel" class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.address}">
                        <label class="form-label" for="sentinel-add-address">server_addr <span class="required">*</span></label>
                        <input id="sentinel-add-address" class="form-control" type="text"
                            ng-model="$ctrl.editor.server_addr" ng-disabled="$ctrl.submitting"
                            ng-change="$ctrl.clearFieldError('address')" placeholder="sentinel.example.com:26379"
                            autocomplete="off" spellcheck="false" required>
                        <p class="form-hint">请输入 Sentinel 的 host:port；添加后会重新加载高可用状态。</p>
                        <p class="form-error" ng-if="$ctrl.fieldErrors.address" role="alert">{{$ctrl.fieldErrors.address}}</p>
                    </div>
                </div>
            </resource-drawer>

            <resource-drawer open="$ctrl.quickOpen" title="$ctrl.quickResource.name || '资源详情'"
                subtitle="$ctrl.title + ' · 快速详情（不会改变当前列表位置）'" width="640"
                submitting="$ctrl.submitting" show-submit="false" on-close="$ctrl.closeQuick()">
                <div ng-if="$ctrl.quickResource">
                    <div class="inline-alert" ng-class="$ctrl.quickResource.status === 'HEALTHY' ? 'inline-alert--success' : 'inline-alert--warning'">
                        <ui-icon name="info" size="18"></ui-icon>
                        <status-tag status="$ctrl.quickResource.status"></status-tag>
                        <span>{{$ctrl.quickResource.haWarning || $ctrl.quickResource.runidError || ''}}</span>
                    </div>
                    <dl class="key-value-grid">
                        <div class="key-value-item" ng-repeat="field in $ctrl.quickFields track by field.key">
                            <dt>{{field.label}}</dt><dd>{{field.value}}</dd>
                        </div>
                    </dl>
                    <div class="card-actions" ng-if="$ctrl.kind === 'proxy' && ($ctrl.quickResource.proxyUrl || $ctrl.quickResource.statsUrl)">
                        <a class="button button-secondary" ng-if="$ctrl.quickResource.proxyUrl"
                            ng-href="{{$ctrl.quickResource.proxyUrl}}" target="_blank" rel="noopener noreferrer">
                            <ui-icon name="link" size="15"></ui-icon>Proxy
                        </a>
                        <a class="button button-secondary" ng-if="$ctrl.quickResource.statsUrl"
                            ng-href="{{$ctrl.quickResource.statsUrl}}" target="_blank" rel="noopener noreferrer">
                            <ui-icon name="link" size="15"></ui-icon>Stats
                        </a>
                    </div>
                    <div class="card-actions" ng-if="$ctrl.kind === 'sentinel' && ($ctrl.quickResource.infoUrl || $ctrl.quickResource.monitoredUrl)">
                        <a class="button button-secondary" ng-if="$ctrl.quickResource.infoUrl"
                            ng-href="{{$ctrl.quickResource.infoUrl}}" target="_blank" rel="noopener noreferrer">Info</a>
                        <a class="button button-secondary" ng-if="$ctrl.quickResource.monitoredUrl"
                            ng-href="{{$ctrl.quickResource.monitoredUrl}}" target="_blank" rel="noopener noreferrer">Monitored</a>
                    </div>
                    <div class="card-actions">
                        <button class="button button-primary" type="button" ng-click="$ctrl.openDetail($ctrl.quickResource)">
                            <ui-icon name="eye" size="15"></ui-icon>完整详情
                        </button>
                        <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'proxy'"
                            ng-click="$ctrl.reinitProxy($ctrl.quickResource)" ng-disabled="$ctrl.busy()">Reinit</button>
                        <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'group'"
                            ng-click="$ctrl.resyncGroup($ctrl.quickResource)" ng-disabled="$ctrl.busy()">Resync</button>
                        <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'group'"
                            ng-click="$ctrl.openForm('add-server', $ctrl.quickResource)" ng-disabled="$ctrl.busy()">添加 Server</button>
                        <button class="button button-danger" type="button" ng-if="$ctrl.kind !== 'group' || $ctrl.quickResource.canRemove"
                            ng-click="$ctrl.removeResource($ctrl.quickResource)" ng-disabled="$ctrl.busy()">
                            {{$ctrl.removeLabel($ctrl.quickResource)}}
                        </button>
                    </div>
                </div>
            </resource-drawer>
        </div>
    `;

    dashboard.factory('ResourceListFactory', ['Api', 'Router', 'ClusterStore', 'Preferences', 'ResourceActions', 'Format', 'Toast',
        function (Api, Router, ClusterStore, Preferences, ResourceActions, Format, Toast) {
            var configs = {
                proxy: {
                    title: 'Proxy 实例', description: '管理 Codis Proxy 生命周期、连接与请求指标。', createLabel: '创建 Proxy',
                    searchPlaceholder: '搜索 Token、Proxy 地址或数据中心', emptyTitle: '还没有 Proxy 实例',
                    emptyDescription: '创建 Proxy 后可在这里检查连接、QPS 与运行状态。', detailPage: 'proxy-detail', collection: 'proxies'
                },
                group: {
                    title: 'Group 与 Server', description: '查看 Redis 主从拓扑、内存与 HA 状态，并管理 Group 同步。', createLabel: '创建 Group',
                    searchPlaceholder: '搜索 Group、Server 地址或数据中心', emptyTitle: '还没有 Group',
                    emptyDescription: '先创建一个空 Group，再添加 Redis Server。', detailPage: 'group-detail', collection: 'groups'
                },
                sentinel: {
                    title: 'Sentinel 高可用', description: '检查 Sentinel 监控覆盖率、主节点状态与拓扑一致性。', createLabel: '添加 Sentinel',
                    searchPlaceholder: '搜索 Sentinel 地址或异常信息', emptyTitle: '还没有 Sentinel',
                    emptyDescription: '添加 Sentinel 后可监控 Group 主节点并检查 HA 状态。', detailPage: 'sentinel-detail', collection: 'sentinels'
                }
            };

            return function (ctrl, kind, $scope) {
                var config = configs[kind];
                var destroyed = false;
                var activated = false;
                var contextSequence = 0;
                var preferenceKey = '';

                angular.extend(ctrl, config);
                ctrl.kind = kind;
                ctrl.cluster = ClusterStore.state(ctrl.product || 'default');
                ctrl.rows = [];
                ctrl.filteredRows = [];
                ctrl.metrics = [];
                ctrl.selected = [];
                ctrl.page = 1;
                ctrl.pageSize = 10;
                ctrl.sortKey = 'name';
                ctrl.sortDirection = 'asc';
                ctrl.filters = {search: '', status: '', type: ''};
                ctrl.statusOptions = statusOptions;
                ctrl.typeOptions = [];
                ctrl.submitting = false;
                ctrl.confirming = false;
                ctrl.operationError = '';
                ctrl.formOpen = false;
                ctrl.formMode = '';
                ctrl.editor = {};
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                ctrl.quickOpen = false;
                ctrl.quickResource = null;
                ctrl.quickFields = [];
                ctrl.resourceContext = function (product) {
                    return !destroyed && ctrl.product === product;
                };

                function preferenceDefaults() {
                    return {filters: {search: '', status: '', type: ''}, page: 1, sortKey: 'name', sortDirection: 'asc'};
                }

                function loadPreferences() {
                    preferenceKey = 'resources.' + ctrl.product + '.' + kind;
                    var saved = Preferences.get(preferenceKey, preferenceDefaults());
                    saved = saved || {};
                    ctrl.filters = angular.extend({search: '', status: '', type: ''}, saved.filters || {});
                    ctrl.page = Number(saved.page) > 0 ? Math.floor(Number(saved.page)) : 1;
                    ctrl.sortKey = saved.sortKey || 'name';
                    ctrl.sortDirection = saved.sortDirection === 'desc' ? 'desc' : 'asc';
                }

                function savePreferences() {
                    if (!preferenceKey || !ctrl.product) { return; }
                    Preferences.set(preferenceKey, {
                        filters: angular.copy(ctrl.filters),
                        page: ctrl.page,
                        sortKey: ctrl.sortKey,
                        sortDirection: ctrl.sortDirection
                    });
                }

                function proxyColumns() {
                    return [
                        {key: 'name', label: 'Token', type: 'link', sortable: true, width: 170},
                        {key: 'status', label: '状态', type: 'status', sortable: true, width: 105},
                        {key: 'proxy_addr', label: 'Proxy 地址', sortable: true, width: 150},
                        {key: 'admin_addr', label: 'admin_addr', sortable: true, width: 150},
                        {key: 'qps', label: 'QPS', type: 'number', sortable: true, width: 90},
                        {key: 'sessions', label: '活跃连接', type: 'number', sortable: true, width: 100},
                        {key: 'commands', label: '总命令', type: 'number', sortable: true, width: 110},
                        {key: 'fails', label: '失败', type: 'number', sortable: true, width: 80},
                        {key: 'qpsTrend', label: 'QPS 趋势', type: 'sparkline', width: 110,
                            trend: function (row) { return ClusterStore.history(ctrl.product, 'proxy:' + row.token, 'qps'); }},
                        {key: 'actions', label: '操作', type: 'actions', width: 225, actions: function (row) {
                            var actions = [
                                {name: 'quick', icon: 'eye', title: '快速详情'},
                                {name: 'reinit', icon: 'refresh', title: 'Reinit and Start'},
                                {name: 'remove', icon: 'trash', title: ctrl.removeLabel(row), danger: true}
                            ];
                            if (row.proxyUrl) { actions.push({name: 'proxy-link', icon: 'link', title: '打开 Proxy'}); }
                            if (row.statsUrl) { actions.push({name: 'stats-link', icon: 'link', title: '打开 Stats'}); }
                            return actions;
                        }}
                    ];
                }

                function groupColumns() {
                    return [
                        {key: 'name', label: 'Group', type: 'link', sortable: true, width: 115},
                        {key: 'status', label: '状态', type: 'status', sortable: true, width: 100},
                        {key: 'masterDisplay', label: 'Logical Master', sortable: true, width: 160},
                        {key: 'serverCount', label: 'Servers', type: 'number', sortable: true, width: 85},
                        {key: 'memory', label: 'Memory', type: 'bytes', sortable: true, width: 105},
                        {key: 'keys', label: 'Keys', type: 'number', sortable: true, width: 105},
                        {key: 'haState', label: 'HA', type: 'status', sortable: true, width: 100},
                        {key: 'syncState', label: '拓扑同步', type: 'status', sortable: true, width: 115},
                        {key: 'actionDisplay', label: 'Action', sortable: true, width: 110},
                        {key: 'memoryTrend', label: 'Memory 趋势', type: 'sparkline', width: 110,
                            trend: function (row) { return ClusterStore.history(ctrl.product, 'group:' + row.id, 'memory'); }},
                        {key: 'actions', label: '操作', type: 'actions', width: 190, actions: function (row) {
                            return [
                                {name: 'quick', icon: 'eye', title: '快速详情'},
                                {name: 'resync', icon: 'refresh', title: 'Resync Group'},
                                {name: 'add-server', icon: 'plus', title: '添加 Server'},
                                {name: 'remove', icon: 'trash', title: row.canRemove ? '删除空 Group' : '仅空 Group 可删除', danger: true,
                                    disabled: function () { return !row.canRemove; }}
                            ];
                        }}
                    ];
                }

                function sentinelColumns() {
                    return [
                        {key: 'name', label: 'Sentinel', type: 'link', sortable: true, width: 180},
                        {key: 'status', label: '状态', type: 'status', sortable: true, width: 100},
                        {key: 'masters', label: 'Masters', type: 'number', sortable: true, width: 90},
                        {key: 'mastersDown', label: 'Down', type: 'number', sortable: true, width: 80},
                        {key: 'averageSlaves', label: 'Avg Slaves', type: 'number', sortable: true, width: 105},
                        {key: 'averageSentinels', label: 'Avg Sentinels', type: 'number', sortable: true, width: 120},
                        {key: 'runidError', label: 'runidError', sortable: true, width: 190},
                        {key: 'syncState', label: 'out_of_sync', type: 'status', sortable: true, width: 120},
                        {key: 'actions', label: '操作', type: 'actions', width: 180, actions: function (row) {
                            var actions = [
                                {name: 'quick', icon: 'eye', title: '快速详情'},
                                {name: 'remove', icon: 'trash', title: ctrl.removeLabel(row), danger: true}
                            ];
                            if (row.infoUrl) { actions.push({name: 'info-link', icon: 'link', title: '打开 Info'}); }
                            if (row.monitoredUrl) { actions.push({name: 'monitored-link', icon: 'link', title: '打开 Monitored'}); }
                            return actions;
                        }}
                    ];
                }

                ctrl.columns = kind === 'proxy' ? proxyColumns() : (kind === 'group' ? groupColumns() : sentinelColumns());
                ctrl.typeOptions = kind === 'proxy' ? [
                    {value: 'tcp4', label: 'tcp4'}, {value: 'tcp6', label: 'tcp6'}, {value: 'other', label: '其他协议'}
                ] : (kind === 'group' ? [
                    {value: 'empty', label: '空 Group'}, {value: 'nonempty', label: '含 Server'}
                ] : [
                    {value: 'runid-error', label: 'runid 异常'}, {value: 'runid-ok', label: 'runid 一致'}
                ]);

                function resourceId(resource) {
                    return kind === 'proxy' ? resource.token : (kind === 'group' ? resource.id : resource.server);
                }

                function buildRows() {
                    var resources = ctrl.cluster[config.collection] || [];
                    ctrl.rows = resources.map(function (resource) {
                        var row = angular.extend({}, resource, {resource: resource, id: resourceId(resource)});
                        if (kind === 'proxy') {
                            row.filterType = row.proto_type === 'tcp4' || row.proto_type === 'tcp6' ? row.proto_type : 'other';
                            row.proxyUrl = ResourceActions.safeHttpUrl(resource.admin_addr, '/proxy');
                            row.statsUrl = ResourceActions.safeHttpUrl(resource.admin_addr, '/proxy/stats');
                            row.searchText = [row.token, row.proxy_addr, row.admin_addr, row.hostname, row.datacenter, row.proto_type].join(' ').toLowerCase();
                        } else if (kind === 'group') {
                            var conflict = ResourceActions.haConflicts(ctrl.cluster, [resource]).length > 0 ||
                                (resource.servers || []).some(function (server) { return server.haStatus === 'not-master'; }) || !!resource.haWarning;
                            row.masterDisplay = resource.servers && resource.servers.length ? resource.servers[0].server : '—';
                            row.serverCount = (resource.servers || []).length;
                            row.haState = !ctrl.cluster.sentinels.length ? 'UNKNOWN' : (conflict ? 'CONFLICT' : 'OK');
                            row.syncState = resource.out_of_sync ? 'OUT_OF_SYNC' : 'IN_SYNC';
                            row.actionDisplay = resource.isPromoting ? 'PROMOTING:' + resource.promotingIndex :
                                ((resource.servers || []).filter(function (server) { return server.actionState; }).map(function (server) {
                                    return server.actionState;
                                }).join(', ') || '—');
                            row.filterType = row.serverCount ? 'nonempty' : 'empty';
                            row.searchText = [row.name, row.id, row.masterDisplay, row.datacenter,
                                (resource.servers || []).map(function (server) { return server.server + ' ' + (server.datacenter || ''); }).join(' ')].join(' ').toLowerCase();
                        } else {
                            row.syncState = ctrl.cluster.sentinelOutOfSync ? 'OUT_OF_SYNC' : 'IN_SYNC';
                            row.filterType = row.runidError ? 'runid-error' : 'runid-ok';
                            row.infoUrl = ResourceActions.safeHttpUrl(ctrl.cluster.model.admin_addr,
                                '/api/topom/sentinels/info/' + Api.segment(resource.server));
                            row.monitoredUrl = ResourceActions.safeHttpUrl(ctrl.cluster.model.admin_addr,
                                '/api/topom/sentinels/info/' + Api.segment(resource.server) + '/monitored');
                            row.searchText = [row.server, row.runidError, row.syncState].join(' ').toLowerCase();
                        }
                        return row;
                    });
                    var existing = {};
                    ctrl.rows.forEach(function (row) { existing[String(row.id)] = true; });
                    ctrl.selected = ctrl.selected.filter(function (id) { return existing[String(id)]; });
                    applyFilters(false);
                    updateQuick();
                }

                function applyFilters(resetPage) {
                    var search = String(ctrl.filters.search || '').trim().toLowerCase();
                    ctrl.filteredRows = ctrl.rows.filter(function (row) {
                        return (!search || row.searchText.indexOf(search) >= 0) &&
                            (!ctrl.filters.status || row.status === ctrl.filters.status) &&
                            (!ctrl.filters.type || row.filterType === ctrl.filters.type);
                    });
                    if (resetPage) { ctrl.page = 1; }
                }

                function updateMetrics() {
                    if (kind === 'proxy') {
                        ctrl.metrics = [
                            {key: 'total', title: 'Proxy 总数', value: ctrl.rows.length, caption: '当前集群注册实例', icon: 'proxy'},
                            {key: 'healthy', title: '健康实例', value: ctrl.rows.filter(function (row) { return row.status === 'HEALTHY'; }).length,
                                caption: 'HEALTHY / 全部 Proxy', icon: 'check'},
                            {key: 'qps', title: '总 QPS', value: ctrl.cluster.totals.qps, caption: '缺失指标不计入汇总', icon: 'activity', theme: 'primary'},
                            {key: 'sessions', title: '活跃连接', value: ctrl.cluster.totals.sessions, caption: '全部 Proxy sessions.alive', icon: 'server'}
                        ];
                    } else if (kind === 'group') {
                        var serverCount = ctrl.rows.reduce(function (sum, row) { return sum + row.serverCount; }, 0);
                        ctrl.metrics = [
                            {key: 'total', title: 'Group 总数', value: ctrl.rows.length, caption: '包含空 Group', icon: 'database'},
                            {key: 'servers', title: 'Redis Server', value: serverCount, caption: '全部 Master 与 Replica', icon: 'server'},
                            {key: 'memory', title: 'Master Memory', value: present(ctrl.cluster.totals.memory) ? Format.bytes(ctrl.cluster.totals.memory) : '—',
                                caption: '仅汇总可用 Master 指标', icon: 'analysis', theme: 'primary'},
                            {key: 'keys', title: 'Master Keys', value: ctrl.cluster.totals.keys, caption: '全部 DB 的 keys 汇总', icon: 'grid'}
                        ];
                    } else {
                        var downValues = ctrl.rows.map(function (row) { return row.mastersDown; }).filter(present);
                        ctrl.metrics = [
                            {key: 'total', title: 'Sentinel 总数', value: ctrl.rows.length, caption: '已注册 Sentinel Server', icon: 'shield'},
                            {key: 'healthy', title: '健康节点', value: ctrl.rows.filter(function (row) { return row.status === 'HEALTHY'; }).length,
                                caption: '无 down master 或 runid 异常', icon: 'check'},
                            {key: 'down', title: 'Down Masters', value: downValues.length ? downValues.reduce(function (a, b) { return a + b; }, 0) : null,
                                caption: 'Sentinel 观测值汇总', icon: 'alert'},
                            {key: 'sync', title: 'HA 配置同步', value: ctrl.cluster.sentinelOutOfSync ? 'OUT_OF_SYNC' : 'IN_SYNC',
                                caption: 'Dashboard Sentinel model', icon: 'refresh', status: ctrl.cluster.sentinelOutOfSync ? 'OUT_OF_SYNC' : 'OK'}
                        ];
                    }
                }

                function updateQuick() {
                    if (!ctrl.quickOpen || !ctrl.quickResource) { return; }
                    var found = null;
                    ctrl.rows.forEach(function (row) {
                        if (String(row.id) === String(ctrl.quickResource.id)) { found = row; }
                    });
                    ctrl.quickResource = found;
                    if (!found) {
                        ctrl.quickOpen = false;
                    } else {
                        ctrl.quickFields = quickFields(found);
                    }
                }

                function sync(state) {
                    if (destroyed || (state && state.product !== ctrl.product)) { return; }
                    ctrl.cluster = state || ClusterStore.state(ctrl.product);
                    buildRows();
                    updateMetrics();
                }

                function load() {
                    var product = ctrl.product;
                    if (!product) { return; }
                    var sequence = ++contextSequence;
                    ClusterStore.load(product).then(function (state) {
                        if (sequence === contextSequence && ctrl.resourceContext(product)) { sync(state); }
                    }).catch(angular.noop);
                }

                ctrl.initialLoading = function () {
                    return !!ctrl.cluster.loading && !ctrl.cluster.updatedAt;
                };
                ctrl.tableError = function () {
                    return !ctrl.cluster.updatedAt ? (ctrl.cluster.error || '') : '';
                };
                ctrl.busy = function () { return ctrl.submitting || ctrl.confirming; };
                ctrl.filtersChanged = function () { applyFilters(true); savePreferences(); };
                ctrl.clearFilters = function () {
                    ctrl.filters = {search: '', status: '', type: ''};
                    applyFilters(true);
                    savePreferences();
                };
                ctrl.refresh = function (manual) {
                    var product = ctrl.product;
                    if (!product || ctrl.cluster.refreshing || ctrl.submitting) { return; }
                    return ClusterStore.refresh(product).then(function (state) {
                        if (ctrl.resourceContext(product)) {
                            sync(state);
                            if (manual) { Toast.success('资源数据已刷新'); }
                        }
                    }).catch(function (response) {
                        if (manual && ctrl.resourceContext(product)) { Toast.error(Api.errorMessage(response)); }
                    });
                };
                ctrl.retry = function () { return ctrl.refresh(false); };
                ctrl.openDetail = function (row) {
                    if (row) { Router.go(ctrl.product, config.detailPage, resourceId(row.resource || row)); }
                };

                function quickFields(row) {
                    if (kind === 'proxy') {
                        return [
                            {key: 'token', label: 'Token', value: row.token || '—'},
                            {key: 'proxy', label: 'proxy_addr', value: row.proxy_addr || '—'},
                            {key: 'admin', label: 'admin_addr', value: row.admin_addr || '—'},
                            {key: 'protocol', label: 'proto_type', value: row.proto_type || '—'},
                            {key: 'dc', label: 'datacenter', value: row.datacenter || '—'},
                            {key: 'hostname', label: 'hostname', value: row.hostname || '—'},
                            {key: 'qps', label: 'QPS', value: present(row.qps) ? Format.number(row.qps) : '—'},
                            {key: 'sessions', label: '活跃连接', value: present(row.sessions) ? Format.number(row.sessions) : '—'},
                            {key: 'switched', label: 'Sentinel switched', value: row.switched ? 'true' : 'false'},
                            {key: 'primary', label: 'primary_only', value: row.primaryOnly ? 'true' : 'false'}
                        ];
                    }
                    if (kind === 'group') {
                        return [
                            {key: 'gid', label: 'gid', value: row.id},
                            {key: 'master', label: 'logical master', value: row.masterDisplay},
                            {key: 'servers', label: 'Server 数', value: row.serverCount},
                            {key: 'memory', label: 'memory', value: present(row.memory) ? Format.bytes(row.memory) : '—'},
                            {key: 'keys', label: 'keys', value: present(row.keys) ? Format.number(row.keys) : '—'},
                            {key: 'ha', label: 'HA', value: row.haState},
                            {key: 'sync', label: 'out_of_sync', value: row.out_of_sync ? 'true' : 'false'},
                            {key: 'action', label: 'action', value: row.actionDisplay}
                        ];
                    }
                    return [
                        {key: 'server', label: 'server_addr', value: row.server},
                        {key: 'masters', label: 'masters', value: present(row.masters) ? Format.number(row.masters) : '—'},
                        {key: 'down', label: 'masters down', value: present(row.mastersDown) ? Format.number(row.mastersDown) : '—'},
                        {key: 'slaves', label: 'avg slaves', value: present(row.averageSlaves) ? Format.number(row.averageSlaves, 2) : '—'},
                        {key: 'sentinels', label: 'avg sentinels', value: present(row.averageSentinels) ? Format.number(row.averageSentinels, 2) : '—'},
                        {key: 'runid', label: 'runidError', value: row.runidError || '—'},
                        {key: 'sync', label: 'out_of_sync', value: ctrl.cluster.sentinelOutOfSync ? 'true' : 'false'}
                    ];
                }

                ctrl.showQuick = function (row) {
                    ctrl.quickResource = row;
                    ctrl.quickFields = quickFields(row);
                    ctrl.quickOpen = true;
                };
                ctrl.closeQuick = function () {
                    if (!ctrl.submitting) { ctrl.quickOpen = false; }
                };

                ctrl.formTitle = function () {
                    return { 'create-proxy': '创建 Proxy', 'create-group': '创建 Group', 'add-server': '添加 Group Server', 'add-sentinel': '添加 Sentinel' }[ctrl.formMode] || '资源操作';
                };
                ctrl.formSubtitle = function () {
                    return ctrl.formMode === 'add-server' ? '空 datacenter 将完全省略对应 URL 路径段' : '提交成功后关闭抽屉并刷新当前集群';
                };
                ctrl.submitLabel = function () { return ctrl.formMode === 'add-server' || ctrl.formMode === 'add-sentinel' ? '添加' : '创建'; };
                ctrl.clearFieldError = function (field) {
                    ctrl.fieldErrors[field] = '';
                    ctrl.drawerError = '';
                };
                ctrl.openForm = function (mode, row) {
                    if (ctrl.busy()) { return; }
                    ctrl.formMode = kind === 'proxy' ? 'create-proxy' : (kind === 'sentinel' ? 'add-sentinel' : (mode === 'add-server' ? 'add-server' : 'create-group'));
                    ctrl.editor = ctrl.formMode === 'add-server' ? {
                        gid: row ? String(row.id) : '', server_addr: '', datacenter: '', lockGid: !!row
                    } : (ctrl.formMode === 'create-proxy' ? {admin_addr: ''} : (ctrl.formMode === 'create-group' ? {gid: ''} : {server_addr: ''}));
                    ctrl.fieldErrors = {};
                    ctrl.drawerError = '';
                    ctrl.formOpen = true;
                    if (ctrl.quickOpen) { ctrl.quickOpen = false; }
                };
                function finishForm(owner) {
                    owner.formOpen = false;
                    owner.editor = {};
                    owner.fieldErrors = {};
                    owner.drawerError = '';
                }
                ctrl.closeForm = function () { if (!ctrl.submitting) { finishForm(ctrl); } };

                function validGid(value) {
                    var text = String(value === undefined || value === null ? '' : value).trim();
                    if (!/^\d+$/.test(text)) { return null; }
                    var gid = Number(text);
                    return gid >= 1 && gid <= 9999 && Math.floor(gid) === gid ? gid : null;
                }

                ctrl.submitForm = function () {
                    if (ctrl.submitting || destroyed) { return; }
                    var product = ctrl.product;
                    var path;
                    var action;
                    var target;
                    var message;
                    ctrl.fieldErrors = {};
                    ctrl.drawerError = '';
                    if (ctrl.formMode === 'create-proxy') {
                        var proxyAddress = String(ctrl.editor.admin_addr || '').trim();
                        ctrl.fieldErrors.address = ResourceActions.hostPortError(proxyAddress, 'admin_addr');
                        if (ctrl.fieldErrors.address) { ctrl.drawerError = '请修正表单后重试'; return; }
                        path = '/api/topom/proxy/create/{xauth}/' + Api.segment(proxyAddress);
                        action = '创建 Proxy'; target = proxyAddress; message = 'Proxy 创建请求已提交';
                    } else if (ctrl.formMode === 'create-group') {
                        var newGid = validGid(ctrl.editor.gid);
                        if (newGid === null) { ctrl.fieldErrors.gid = 'GID 必须是 1 到 9999 的整数'; ctrl.drawerError = '请修正表单后重试'; return; }
                        path = '/api/topom/group/create/{xauth}/' + Api.segment(newGid);
                        action = '创建 Group'; target = 'Group-' + newGid; message = 'Group-' + newGid + ' 已创建';
                    } else if (ctrl.formMode === 'add-server') {
                        var gid = validGid(ctrl.editor.gid);
                        var serverAddress = String(ctrl.editor.server_addr || '').trim();
                        var datacenter = String(ctrl.editor.datacenter || '').trim();
                        if (gid === null) { ctrl.fieldErrors.gid = 'GID 必须是 1 到 9999 的整数'; }
                        ctrl.fieldErrors.address = ResourceActions.hostPortError(serverAddress, 'server_addr');
                        if (/\//.test(datacenter)) { ctrl.fieldErrors.datacenter = 'datacenter 不能包含斜杠'; }
                        if (ctrl.fieldErrors.gid || ctrl.fieldErrors.address || ctrl.fieldErrors.datacenter) {
                            ctrl.drawerError = '请修正表单后重试'; return;
                        }
                        path = '/api/topom/group/add/{xauth}/' + Api.segment(gid) + '/' + Api.segment(serverAddress) +
                            (datacenter ? '/' + Api.segment(datacenter) : '');
                        action = '添加 Server ' + serverAddress; target = 'Group-' + gid; message = 'Server 已添加到 Group-' + gid;
                    } else if (ctrl.formMode === 'add-sentinel') {
                        var sentinelAddress = String(ctrl.editor.server_addr || '').trim();
                        ctrl.fieldErrors.address = ResourceActions.hostPortError(sentinelAddress, 'server_addr');
                        if (ctrl.fieldErrors.address) { ctrl.drawerError = '请修正表单后重试'; return; }
                        path = '/api/topom/sentinels/add/{xauth}/' + Api.segment(sentinelAddress);
                        action = '添加 Sentinel'; target = sentinelAddress; message = 'Sentinel 添加请求已提交';
                    } else {
                        return;
                    }
                    ResourceActions.execute(ctrl, product, {
                        path: path, action: action, target: target, successMessage: message,
                        errorField: 'drawerError', close: finishForm
                    });
                };

                function targetFor(row) {
                    return kind === 'group' ? 'Group-' + row.id : (kind === 'proxy' ? row.token : row.server);
                }

                function afterRemove(row) {
                    if (ctrl.quickResource && String(ctrl.quickResource.id) === String(row.id)) {
                        ctrl.quickOpen = false;
                    }
                }

                ctrl.removeLabel = function (row) {
                    if (kind === 'group') { return '删除空 Group'; }
                    return row.status === 'ERROR' ? '强制移除' : '移除';
                };

                ctrl.reinitProxy = function (row) {
                    if (!row || ctrl.busy()) { return; }
                    var conflicts = ResourceActions.haConflicts(ctrl.cluster);
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: 'Reinit and Start Proxy？',
                        message: conflicts.length ?
                            '检测到 HA real master 与 logical master 冲突。继续 Reinit 可能按错误拓扑恢复 Proxy 流量，请先修复冲突；坚持继续即表示接受风险。' :
                            '将重新初始化并启动 Proxy ' + row.token + '，现有连接可能受到影响。',
                        details: conflicts.length ? conflicts : 'Token: ' + row.token,
                        confirmText: conflicts.length ? '了解风险，仍要 Reinit' : '确认 Reinit', danger: conflicts.length > 0
                    }, {
                        path: '/api/topom/proxy/reinit/{xauth}/' + Api.segment(row.token),
                        action: 'Reinit Proxy', target: row.token, successMessage: 'Proxy ' + row.token + ' 已完成 Reinit 请求'
                    });
                };

                ctrl.resyncGroup = function (row) {
                    if (!row || ctrl.busy()) { return; }
                    var conflicts = ResourceActions.haConflicts(ctrl.cluster, [row.resource || row]);
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: 'Resync Group-' + row.id + '？',
                        message: conflicts.length ?
                            'HA real master 与 logical master 不一致。继续将按 Dashboard logical 拓扑重新同步，可能覆盖真实主从关系。' :
                            '将按 Dashboard 中的 logical 拓扑重新同步 Group-' + row.id + '。',
                        details: conflicts.length ? conflicts : row.masterDisplay,
                        confirmText: conflicts.length ? '了解风险，仍要 Resync' : '确认 Resync', danger: conflicts.length > 0
                    }, {
                        path: '/api/topom/group/resync/{xauth}/' + Api.segment(row.id),
                        action: 'Resync Group', target: targetFor(row), successMessage: 'Group-' + row.id + ' 已重新同步'
                    });
                };

                ctrl.resyncAllGroups = function () {
                    if (ctrl.busy()) { return; }
                    var conflicts = ResourceActions.haConflicts(ctrl.cluster);
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: 'Resync All Groups？',
                        message: conflicts.length ? '至少一个 Group 的 HA real master 与 logical master 冲突；全量 Resync 可能覆盖真实主从关系。' : '将重新同步当前集群全部 Group 的 logical 拓扑。',
                        details: conflicts.length ? conflicts : ctrl.rows.map(function (row) { return row.name; }).join(', '),
                        confirmText: conflicts.length ? '了解风险，仍要全部 Resync' : '确认全部 Resync', danger: conflicts.length > 0
                    }, {
                        path: '/api/topom/group/resync-all/{xauth}', action: 'Resync All Groups', target: '全部 Group',
                        successMessage: '全部 Group 的 Resync 请求已完成'
                    });
                };

                ctrl.resyncSentinels = function () {
                    if (ctrl.busy()) { return; }
                    var conflicts = ResourceActions.haConflicts(ctrl.cluster);
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: 'Resync All Sentinels？',
                        message: conflicts.length ? 'HA real master 与 logical master 冲突。继续全量同步可能把错误拓扑写入 Sentinel，请先修复冲突。' : '将按 Dashboard 当前拓扑重新同步全部 Sentinel。',
                        details: conflicts.length ? conflicts : ctrl.rows.map(function (row) { return row.server; }).join('\n'),
                        confirmText: conflicts.length ? '了解风险，仍要同步全部 Sentinel' : '确认全部同步', danger: conflicts.length > 0
                    }, {
                        path: '/api/topom/sentinels/resync-all/{xauth}', action: 'Resync All Sentinels', target: '全部 Sentinel',
                        successMessage: '全部 Sentinel 的 Resync 请求已完成'
                    });
                };

                ctrl.removeResource = function (row) {
                    if (!row || ctrl.busy() || (kind === 'group' && !row.canRemove)) { return; }
                    var force = kind !== 'group' && row.status === 'ERROR';
                    var path;
                    if (kind === 'proxy') {
                        path = '/api/topom/proxy/remove/{xauth}/' + Api.segment(row.token) + (force ? '/1' : '/0');
                    } else if (kind === 'group') {
                        path = '/api/topom/group/remove/{xauth}/' + Api.segment(row.id);
                    } else {
                        path = '/api/topom/sentinels/del/{xauth}/' + Api.segment(row.server) + (force ? '/1' : '/0');
                    }
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: (force ? '强制移除 ' : '移除 ') + targetFor(row) + '？',
                        message: kind === 'proxy' ? '将移除并 Shutdown 该 Proxy。' + (force ? '当前状态为 ERROR，将使用 force=1；这可能绕过在线状态检查。' : '现有客户端连接会中断。') :
                            (kind === 'group' ? '仅删除当前空 Group；此操作不可撤销。' : '将移除该 Sentinel 的注册信息。' + (force ? '当前状态为 ERROR，将使用 force=1。' : '高可用监控覆盖可能下降。')),
                        details: targetFor(row), confirmText: force ? '确认强制移除' : '确认移除', danger: true
                    }, {
                        path: path, action: force ? '强制移除资源' : '移除资源', target: targetFor(row),
                        successMessage: targetFor(row) + ' 已移除', onSuccess: function () { afterRemove(row); }
                    });
                };

                ctrl.replicaAll = function (value) {
                    if (ctrl.busy()) { return; }
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: value ? '启用全部 Replica Group？' : '停用全部 Replica Group？',
                        message: '将修改当前集群所有 Group 中全部 Server 的 replica_group 标记。',
                        confirmText: value ? '确认全部启用' : '确认全部停用', danger: !value
                    }, {
                        path: '/api/topom/group/replica-groups-all/{xauth}/' + value,
                        action: value ? '启用全部 Replica Group' : '停用全部 Replica Group', target: '全部 Group',
                        successMessage: '全部 Server 的 replica_group 已更新'
                    });
                };

                ctrl.rowAction = function (row, action) {
                    if (!row || ctrl.busy()) { return; }
                    if (action === 'quick') { ctrl.showQuick(row); }
                    else if (action === 'reinit') { ctrl.reinitProxy(row); }
                    else if (action === 'resync') { ctrl.resyncGroup(row); }
                    else if (action === 'add-server') { ctrl.openForm('add-server', row); }
                    else if (action === 'remove') { ctrl.removeResource(row); }
                    else if (action === 'proxy-link') { ResourceActions.openExternal(row.proxyUrl); }
                    else if (action === 'stats-link') { ResourceActions.openExternal(row.statsUrl); }
                    else if (action === 'info-link') { ResourceActions.openExternal(row.infoUrl); }
                    else if (action === 'monitored-link') { ResourceActions.openExternal(row.monitoredUrl); }
                };

                function batchConfirmation(action, row, index, total) {
                    if (action === 'reinit') {
                        var conflicts = ResourceActions.haConflicts(ctrl.cluster);
                        return {
                            title: '批量 Reinit（' + index + '/' + total + '）：' + row.token,
                            message: conflicts.length ? '当前集群存在 HA real master 与 logical master 冲突；继续可能影响 Proxy 恢复后的流量拓扑。' : '确认重新初始化并启动这个 Proxy？',
                            details: conflicts.length ? conflicts : row.token,
                            confirmText: conflicts.length ? '了解风险，确认此项' : '确认此项', danger: conflicts.length > 0
                        };
                    }
                    if (action === 'resync') {
                        var groupConflicts = ResourceActions.haConflicts(ctrl.cluster, [row.resource]);
                        return {
                            title: '批量 Resync（' + index + '/' + total + '）：Group-' + row.id,
                            message: groupConflicts.length ? '该 Group 的 HA real master 与 logical master 冲突，继续可能覆盖真实主从关系。' : '确认按 logical 拓扑重新同步这个 Group？',
                            details: groupConflicts.length ? groupConflicts : row.masterDisplay,
                            confirmText: groupConflicts.length ? '了解风险，确认此项' : '确认此项', danger: groupConflicts.length > 0
                        };
                    }
                    var force = row.status === 'ERROR';
                    return {
                        title: '批量移除（' + index + '/' + total + '）：' + targetFor(row),
                        message: kind === 'proxy' ? '将移除并 Shutdown 此 Proxy。' + (force ? '将使用 force=1。' : '') : '将移除此 Sentinel。' + (force ? '将使用 force=1。' : ''),
                        confirmText: force ? '确认强制移除此项' : '确认移除此项', danger: true
                    };
                }

                ctrl.batchAction = function (action) {
                    if (ctrl.busy()) { return; }
                    var selected = {};
                    ctrl.selected.forEach(function (id) { selected[String(id)] = true; });
                    var rows = ctrl.rows.filter(function (row) { return selected[String(row.id)]; });
                    if (!rows.length) { return; }
                    var product = ctrl.product;
                    ResourceActions.confirmEach(ctrl, product, rows, function (row, index, total) {
                        return batchConfirmation(action, row, index, total);
                    }).then(function (confirmed) {
                        if (!ctrl.resourceContext(product) || !confirmed.length) { return; }
                        var jobs = confirmed.map(function (row) {
                            var path;
                            if (action === 'reinit') {
                                path = '/api/topom/proxy/reinit/{xauth}/' + Api.segment(row.token);
                            } else if (action === 'resync') {
                                path = '/api/topom/group/resync/{xauth}/' + Api.segment(row.id);
                            } else if (kind === 'proxy') {
                                path = '/api/topom/proxy/remove/{xauth}/' + Api.segment(row.token) + (row.status === 'ERROR' ? '/1' : '/0');
                            } else {
                                path = '/api/topom/sentinels/del/{xauth}/' + Api.segment(row.server) + (row.status === 'ERROR' ? '/1' : '/0');
                            }
                            return {
                                path: path, row: row, target: targetFor(row),
                                action: action === 'reinit' ? '批量 Reinit Proxy' : (action === 'resync' ? '批量 Resync Group' : '批量移除资源')
                            };
                        });
                        ResourceActions.batch(ctrl, product, jobs, {
                            successMessage: action === 'remove' ? '批量移除已部分或全部完成' : '批量操作已部分或全部完成',
                            onComplete: function (results) {
                                ctrl.selected = results.filter(function (result) { return !result.ok; }).map(function (result) { return result.job.row.id; });
                                confirmed.forEach(function (row) {
                                    var result = results.filter(function (item) { return item.job.row === row; })[0];
                                    if (action === 'remove' && result && result.ok) { afterRemove(row); }
                                });
                            }
                        });
                    });
                };

                function activate() {
                    contextSequence++;
                    ctrl._mutationSequence = (ctrl._mutationSequence || 0) + 1;
                    ctrl.submitting = false;
                    ctrl.confirming = false;
                    ctrl.operationError = '';
                    ctrl.formOpen = false;
                    ctrl.quickOpen = false;
                    ctrl.quickResource = null;
                    ctrl.selected = [];
                    loadPreferences();
                    ctrl.cluster = ClusterStore.state(ctrl.product || 'default');
                    sync(ctrl.cluster);
                    load();
                }

                ctrl.$onInit = function () {
                    activated = true;
                    activate();
                    $scope.$watch(function () { return ctrl.page; }, savePreferences);
                    $scope.$watch(function () { return ctrl.sortKey; }, savePreferences);
                    $scope.$watch(function () { return ctrl.sortDirection; }, savePreferences);
                };
                ctrl.$onChanges = function (changes) {
                    if (activated && changes.product && !changes.product.isFirstChange() && changes.product.currentValue !== changes.product.previousValue) {
                        activate();
                    }
                };
                ctrl.$onDestroy = function () {
                    destroyed = true;
                    contextSequence++;
                    ctrl._mutationSequence = (ctrl._mutationSequence || 0) + 1;
                };

                $scope.$on('cluster:updated', function (event, payload) {
                    if (!destroyed && payload && payload.product === ctrl.product) { sync(payload.state); }
                });
                $scope.$on('cluster:refreshRequested', function (event, payload) {
                    if (!destroyed && payload && payload.product === ctrl.product) { ctrl.refresh(false); }
                });
            };
        }
    ]);

    dashboard.component('proxiesPage', {
        bindings: {product: '<'},
        template: listTemplate,
        controller: ['ResourceListFactory', '$scope', function (ResourceListFactory, $scope) {
            ResourceListFactory(this, 'proxy', $scope);
        }]
    });

    dashboard.component('groupsPage', {
        bindings: {product: '<'},
        template: listTemplate,
        controller: ['ResourceListFactory', '$scope', function (ResourceListFactory, $scope) {
            ResourceListFactory(this, 'group', $scope);
        }]
    });

    dashboard.component('sentinelsPage', {
        bindings: {product: '<'},
        template: listTemplate,
        controller: ['ResourceListFactory', '$scope', function (ResourceListFactory, $scope) {
            ResourceListFactory(this, 'sentinel', $scope);
        }]
    });
}());
