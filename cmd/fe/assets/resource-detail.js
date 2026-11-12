'use strict';

(function () {
    var detailTemplate = `
        <div class="resource-detail-page">
            <div class="page-header-actions">
                <button class="button button-secondary" type="button" ng-click="$ctrl.backToList()">
                    <ui-icon name="chevron-left" size="16"></ui-icon>返回{{$ctrl.listTitle}}
                </button>
            </div>

            <loading-skeleton ng-if="$ctrl.initialLoading()" variant="page"></loading-skeleton>
            <empty-state ng-if="$ctrl.initialError()" icon="alert" title="无法加载资源详情"
                description="{{$ctrl.cluster.error}}" action-label="重新加载" on-action="$ctrl.refresh(true)"></empty-state>
            <empty-state ng-if="$ctrl.notFound()" icon="search" title="资源不存在或已移除"
                description="当前集群中找不到该资源，请返回列表或刷新后重试。" action-label="返回列表" on-action="$ctrl.backToList()"></empty-state>

            <div ng-if="$ctrl.resource">
                <page-header title="{{$ctrl.resource.name}}" description="{{$ctrl.detailSubtitle()}}">
                    <status-tag status="$ctrl.resource.status"></status-tag>
                    <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'proxy'"
                        ng-click="$ctrl.reinitProxy()" ng-disabled="$ctrl.busy()"><ui-icon name="refresh" size="15"></ui-icon>Reinit</button>
                    <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'group'"
                        ng-click="$ctrl.openAddServer()" ng-disabled="$ctrl.busy()"><ui-icon name="plus" size="15"></ui-icon>添加 Server</button>
                    <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'group'"
                        ng-click="$ctrl.resyncGroup()" ng-disabled="$ctrl.busy()"><ui-icon name="refresh" size="15"></ui-icon>Resync</button>
                    <button class="button button-secondary" type="button" ng-if="$ctrl.kind === 'sentinel'"
                        ng-click="$ctrl.resyncSentinels()" ng-disabled="$ctrl.busy()"><ui-icon name="refresh" size="15"></ui-icon>Resync All</button>
                    <button class="button button-danger" type="button" ng-if="$ctrl.kind !== 'group' || $ctrl.resource.canRemove"
                        ng-click="$ctrl.removeResource()" ng-disabled="$ctrl.busy()"><ui-icon name="trash" size="15"></ui-icon>{{$ctrl.removeLabel()}}</button>
                </page-header>

                <div class="inline-alert inline-alert--error" ng-if="$ctrl.cluster.error" role="alert">
                    <ui-icon name="alert" size="18"></ui-icon>
                    <div><strong>刷新失败，当前展示上次成功加载的数据</strong><p>{{$ctrl.cluster.error}}</p>
                        <button class="button button-secondary" type="button" ng-click="$ctrl.refresh(true)">重试</button></div>
                </div>
                <div class="inline-alert inline-alert--error" ng-if="$ctrl.operationError" role="alert">
                    <ui-icon name="alert" size="18"></ui-icon><span>{{$ctrl.operationError}}</span>
                </div>
                <div class="inline-alert inline-alert--warning" ng-if="$ctrl.kind === 'group' && $ctrl.resource.haWarning" role="alert">
                    <ui-icon name="alert" size="18"></ui-icon><span>Sentinel 观测到的 master {{$ctrl.resource.haWarning}} 不在当前 Group 中。</span>
                </div>

                <div class="metrics-grid">
                    <metric-card ng-repeat="metric in $ctrl.metrics track by metric.key" metric="metric"></metric-card>
                </div>

                <div class="tabs" role="tablist" aria-label="资源详情视图">
                    <button id="resource-tab-overview" class="tab-button" type="button" role="tab"
                        ng-class="{active: $ctrl.activeTab === 'overview'}" ng-click="$ctrl.setTab('overview')"
                        aria-controls="resource-panel-overview" ng-attr-aria-selected="{{$ctrl.activeTab === 'overview'}}">
                        <ui-icon name="grid" size="15"></ui-icon>概览
                    </button>
                    <button id="resource-tab-monitor" class="tab-button" type="button" role="tab"
                        ng-class="{active: $ctrl.activeTab === 'monitor'}" ng-click="$ctrl.setTab('monitor')"
                        aria-controls="resource-panel-monitor" ng-attr-aria-selected="{{$ctrl.activeTab === 'monitor'}}">
                        <ui-icon name="activity" size="15"></ui-icon>监控
                    </button>
                    <button id="resource-tab-config" class="tab-button" type="button" role="tab"
                        ng-class="{active: $ctrl.activeTab === 'config'}" ng-click="$ctrl.setTab('config')"
                        aria-controls="resource-panel-config" ng-attr-aria-selected="{{$ctrl.activeTab === 'config'}}">
                        <ui-icon name="settings" size="15"></ui-icon>配置
                    </button>
                    <button id="resource-tab-activity" class="tab-button" type="button" role="tab"
                        ng-class="{active: $ctrl.activeTab === 'activity'}" ng-click="$ctrl.setTab('activity')"
                        aria-controls="resource-panel-activity" ng-attr-aria-selected="{{$ctrl.activeTab === 'activity'}}">
                        <ui-icon name="history" size="15"></ui-icon>操作记录
                    </button>
                </div>

                <section id="resource-panel-overview" role="tabpanel" aria-labelledby="resource-tab-overview" ng-show="$ctrl.activeTab === 'overview'">
                    <div style="display:grid;grid-template-columns:minmax(220px,240px) minmax(0,1fr);gap:16px;align-items:stretch;margin-top:16px">
                        <section class="panel">
                            <div class="card-header"><div><h2 class="card-title">资源标识</h2><p>{{$ctrl.kindLabel}}</p></div></div>
                            <dl>
                                <div class="key-value-item" ng-repeat="field in $ctrl.identityFields track by field.key">
                                    <dt>{{field.label}}</dt><dd>{{field.value}}</dd>
                                </div>
                            </dl>
                            <div class="card-actions" ng-if="$ctrl.kind === 'proxy' && ($ctrl.resource.proxyUrl || $ctrl.resource.statsUrl)">
                                <a class="button button-secondary" ng-if="$ctrl.resource.proxyUrl" ng-href="{{$ctrl.resource.proxyUrl}}"
                                    target="_blank" rel="noopener noreferrer"><ui-icon name="link" size="15"></ui-icon>Proxy</a>
                                <a class="button button-secondary" ng-if="$ctrl.resource.statsUrl" ng-href="{{$ctrl.resource.statsUrl}}"
                                    target="_blank" rel="noopener noreferrer"><ui-icon name="link" size="15"></ui-icon>Stats</a>
                            </div>
                            <div class="card-actions" ng-if="$ctrl.kind === 'sentinel' && ($ctrl.resource.infoUrl || $ctrl.resource.monitoredUrl)">
                                <a class="button button-secondary" ng-if="$ctrl.resource.infoUrl" ng-href="{{$ctrl.resource.infoUrl}}"
                                    target="_blank" rel="noopener noreferrer"><ui-icon name="link" size="15"></ui-icon>Info</a>
                                <a class="button button-secondary" ng-if="$ctrl.resource.monitoredUrl" ng-href="{{$ctrl.resource.monitoredUrl}}"
                                    target="_blank" rel="noopener noreferrer"><ui-icon name="link" size="15"></ui-icon>Monitored</a>
                            </div>
                        </section>
                        <chart-card ng-if="$ctrl.kind !== 'sentinel'" title="{{$ctrl.overviewChart.title}}" unit="{{$ctrl.overviewChart.unit}}"
                            subtitle="来自 ClusterStore 的本地采样历史" on-expand="$ctrl.expandChart($ctrl.overviewChart)">
                            <trend-chart series="$ctrl.overviewChart.series" height="260" unit="{{$ctrl.overviewChart.unit}}"></trend-chart>
                        </chart-card>
                        <section class="panel" ng-if="$ctrl.kind === 'sentinel'">
                            <empty-state icon="analysis" title="暂无 Sentinel 历史趋势"
                                description="ClusterStore 未提供 Sentinel 历史序列；这里不会生成或填充模拟趋势。可通过 Info 与 Monitored 查看节点实时信息。"></empty-state>
                        </section>
                    </div>

                    <section class="panel">
                        <div class="card-header"><div><h2 class="card-title">基础信息</h2><p>数据来自最近一次成功的 Dashboard 拓扑采集</p></div>
                            <span class="form-hint">更新于 {{$ctrl.cluster.updatedAt ? ($ctrl.cluster.updatedAt | date:'HH:mm:ss') : '—'}}</span></div>
                        <dl class="key-value-grid">
                            <div class="key-value-item" ng-repeat="field in $ctrl.basicFields track by field.key">
                                <dt>{{field.label}}</dt><dd>{{field.value}}</dd>
                            </div>
                        </dl>
                    </section>

                    <section class="panel" ng-if="$ctrl.kind === 'group'">
                        <div class="card-header">
                            <div><h2 class="card-title">Group Servers</h2><p>Logical master 位于第一位；全部 Server 操作均可在此完成。</p></div>
                            <div class="card-actions">
                                <button class="button button-secondary" type="button" ng-click="$ctrl.replicaAll(1)" ng-disabled="$ctrl.busy()">全部启用 Replica Group</button>
                                <button class="button button-secondary" type="button" ng-click="$ctrl.replicaAll(0)" ng-disabled="$ctrl.busy()">全部停用 Replica Group</button>
                            </div>
                        </div>
                        <empty-state ng-if="!$ctrl.resource.servers.length" icon="database" title="这是一个空 Group"
                            description="添加 Redis Server 后，可在这里检查复制、HA 与同步状态。" action-label="添加 Server" on-action="$ctrl.openAddServer()"></empty-state>
                        <div class="data-table-scroll" ng-if="$ctrl.resource.servers.length">
                            <table class="result-table">
                                <thead><tr><th>Server</th><th>状态</th><th>角色 / Master</th><th>Memory</th><th>Max Memory</th><th>Keys</th><th>HA</th><th>Sync Action</th><th>Replica Group</th><th>操作</th></tr></thead>
                                <tbody>
                                    <tr ng-repeat="server in $ctrl.resource.servers track by server.server">
                                        <td><strong>{{server.server}}</strong><br><small>{{server.datacenter || '—'}}</small></td>
                                        <td><status-tag status="server.status"></status-tag><small ng-if="server.isPromoting">PROMOTING</small></td>
                                        <td>{{server.isPrimary ? 'Logical Master' : 'Replica'}}<br><small>{{server.master}}</small></td>
                                        <td>{{$ctrl.bytes(server.memory)}}</td>
                                        <td>{{server.maxMemory === 0 ? 'INF.' : $ctrl.bytes(server.maxMemory)}}</td>
                                        <td>{{$ctrl.number(server.keys)}}</td>
                                        <td><status-tag status="$ctrl.haTone(server)" label="{{$ctrl.haLabel(server)}}"></status-tag></td>
                                        <td>{{$ctrl.serverActionText(server)}}</td>
                                        <td><status-tag status="server.replica_group ? 'ENABLED' : 'DISABLED'" label="{{server.replica_group ? 'ENABLED' : 'DISABLED'}}"></status-tag></td>
                                        <td>
                                            <div class="table-actions">
                                                <button class="table-action" type="button" ng-click="$ctrl.promoteServer(server)"
                                                    ng-disabled="$ctrl.busy() || !server.canPromote" title="Promote 为 Logical Master">Promote</button>
                                                <button class="table-action" type="button" ng-click="$ctrl.toggleReplica(server)" ng-disabled="$ctrl.busy()">
                                                    {{server.replica_group ? '停用 Replica' : '启用 Replica'}}
                                                </button>
                                                <button class="table-action" type="button" ng-click="$ctrl.syncAction(server)" ng-disabled="$ctrl.busy()">
                                                    {{server.actionState === 'pending' ? 'Remove Sync Action' : 'Create Sync Action'}}
                                                </button>
                                                <button class="table-action table-action--danger" type="button" ng-click="$ctrl.removeServer(server)"
                                                    ng-disabled="$ctrl.busy() || !server.canRemove" title="{{server.canRemove ? 'Remove Server' : '有 Replica 时不可移除 Master，或 Promote 进行中'}}">Remove</button>
                                            </div>
                                        </td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>
                    </section>
                </section>

                <section id="resource-panel-monitor" role="tabpanel" aria-labelledby="resource-tab-monitor" ng-show="$ctrl.activeTab === 'monitor'">
                    <empty-state ng-if="$ctrl.kind === 'sentinel'" icon="analysis" title="Sentinel 暂无历史监控序列"
                        description="当前数据源只提供 Sentinel 实时统计，没有可用于绘图的历史样本。请使用概览中的 Info / Monitored 外链查看实时状态。"></empty-state>
                    <div ng-if="$ctrl.kind !== 'sentinel'">
                        <div class="monitor-toolbar">
                            <div class="monitor-toolbar__group" aria-label="监控时间范围">
                                <button class="button" type="button" ng-repeat="range in $ctrl.ranges"
                                    ng-class="$ctrl.monitorRange === range.value ? 'button-primary' : 'button-secondary'"
                                    ng-click="$ctrl.setRange(range.value)">{{range.label}}</button>
                                <button class="button button-secondary" type="button" ng-click="$ctrl.refresh(true)" ng-disabled="$ctrl.cluster.refreshing || $ctrl.submitting">
                                    <ui-icon name="refresh" size="15" ng-class="{'is-spinning': $ctrl.cluster.refreshing}"></ui-icon>手动刷新
                                </button>
                                <label class="form-hint"><input type="checkbox" ng-model="$ctrl.autoRefresh" ng-change="$ctrl.toggleAutoRefresh()"> 自动刷新（15s）</label>
                            </div>
                            <div class="monitor-toolbar__group" aria-label="监控布局">
                                <span class="form-hint">布局</span>
                                <button class="button" type="button" ng-class="$ctrl.monitorLayout === 2 ? 'button-primary' : 'button-secondary'"
                                    ng-click="$ctrl.setLayout(2)"><ui-icon name="columns" size="15"></ui-icon>2 列</button>
                                <button class="button" type="button" ng-class="$ctrl.monitorLayout === 3 ? 'button-primary' : 'button-secondary'"
                                    ng-click="$ctrl.setLayout(3)"><ui-icon name="grid" size="15"></ui-icon>3 列</button>
                            </div>
                        </div>
                        <div class="panel">
                            <span class="form-label">指标筛选</span>
                            <label class="form-hint" ng-repeat="metric in $ctrl.availableMetrics track by metric.key">
                                <input type="checkbox" ng-model="$ctrl.metricFilter[metric.key]" ng-change="$ctrl.metricsChanged()"> {{metric.title}}（{{metric.unit || 'count'}}）
                            </label>
                        </div>
                        <empty-state ng-if="!$ctrl.visibleCharts.length" icon="filter" title="未选择监控指标"
                            description="至少选择一个指标后显示趋势图。"></empty-state>
                        <div class="chart-grid" ng-class="'columns-' + $ctrl.monitorLayout">
                            <chart-card ng-repeat="chart in $ctrl.visibleCharts track by chart.key" title="{{chart.title}}" unit="{{chart.unit}}"
                                subtitle="{{chart.subtitle}}" on-expand="$ctrl.expandChart(chart)">
                                <trend-chart series="chart.series" height="240" unit="{{chart.unit}}"></trend-chart>
                            </chart-card>
                        </div>
                    </div>
                </section>

                <section id="resource-panel-config" role="tabpanel" aria-labelledby="resource-tab-config" ng-show="$ctrl.activeTab === 'config'">
                    <section class="panel">
                        <div class="card-header"><div><h2 class="card-title">{{$ctrl.kind === 'proxy' ? 'Proxy Model 配置' : '资源配置'}}</h2>
                            <p>{{$ctrl.kind === 'proxy' ? '完整列出原始 model 的非敏感字段；嵌套值以 JSON 展示。' : '展示当前资源的非敏感配置与状态标识。'}}</p></div></div>
                        <dl class="key-value-grid" ng-if="$ctrl.configEntries.length">
                            <div class="key-value-item" ng-repeat="field in $ctrl.configEntries track by field.key">
                                <dt>{{field.key}}</dt><dd>{{field.value}}</dd>
                            </div>
                        </dl>
                        <empty-state ng-if="!$ctrl.configEntries.length" icon="settings" title="暂无可展示的配置"
                            description="Dashboard 尚未返回该资源的配置字段。"></empty-state>
                    </section>
                </section>

                <section id="resource-panel-activity" role="tabpanel" aria-labelledby="resource-tab-activity" ng-show="$ctrl.activeTab === 'activity'">
                    <section class="panel">
                        <div class="card-header"><div><h2 class="card-title">本地操作记录</h2><p>仅记录操作名称、资源标识、结果与时间，不记录表单或敏感数据。</p></div></div>
                        <empty-state ng-if="!$ctrl.activities.length" icon="history" title="暂无操作记录"
                            description="此浏览器中还没有针对当前资源的写操作记录。"></empty-state>
                        <div class="data-table-scroll" ng-if="$ctrl.activities.length">
                            <table class="result-table">
                                <thead><tr><th>时间</th><th>操作</th><th>资源</th><th>结果</th></tr></thead>
                                <tbody><tr ng-repeat="item in $ctrl.activities track by item.id">
                                    <td>{{item.time | date:'yyyy-MM-dd HH:mm:ss'}}</td><td>{{item.action}}</td><td>{{item.target}}</td>
                                    <td><status-tag status="item.result === 'success' ? 'SUCCESS' : 'ERROR'" label="{{item.result}}"></status-tag></td>
                                </tr></tbody>
                            </table>
                        </div>
                    </section>
                </section>
            </div>

            <resource-drawer open="$ctrl.addServerOpen" title="'添加 Server 到 Group-' + $ctrl.resourceId"
                subtitle="'datacenter 留空时省略对应 URL 路径段'" width="600" submitting="$ctrl.submitting"
                submit-disabled="$ctrl.submitting" submit-label="添加 Server" show-submit="true" error="$ctrl.drawerError"
                on-close="$ctrl.closeAddServer()" on-submit="$ctrl.submitAddServer()">
                <div class="form-grid">
                    <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.gid}">
                        <label class="form-label" for="detail-group-gid">gid <span class="required">*</span></label>
                        <input id="detail-group-gid" class="form-control" type="text" ng-model="$ctrl.addServerForm.gid" readonly>
                        <p class="form-error" ng-if="$ctrl.fieldErrors.gid">{{$ctrl.fieldErrors.gid}}</p>
                    </div>
                    <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.address}">
                        <label class="form-label" for="detail-group-address">server_addr <span class="required">*</span></label>
                        <input id="detail-group-address" class="form-control" type="text" ng-model="$ctrl.addServerForm.server_addr"
                            ng-disabled="$ctrl.submitting" ng-change="$ctrl.clearFieldError('address')"
                            placeholder="redis.example.com:6379" autocomplete="off" spellcheck="false">
                        <p class="form-error" ng-if="$ctrl.fieldErrors.address" role="alert">{{$ctrl.fieldErrors.address}}</p>
                    </div>
                    <div class="form-group form-group--full" ng-class="{'has-error': $ctrl.fieldErrors.datacenter}">
                        <label class="form-label" for="detail-group-datacenter">datacenter（可选）</label>
                        <input id="detail-group-datacenter" class="form-control" type="text" ng-model="$ctrl.addServerForm.datacenter"
                            ng-disabled="$ctrl.submitting" ng-change="$ctrl.clearFieldError('datacenter')" autocomplete="off">
                        <p class="form-error" ng-if="$ctrl.fieldErrors.datacenter" role="alert">{{$ctrl.fieldErrors.datacenter}}</p>
                    </div>
                </div>
            </resource-drawer>

            <resource-drawer open="$ctrl.expandedOpen" title="$ctrl.expandedTitle" subtitle="$ctrl.rangeLabel() + ' · 来自 ClusterStore 历史采样'"
                width="1100" show-submit="false" on-close="$ctrl.expandedOpen = false">
                <trend-chart series="$ctrl.expandedSeries" height="520" unit="{{$ctrl.expandedUnit}}"></trend-chart>
            </resource-drawer>
        </div>
    `;

    dashboard.factory('ResourceDetailFactory', ['Api', 'Router', 'ClusterStore', 'Preferences', 'ResourceActions', 'Activity', 'Format', 'Toast', '$interval',
        function (Api, Router, ClusterStore, Preferences, ResourceActions, Activity, Format, Toast, $interval) {
            var configs = {
                proxy: {listPage: 'proxies', listTitle: 'Proxy 列表', kindLabel: 'Codis Proxy', collection: 'proxies'},
                group: {listPage: 'groups', listTitle: 'Group 列表', kindLabel: 'Codis Group', collection: 'groups'},
                sentinel: {listPage: 'sentinels', listTitle: 'Sentinel 列表', kindLabel: 'Codis Sentinel', collection: 'sentinels'}
            };

            return function (ctrl, kind, $scope) {
                var config = configs[kind];
                var destroyed = false;
                var activated = false;
                var contextSequence = 0;
                var refreshTimer = null;
                var monitorPreferenceKey = '';

                ctrl.kind = kind;
                ctrl.kindLabel = config.kindLabel;
                ctrl.listTitle = config.listTitle;
                ctrl.cluster = ClusterStore.state(ctrl.product || 'default');
                ctrl.resource = null;
                ctrl.metrics = [];
                ctrl.identityFields = [];
                ctrl.basicFields = [];
                ctrl.configEntries = [];
                ctrl.activities = [];
                ctrl.activeTab = 'overview';
                ctrl.ranges = [{value: '15m', label: '15 分钟'}, {value: '1h', label: '1 小时'}, {value: 'all', label: '全部'}];
                ctrl.monitorRange = '1h';
                ctrl.monitorLayout = 2;
                ctrl.autoRefresh = false;
                ctrl.metricFilter = {};
                ctrl.availableMetrics = kind === 'proxy' ? [
                    {key: 'qps', title: 'QPS', unit: 'ops/s', subtitle: 'ops.qps'},
                    {key: 'sessions', title: '活跃连接', unit: 'connections', subtitle: 'sessions.alive'},
                    {key: 'fails', title: '失败命令', unit: 'commands', subtitle: 'ops.fails'}
                ] : (kind === 'group' ? [
                    {key: 'memory', title: 'Master Memory', unit: 'B', subtitle: 'Master used_memory'},
                    {key: 'keys', title: 'Master Keys', unit: 'keys', subtitle: '全部 DB keys 汇总'}
                ] : []);
                ctrl.visibleCharts = [];
                ctrl.overviewChart = {key: '', title: '趋势', unit: '', series: []};
                ctrl.expandedOpen = false;
                ctrl.expandedMetric = '';
                ctrl.expandedTitle = '';
                ctrl.expandedUnit = '';
                ctrl.expandedSeries = [];
                ctrl.addServerOpen = false;
                ctrl.addServerForm = {gid: '', server_addr: '', datacenter: ''};
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                ctrl.submitting = false;
                ctrl.confirming = false;
                ctrl.operationError = '';
                ctrl.number = Format.number;
                ctrl.bytes = Format.bytes;
                ctrl.resourceContext = function (product, resourceId) {
                    return !destroyed && ctrl.product === product &&
                        (resourceId === undefined || String(ctrl.resourceId) === String(resourceId));
                };

                function present(value) { return value !== undefined && value !== null && value !== ''; }
                function target() {
                    return kind === 'group' ? 'Group-' + ctrl.resourceId : (kind === 'proxy' ? ctrl.resourceId : ctrl.resourceId);
                }
                function matches(resource) {
                    if (kind === 'proxy') { return resource.token === ctrl.resourceId; }
                    if (kind === 'group') { return String(resource.id) === String(ctrl.resourceId); }
                    return resource.server === ctrl.resourceId;
                }
                function findRawProxyModel() {
                    var raw = ctrl.cluster.raw || {};
                    var models = raw.proxy && angular.isArray(raw.proxy.models) ? raw.proxy.models : [];
                    var found = null;
                    models.forEach(function (model) {
                        if (model && model.token === ctrl.resourceId) { found = model; }
                    });
                    return found;
                }
                function sensitive(key) {
                    return /(password|passwd|secret|credential|auth|private[._-]?key|access[._-]?key)/i.test(key);
                }
                function nonSensitiveValue(value) {
                    if (angular.isArray(value)) {
                        return value.map(nonSensitiveValue);
                    }
                    if (angular.isObject(value)) {
                        var result = {};
                        angular.forEach(value, function (nestedValue, nestedKey) {
                            if (!sensitive(nestedKey)) { result[nestedKey] = nonSensitiveValue(nestedValue); }
                        });
                        return result;
                    }
                    return value;
                }
                function displayValue(value) {
                    if (!present(value)) { return '—'; }
                    if (angular.isObject(value)) { return angular.toJson(nonSensitiveValue(value)); }
                    if (typeof value === 'boolean') { return value ? 'true' : 'false'; }
                    return String(value);
                }
                function configForResource() {
                    var entries = [];
                    if (kind === 'proxy') {
                        var model = findRawProxyModel();
                        angular.forEach(model, function (value, key) {
                            if (!sensitive(key)) { entries.push({key: key, value: displayValue(value)}); }
                        });
                    } else if (kind === 'group') {
                        entries = [
                            {key: 'id', value: displayValue(ctrl.resource.id)},
                            {key: 'name', value: displayValue(ctrl.resource.name)},
                            {key: 'out_of_sync', value: displayValue(ctrl.resource.out_of_sync)},
                            {key: 'promoting', value: displayValue(ctrl.resource.promoting || {})},
                            {key: 'server_count', value: displayValue(ctrl.resource.servers.length)},
                            {key: 'ha_warning', value: displayValue(ctrl.resource.haWarning)}
                        ];
                    } else {
                        entries = [
                            {key: 'server_addr', value: displayValue(ctrl.resource.server)},
                            {key: 'sentinel_masters', value: displayValue(ctrl.resource.masters)},
                            {key: 'masters_down', value: displayValue(ctrl.resource.mastersDown)},
                            {key: 'runidError', value: displayValue(ctrl.resource.runidError)},
                            {key: 'out_of_sync', value: displayValue(ctrl.cluster.sentinelOutOfSync)}
                        ];
                    }
                    return entries;
                }

                function updateFields() {
                    var resource = ctrl.resource;
                    if (!resource) {
                        ctrl.identityFields = [];
                        ctrl.basicFields = [];
                        ctrl.configEntries = [];
                        return;
                    }
                    if (kind === 'proxy') {
                        ctrl.identityFields = [
                            {key: 'token', label: 'Token', value: resource.token || '—'},
                            {key: 'id', label: 'ID', value: present(resource.id) ? resource.id : '—'},
                            {key: 'status', label: 'Status', value: resource.status || '—'}
                        ];
                        ctrl.basicFields = [
                            {key: 'proxy', label: 'proxy_addr', value: resource.proxy_addr || '—'},
                            {key: 'admin', label: 'admin_addr', value: resource.admin_addr || '—'},
                            {key: 'protocol', label: 'proto_type', value: resource.proto_type || '—'},
                            {key: 'host', label: 'hostname', value: resource.hostname || '—'},
                            {key: 'dc', label: 'datacenter', value: resource.datacenter || '—'},
                            {key: 'pid', label: 'pid', value: present(resource.pid) ? Format.number(resource.pid) : '—'},
                            {key: 'start', label: 'start_time', value: resource.start_time ? Format.date(resource.start_time, true) : '—'},
                            {key: 'product', label: 'product_name', value: resource.product_name || ctrl.product}
                        ];
                    } else if (kind === 'group') {
                        ctrl.identityFields = [
                            {key: 'gid', label: 'gid', value: resource.id},
                            {key: 'name', label: 'Name', value: resource.name},
                            {key: 'status', label: 'Status', value: resource.status}
                        ];
                        ctrl.basicFields = [
                            {key: 'master', label: 'Logical Master', value: resource.servers.length ? resource.servers[0].server : '—'},
                            {key: 'servers', label: 'Server 数', value: Format.number(resource.servers.length)},
                            {key: 'memory', label: 'Master Memory', value: present(resource.memory) ? Format.bytes(resource.memory) : '—'},
                            {key: 'keys', label: 'Master Keys', value: present(resource.keys) ? Format.number(resource.keys) : '—'},
                            {key: 'out', label: 'out_of_sync', value: resource.out_of_sync ? 'true' : 'false'},
                            {key: 'promoting', label: 'promoting', value: resource.isPromoting ? 'true (index=' + resource.promotingIndex + ')' : 'false'},
                            {key: 'ha', label: 'HA real master', value: realMaster(resource) || '—'},
                            {key: 'warning', label: 'haWarning', value: resource.haWarning || '—'}
                        ];
                    } else {
                        ctrl.identityFields = [
                            {key: 'server', label: 'server_addr', value: resource.server},
                            {key: 'product', label: 'Product', value: ctrl.product},
                            {key: 'status', label: 'Status', value: resource.status}
                        ];
                        ctrl.basicFields = [
                            {key: 'masters', label: 'masters', value: present(resource.masters) ? Format.number(resource.masters) : '—'},
                            {key: 'down', label: 'masters down', value: present(resource.mastersDown) ? Format.number(resource.mastersDown) : '—'},
                            {key: 'slaves', label: 'avg slaves', value: present(resource.averageSlaves) ? Format.number(resource.averageSlaves, 2) : '—'},
                            {key: 'sentinels', label: 'avg sentinels', value: present(resource.averageSentinels) ? Format.number(resource.averageSentinels, 2) : '—'},
                            {key: 'total-slaves', label: 'observed slaves', value: present(resource.slaves) ? Format.number(resource.slaves) : '—'},
                            {key: 'total-sentinels', label: 'observed sentinels', value: present(resource.sentinels) ? Format.number(resource.sentinels) : '—'},
                            {key: 'runid', label: 'runidError', value: resource.runidError || '—'},
                            {key: 'out', label: 'out_of_sync', value: ctrl.cluster.sentinelOutOfSync ? 'true' : 'false'}
                        ];
                    }
                    ctrl.configEntries = configForResource();
                }

                function updateMetrics() {
                    var resource = ctrl.resource;
                    if (!resource) { ctrl.metrics = []; return; }
                    if (kind === 'proxy') {
                        ctrl.metrics = [
                            {key: 'qps', title: 'QPS', value: resource.qps, unit: 'ops/s', caption: 'ops.qps', icon: 'activity', theme: 'primary'},
                            {key: 'sessions', title: '活跃连接', value: resource.sessions, caption: 'sessions.alive', icon: 'server'},
                            {key: 'commands', title: '总命令', value: resource.commands, caption: 'ops.total', icon: 'analysis'},
                            {key: 'fails', title: '失败命令', value: resource.fails, caption: 'ops.fails', icon: 'alert'}
                        ];
                    } else if (kind === 'group') {
                        ctrl.metrics = [
                            {key: 'memory', title: 'Master Memory', value: present(resource.memory) ? Format.bytes(resource.memory) : '—', caption: 'used_memory', icon: 'database', theme: 'primary'},
                            {key: 'keys', title: 'Master Keys', value: resource.keys, caption: '全部 DB keys', icon: 'grid'},
                            {key: 'servers', title: 'Server 数', value: resource.servers.length, caption: 'Logical Master + Replicas', icon: 'server'},
                            {key: 'sync', title: '拓扑状态', value: resource.out_of_sync ? 'OUT_OF_SYNC' : 'IN_SYNC', caption: 'Group out_of_sync', icon: 'refresh',
                                status: resource.out_of_sync ? 'OUT_OF_SYNC' : 'OK'}
                        ];
                    } else {
                        ctrl.metrics = [
                            {key: 'masters', title: 'Masters', value: resource.masters, caption: '当前 Sentinel 观测', icon: 'database', theme: 'primary'},
                            {key: 'down', title: 'Down Masters', value: resource.mastersDown, caption: 'status != ok', icon: 'alert'},
                            {key: 'slaves', title: 'Avg Slaves', value: resource.averageSlaves, caption: '每个 Master 平均副本数', icon: 'server'},
                            {key: 'sentinels', title: 'Avg Sentinels', value: resource.averageSentinels, caption: '每个 Master 平均 Sentinel 数', icon: 'shield'}
                        ];
                    }
                }

                function loadMonitorPreferences() {
                    monitorPreferenceKey = 'resource-detail.' + ctrl.product + '.' + kind + '.' + ctrl.resourceId + '.monitor';
                    var saved = Preferences.get(monitorPreferenceKey, {range: '1h', layout: 2, autoRefresh: false, metricFilter: {}}) || {};
                    ctrl.monitorRange = ['15m', '1h', 'all'].indexOf(saved.range) >= 0 ? saved.range : '1h';
                    ctrl.monitorLayout = Number(saved.layout) === 3 ? 3 : 2;
                    ctrl.autoRefresh = saved.autoRefresh === true;
                    ctrl.metricFilter = angular.copy(saved.metricFilter || {});
                    ctrl.availableMetrics.forEach(function (metric) {
                        if (typeof ctrl.metricFilter[metric.key] !== 'boolean') { ctrl.metricFilter[metric.key] = true; }
                    });
                }
                function saveMonitorPreferences() {
                    if (!monitorPreferenceKey) { return; }
                    Preferences.set(monitorPreferenceKey, {
                        range: ctrl.monitorRange, layout: ctrl.monitorLayout, autoRefresh: ctrl.autoRefresh,
                        metricFilter: angular.copy(ctrl.metricFilter)
                    });
                }
                function historyKey(metric) {
                    return kind === 'proxy' ? 'proxy:' + ctrl.resourceId : 'group:' + ctrl.resourceId;
                }
                function pointsFor(metric) {
                    if (kind === 'sentinel') { return []; }
                    var points = ClusterStore.history(ctrl.product, historyKey(metric), metric).slice();
                    if (ctrl.monitorRange === 'all') { return points; }
                    var duration = ctrl.monitorRange === '15m' ? 15 * 60 * 1000 : 60 * 60 * 1000;
                    var cutoff = Date.now() - duration;
                    return points.filter(function (point) {
                        var time = angular.isArray(point) ? point[0] : (point && point.x);
                        return present(time) && Number(time) >= cutoff;
                    });
                }
                function chartFor(metric) {
                    return {
                        key: metric.key, title: metric.title, unit: metric.unit, subtitle: metric.subtitle,
                        series: [{name: metric.title, data: pointsFor(metric.key)}]
                    };
                }
                function refreshCharts() {
                    ctrl.visibleCharts = ctrl.availableMetrics.filter(function (metric) {
                        return ctrl.metricFilter[metric.key] !== false;
                    }).map(chartFor);
                    var primary = ctrl.availableMetrics[0];
                    ctrl.overviewChart = primary ? chartFor(primary) : {key: '', title: '趋势', unit: '', series: []};
                    if (ctrl.expandedOpen && ctrl.expandedMetric) {
                        var selected = null;
                        ctrl.availableMetrics.forEach(function (metric) {
                            if (metric.key === ctrl.expandedMetric) { selected = metric; }
                        });
                        if (selected) { ctrl.expandedSeries = chartFor(selected).series; }
                    }
                }

                function loadActivities() {
                    ctrl.activities = ctrl.product ? Activity.list(ctrl.product, target()) : [];
                }
                function sync(state) {
                    if (destroyed || (state && state.product !== ctrl.product)) { return; }
                    ctrl.cluster = state || ClusterStore.state(ctrl.product);
                    var found = null;
                    (ctrl.cluster[config.collection] || []).forEach(function (resource) {
                        if (matches(resource)) { found = resource; }
                    });
                    ctrl.resource = found ? angular.extend({}, found) : null;
                    if (ctrl.resource && kind === 'proxy') {
                        ctrl.resource.proxyUrl = ResourceActions.safeHttpUrl(ctrl.resource.admin_addr, '/proxy');
                        ctrl.resource.statsUrl = ResourceActions.safeHttpUrl(ctrl.resource.admin_addr, '/proxy/stats');
                    } else if (ctrl.resource && kind === 'sentinel') {
                        ctrl.resource.infoUrl = ResourceActions.safeHttpUrl(ctrl.cluster.model.admin_addr,
                            '/api/topom/sentinels/info/' + Api.segment(ctrl.resource.server));
                        ctrl.resource.monitoredUrl = ResourceActions.safeHttpUrl(ctrl.cluster.model.admin_addr,
                            '/api/topom/sentinels/info/' + Api.segment(ctrl.resource.server) + '/monitored');
                    }
                    updateFields();
                    updateMetrics();
                    refreshCharts();
                    loadActivities();
                    updateTimer();
                }

                function load() {
                    var product = ctrl.product;
                    if (!product) { return; }
                    var sequence = ++contextSequence;
                    ClusterStore.load(product).then(function (state) {
                        if (sequence === contextSequence && ctrl.resourceContext(product)) { sync(state); }
                    }).catch(angular.noop);
                }
                function stopTimer() {
                    if (refreshTimer) {
                        $interval.cancel(refreshTimer);
                        refreshTimer = null;
                    }
                }
                function updateTimer() {
                    stopTimer();
                    if (!ctrl.autoRefresh || ctrl.activeTab !== 'monitor' || kind === 'sentinel' || !ctrl.resource || destroyed) { return; }
                    var product = ctrl.product;
                    var resourceId = ctrl.resourceId;
                    refreshTimer = $interval(function () {
                        if (ctrl.resourceContext(product) && ctrl.resourceId === resourceId && !ctrl.cluster.refreshing && !ctrl.submitting) {
                            ClusterStore.refresh(product).then(function (state) {
                                if (ctrl.resourceContext(product) && ctrl.resourceId === resourceId) { sync(state); }
                            }).catch(angular.noop);
                        }
                    }, 15000);
                }

                ctrl.initialLoading = function () { return !!ctrl.cluster.loading && !ctrl.cluster.updatedAt; };
                ctrl.initialError = function () { return !ctrl.cluster.updatedAt && !!ctrl.cluster.error; };
                ctrl.notFound = function () { return !!ctrl.cluster.updatedAt && !ctrl.resource; };
                ctrl.busy = function () { return ctrl.submitting || ctrl.confirming; };
                ctrl.detailSubtitle = function () {
                    if (!ctrl.resource) { return ''; }
                    return kind === 'proxy' ? 'Token: ' + ctrl.resource.token + ' · ' + (ctrl.resource.proxy_addr || '—') :
                        (kind === 'group' ? 'gid: ' + ctrl.resource.id + ' · ' + ctrl.resource.servers.length + ' Servers' : 'server_addr: ' + ctrl.resource.server);
                };
                ctrl.backToList = function () { Router.go(ctrl.product, config.listPage); };
                ctrl.refresh = function (manual) {
                    var product = ctrl.product;
                    var resourceId = ctrl.resourceId;
                    if (!product || ctrl.cluster.refreshing || ctrl.submitting) { return; }
                    return ClusterStore.refresh(product).then(function (state) {
                        if (ctrl.resourceContext(product) && ctrl.resourceId === resourceId) {
                            sync(state);
                            if (manual) { Toast.success('资源详情已刷新'); }
                        }
                    }).catch(function (response) {
                        if (manual && ctrl.resourceContext(product) && ctrl.resourceId === resourceId) { Toast.error(Api.errorMessage(response)); }
                    });
                };
                ctrl.setTab = function (tab) {
                    if (['overview', 'monitor', 'config', 'activity'].indexOf(tab) < 0) { return; }
                    ctrl.activeTab = tab;
                    if (tab === 'activity') { loadActivities(); }
                    if (tab === 'monitor') { refreshCharts(); }
                    updateTimer();
                };
                ctrl.setRange = function (range) {
                    if (['15m', '1h', 'all'].indexOf(range) < 0) { return; }
                    ctrl.monitorRange = range;
                    saveMonitorPreferences();
                    refreshCharts();
                };
                ctrl.setLayout = function (layout) {
                    ctrl.monitorLayout = Number(layout) === 3 ? 3 : 2;
                    saveMonitorPreferences();
                };
                ctrl.toggleAutoRefresh = function () { saveMonitorPreferences(); updateTimer(); };
                ctrl.metricsChanged = function () { saveMonitorPreferences(); refreshCharts(); };
                ctrl.rangeLabel = function () {
                    return ctrl.monitorRange === '15m' ? '最近 15 分钟' : (ctrl.monitorRange === '1h' ? '最近 1 小时' : '全部历史');
                };
                ctrl.expandChart = function (chart) {
                    if (!chart) { return; }
                    ctrl.expandedMetric = chart.key;
                    ctrl.expandedTitle = chart.title;
                    ctrl.expandedUnit = chart.unit;
                    ctrl.expandedSeries = chart.series;
                    ctrl.expandedOpen = true;
                };

                function realMaster(group) {
                    var address = '';
                    (group.servers || []).forEach(function (server) {
                        if (server.haStatus === 'real-master') { address = server.server; }
                    });
                    return address;
                }
                ctrl.haLabel = function (server) {
                    return {master: 'HA MASTER', 'real-master': 'REAL MASTER', 'not-master': 'NOT MASTER', replica: 'REPLICA', unknown: 'UNKNOWN'}[server.haStatus] || 'UNKNOWN';
                };
                ctrl.haTone = function (server) {
                    return server.haStatus === 'real-master' || server.haStatus === 'not-master' ? 'WARNING' :
                        (server.haStatus === 'master' || server.haStatus === 'replica' ? 'OK' : 'UNKNOWN');
                };
                ctrl.serverActionText = function (server) {
                    if (!server.actionState) { return '—'; }
                    return server.actionState + (server.actionState === 'pending' && present(server.action.index) ? ':' + server.action.index : '');
                };
                ctrl.removeLabel = function () {
                    if (kind === 'group') { return '删除空 Group'; }
                    return ctrl.resource && ctrl.resource.status === 'ERROR' ? '强制移除' : '移除';
                };
                function afterMutation() { loadActivities(); }

                ctrl.reinitProxy = function () {
                    if (!ctrl.resource || ctrl.busy()) { return; }
                    var conflicts = ResourceActions.haConflicts(ctrl.cluster);
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: 'Reinit and Start Proxy？',
                        message: conflicts.length ? 'HA real master 与 logical master 冲突。继续 Reinit 可能按错误拓扑恢复流量，请先修复冲突。' : '将重新初始化并启动该 Proxy，现有连接可能受到影响。',
                        details: conflicts.length ? conflicts : ctrl.resource.token,
                        confirmText: conflicts.length ? '了解风险，仍要 Reinit' : '确认 Reinit', danger: conflicts.length > 0
                    }, {
                        path: '/api/topom/proxy/reinit/{xauth}/' + Api.segment(ctrl.resource.token),
                        action: 'Reinit Proxy', target: target(), successMessage: 'Proxy Reinit 请求已完成', onSuccess: afterMutation
                    });
                };
                ctrl.resyncGroup = function () {
                    if (!ctrl.resource || ctrl.busy()) { return; }
                    var conflicts = ResourceActions.haConflicts(ctrl.cluster, [ctrl.resource]);
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: 'Resync Group-' + ctrl.resource.id + '？',
                        message: conflicts.length ? 'HA real master 与 logical master 冲突；继续可能按 logical 拓扑覆盖真实主从关系。' : '将按 Dashboard logical 拓扑重新同步该 Group。',
                        details: conflicts.length ? conflicts : (realMaster(ctrl.resource) || '当前无 HA real-master 冲突'),
                        confirmText: conflicts.length ? '了解风险，仍要 Resync' : '确认 Resync', danger: conflicts.length > 0
                    }, {
                        path: '/api/topom/group/resync/{xauth}/' + Api.segment(ctrl.resource.id),
                        action: 'Resync Group', target: target(), successMessage: 'Group 已重新同步', onSuccess: afterMutation
                    });
                };
                ctrl.resyncSentinels = function () {
                    if (ctrl.busy()) { return; }
                    var conflicts = ResourceActions.haConflicts(ctrl.cluster);
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: 'Resync All Sentinels？',
                        message: conflicts.length ? 'HA real master 与 logical master 冲突；继续可能将错误拓扑同步到全部 Sentinel。' : '将按 Dashboard 当前拓扑重新同步全部 Sentinel。',
                        details: conflicts.length ? conflicts : '全部 Sentinel',
                        confirmText: conflicts.length ? '了解风险，仍要全部同步' : '确认全部同步', danger: conflicts.length > 0
                    }, {
                        path: '/api/topom/sentinels/resync-all/{xauth}', action: 'Resync All Sentinels', target: target(),
                        successMessage: '全部 Sentinel 的 Resync 请求已完成', onSuccess: afterMutation
                    });
                };
                ctrl.removeResource = function () {
                    if (!ctrl.resource || ctrl.busy() || (kind === 'group' && !ctrl.resource.canRemove)) { return; }
                    var force = kind !== 'group' && ctrl.resource.status === 'ERROR';
                    var path = kind === 'proxy' ? '/api/topom/proxy/remove/{xauth}/' + Api.segment(ctrl.resource.token) + (force ? '/1' : '/0') :
                        (kind === 'group' ? '/api/topom/group/remove/{xauth}/' + Api.segment(ctrl.resource.id) :
                            '/api/topom/sentinels/del/{xauth}/' + Api.segment(ctrl.resource.server) + (force ? '/1' : '/0'));
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: (force ? '强制移除 ' : '移除 ') + target() + '？',
                        message: kind === 'proxy' ? '将移除并 Shutdown 此 Proxy，现有客户端连接会中断。' + (force ? ' ERROR 状态将使用 force=1。' : '') :
                            (kind === 'group' ? '仅可删除空 Group；此操作不可撤销。' : '将移除此 Sentinel，高可用监控覆盖可能下降。' + (force ? ' ERROR 状态将使用 force=1。' : '')),
                        confirmText: force ? '确认强制移除' : '确认移除', danger: true
                    }, {
                        path: path, action: force ? '强制移除资源' : '移除资源', target: target(), successMessage: target() + ' 已移除',
                        onSuccess: function () { afterMutation(); ctrl.backToList(); }
                    });
                };

                ctrl.openAddServer = function () {
                    if (ctrl.busy() || kind !== 'group') { return; }
                    ctrl.addServerForm = {gid: String(ctrl.resourceId), server_addr: '', datacenter: ''};
                    ctrl.fieldErrors = {};
                    ctrl.drawerError = '';
                    ctrl.addServerOpen = true;
                };
                function finishAddServer(owner) {
                    owner.addServerOpen = false;
                    owner.fieldErrors = {};
                    owner.drawerError = '';
                }
                ctrl.closeAddServer = function () { if (!ctrl.submitting) { finishAddServer(ctrl); } };
                ctrl.clearFieldError = function (field) { ctrl.fieldErrors[field] = ''; ctrl.drawerError = ''; };
                ctrl.submitAddServer = function () {
                    if (ctrl.submitting || kind !== 'group') { return; }
                    var gidText = String(ctrl.addServerForm.gid || '').trim();
                    var gid = /^\d+$/.test(gidText) ? Number(gidText) : null;
                    var address = String(ctrl.addServerForm.server_addr || '').trim();
                    var datacenter = String(ctrl.addServerForm.datacenter || '').trim();
                    ctrl.fieldErrors = {};
                    ctrl.drawerError = '';
                    if (gid === null || gid < 1 || gid > 9999 || Math.floor(gid) !== gid || String(gid) !== String(ctrl.resourceId)) {
                        ctrl.fieldErrors.gid = 'gid 必须与当前 Group 一致，且为 1 到 9999 的整数';
                    }
                    ctrl.fieldErrors.address = ResourceActions.hostPortError(address, 'server_addr');
                    if (/\//.test(datacenter)) { ctrl.fieldErrors.datacenter = 'datacenter 不能包含斜杠'; }
                    if (ctrl.fieldErrors.gid || ctrl.fieldErrors.address || ctrl.fieldErrors.datacenter) {
                        ctrl.drawerError = '请修正表单后重试'; return;
                    }
                    var path = '/api/topom/group/add/{xauth}/' + Api.segment(gid) + '/' + Api.segment(address) + (datacenter ? '/' + Api.segment(datacenter) : '');
                    ResourceActions.execute(ctrl, ctrl.product, {
                        path: path, action: '添加 Server ' + address, target: target(), successMessage: 'Server 已添加到 ' + target(),
                        errorField: 'drawerError', close: finishAddServer, onSuccess: afterMutation
                    });
                };
                ctrl.removeServer = function (server) {
                    if (!server || ctrl.busy() || !server.canRemove) { return; }
                    var conflict = server.haStatus === 'real-master';
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: 'Remove Server ' + server.server + '？',
                        message: conflict ? '该 Server 是 Sentinel 观测到的 HA real master，但不是 logical master。移除它会丢失真实主节点并可能造成服务中断。' : '将从 Group-' + ctrl.resource.id + ' 移除此 Server；请确认复制与流量已不再依赖它。',
                        details: conflict ? {gid: ctrl.resource.id, server: server.server, logical_master: ctrl.resource.servers[0].server, ha_real_master: server.server} : server.server,
                        confirmText: conflict ? '了解风险，仍要移除 Real Master' : '确认移除 Server', danger: true
                    }, {
                        path: '/api/topom/group/del/{xauth}/' + Api.segment(ctrl.resource.id) + '/' + Api.segment(server.server),
                        action: 'Remove Server ' + server.server, target: target(), successMessage: 'Server 已从 ' + target() + ' 移除', onSuccess: afterMutation
                    });
                };
                ctrl.promoteServer = function (server) {
                    if (!server || ctrl.busy() || !server.canPromote) { return; }
                    var currentRealMaster = realMaster(ctrl.resource);
                    var conflict = !!currentRealMaster && currentRealMaster !== server.server;
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: 'Promote Server ' + server.server + '？',
                        message: conflict ? '当前 HA real master 是 ' + currentRealMaster + '，与待 Promote 节点不同。继续会改变 logical master，可能与 Sentinel 拓扑冲突并影响流量。' : '将把此 Server Promote 为 Group-' + ctrl.resource.id + ' 的 logical master，请确认复制状态正常。',
                        details: conflict ? {gid: ctrl.resource.id, logical_master: ctrl.resource.servers[0].server, ha_real_master: currentRealMaster, promote: server.server} : server.server,
                        confirmText: conflict ? '了解冲突，仍要 Promote' : '确认 Promote', danger: conflict
                    }, {
                        path: '/api/topom/group/promote/{xauth}/' + Api.segment(ctrl.resource.id) + '/' + Api.segment(server.server),
                        action: 'Promote Server ' + server.server, target: target(), successMessage: 'Server Promote 请求已完成', onSuccess: afterMutation
                    });
                };
                ctrl.toggleReplica = function (server) {
                    if (!server || ctrl.busy()) { return; }
                    var current = (ctrl.resource.servers || []).filter(function (item) {
                        return item.server === server.server;
                    })[0] || server;
                    var value = current.replica_group ? 0 : 1;
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: (value ? '启用 Replica Group ' : '停用 Replica Group ') + current.server + '？',
                        message: '将修改此 Server 的 replica_group 标记，并影响 Group-' + ctrl.resource.id + ' 的副本拓扑行为。',
                        confirmText: value ? '确认启用' : '确认停用', danger: !value
                    }, {
                        path: '/api/topom/group/replica-groups/{xauth}/' + Api.segment(ctrl.resource.id) + '/' + Api.segment(current.server) + '/' + value,
                        action: (value ? '启用 Replica Group ' : '停用 Replica Group ') + current.server, target: target(),
                        successMessage: current.server + ' 的 replica_group 已更新', onSuccess: afterMutation
                    });
                };
                ctrl.replicaAll = function (value) {
                    if (ctrl.busy()) { return; }
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: value ? '启用全部 Replica Group？' : '停用全部 Replica Group？',
                        message: '将修改当前集群全部 Group 中所有 Server 的 replica_group 标记。',
                        confirmText: value ? '确认全部启用' : '确认全部停用', danger: !value
                    }, {
                        path: '/api/topom/group/replica-groups-all/{xauth}/' + value,
                        action: value ? '启用全部 Replica Group' : '停用全部 Replica Group', target: target(),
                        successMessage: '全部 Server 的 replica_group 已更新', onSuccess: afterMutation
                    });
                };
                ctrl.syncAction = function (server) {
                    if (!server || ctrl.busy()) { return; }
                    var remove = server.actionState === 'pending';
                    ResourceActions.confirmAndRun(ctrl, ctrl.product, {
                        title: remove ? 'Remove Sync Action？' : 'Create Sync Action？',
                        message: remove ? '将移除 ' + server.server + ' 当前 pending 的同步 Action。' : '将为 ' + server.server + ' 创建主从同步 Action，请确认目标拓扑正确。',
                        confirmText: remove ? '确认移除 Action' : '确认创建 Action', danger: remove
                    }, {
                        path: '/api/topom/group/action/' + (remove ? 'remove' : 'create') + '/{xauth}/' + Api.segment(server.server),
                        action: (remove ? 'Remove Sync Action ' : 'Create Sync Action ') + server.server, target: target(),
                        successMessage: remove ? 'Sync Action 已移除' : 'Sync Action 已创建', onSuccess: afterMutation
                    });
                };

                function activate() {
                    contextSequence++;
                    ctrl._mutationSequence = (ctrl._mutationSequence || 0) + 1;
                    stopTimer();
                    ctrl.submitting = false;
                    ctrl.confirming = false;
                    ctrl.operationError = '';
                    ctrl.activeTab = 'overview';
                    ctrl.addServerOpen = false;
                    ctrl.expandedOpen = false;
                    ctrl.cluster = ClusterStore.state(ctrl.product || 'default');
                    loadMonitorPreferences();
                    sync(ctrl.cluster);
                    load();
                }
                ctrl.$onInit = function () { activated = true; activate(); };
                ctrl.$onChanges = function (changes) {
                    if (!activated) { return; }
                    var productChanged = changes.product && !changes.product.isFirstChange() && changes.product.currentValue !== changes.product.previousValue;
                    var resourceChanged = changes.resourceId && !changes.resourceId.isFirstChange() && changes.resourceId.currentValue !== changes.resourceId.previousValue;
                    if (productChanged || resourceChanged) { activate(); }
                };
                ctrl.$onDestroy = function () {
                    destroyed = true;
                    contextSequence++;
                    ctrl._mutationSequence = (ctrl._mutationSequence || 0) + 1;
                    stopTimer();
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

    dashboard.component('proxyDetailPage', {
        bindings: {product: '<', resourceId: '<'},
        template: detailTemplate,
        controller: ['ResourceDetailFactory', '$scope', function (ResourceDetailFactory, $scope) {
            ResourceDetailFactory(this, 'proxy', $scope);
        }]
    });

    dashboard.component('groupDetailPage', {
        bindings: {product: '<', resourceId: '<'},
        template: detailTemplate,
        controller: ['ResourceDetailFactory', '$scope', function (ResourceDetailFactory, $scope) {
            ResourceDetailFactory(this, 'group', $scope);
        }]
    });

    dashboard.component('sentinelDetailPage', {
        bindings: {product: '<', resourceId: '<'},
        template: detailTemplate,
        controller: ['ResourceDetailFactory', '$scope', function (ResourceDetailFactory, $scope) {
            ResourceDetailFactory(this, 'sentinel', $scope);
        }]
    });
}());
