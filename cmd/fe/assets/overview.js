'use strict';

dashboard.component('overviewPage', {
    bindings: {
        product: '<'
    },
    template: `
        <div class="overview-page">
            <page-header title="{{$ctrl.product || '集群总览'}}" description="{{$ctrl.description}}">
                <div class="overview-auto-controls" ng-if="$ctrl.loaded">
                    <label class="overview-interval-control">
                        <span>自动刷新</span>
                        <select ng-model="$ctrl.refreshInterval" ng-change="$ctrl.changeRefreshInterval()"
                            ng-options="option.value as option.label for option in $ctrl.refreshOptions"
                            aria-label="自动刷新间隔"></select>
                    </label>
                    <button class="button button-secondary" type="button" ng-click="$ctrl.toggleAutoRefresh()"
                        ng-attr-aria-label="{{$ctrl.paused ? '继续自动刷新' : '暂停自动刷新'}}">
                        <ui-icon name="{{$ctrl.paused ? 'play' : 'pause'}}" size="15"></ui-icon>
                        {{$ctrl.paused ? '继续' : '暂停'}}
                    </button>
                </div>
                <button class="button button-primary" type="button" ng-click="$ctrl.manualRefresh()"
                    ng-disabled="!$ctrl.product || $ctrl.initialLoading || $ctrl.refreshing">
                    <ui-icon name="refresh" size="16" ng-class="{'is-spinning': $ctrl.refreshing}"></ui-icon>
                    {{$ctrl.refreshing ? '刷新中' : '手动刷新'}}
                </button>
            </page-header>

            <loading-skeleton ng-if="$ctrl.initialLoading" variant="page"></loading-skeleton>

            <empty-state ng-if="!$ctrl.initialLoading && !$ctrl.loaded && $ctrl.loadError"
                icon="alert" title="{{$ctrl.loadErrorTitle()}}" description="{{$ctrl.loadError}}"
                action-label="重试" on-action="$ctrl.retry()"></empty-state>

            <div class="overview-loaded" ng-if="$ctrl.loaded">
                <div class="inline-alert inline-alert--error overview-inline-alert" ng-if="$ctrl.refreshError" role="alert">
                    <ui-icon name="alert" size="18"></ui-icon>
                    <div>
                        <strong>刷新失败，已保留上次成功加载的数据</strong>
                        <p>{{$ctrl.refreshError}}</p>
                        <button class="button button-secondary" type="button" ng-click="$ctrl.manualRefresh()"
                            ng-disabled="$ctrl.refreshing">重试</button>
                    </div>
                </div>

                <div class="overview-refresh-feedback" ng-if="$ctrl.refreshFeedback" aria-live="polite">
                    <ui-icon name="{{$ctrl.refreshing ? 'refresh' : 'check'}}" size="14"
                        ng-class="{'is-spinning': $ctrl.refreshing}"></ui-icon>
                    <span>{{$ctrl.refreshFeedback}}</span>
                </div>

                <section class="overview-metrics" aria-label="集群核心指标">
                    <metric-card class="overview-primary-metric" metric="$ctrl.primaryMetric"></metric-card>
                    <div class="overview-secondary-metrics">
                        <metric-card ng-repeat="metric in $ctrl.secondaryMetrics track by metric.key" metric="metric"></metric-card>
                    </div>
                </section>

                <div class="overview-content-grid overview-trend-grid">
                    <chart-card title="集群 QPS 趋势" unit="次/秒" subtitle="最近采样记录，最多保留 180 个数据点"
                        on-expand="$ctrl.expandChart()">
                        <trend-chart series="$ctrl.qpsSeries" height="292" unit="次/秒"></trend-chart>
                    </chart-card>

                    <section class="panel overview-attention" ng-class="{'overview-attention--calm': !$ctrl.issues.length}"
                        aria-labelledby="overview-attention-title">
                        <header class="overview-attention__header">
                            <div>
                                <span class="overview-eyebrow">运行信号</span>
                                <h2 id="overview-attention-title">需要关注</h2>
                                <p>异常资源可直接进入详情排查</p>
                            </div>
                            <span class="overview-issue-count" ng-if="$ctrl.issues.length">
                                <strong>{{$ctrl.issueCount}}</strong><span>项</span>
                            </span>
                        </header>

                        <div class="overview-issue-list" ng-if="$ctrl.issues.length">
                            <button class="overview-issue" type="button" ng-repeat="issue in $ctrl.issues track by $index"
                                ng-click="$ctrl.openIssue(issue)" ng-attr-title="查看 {{issue.name}} 详情">
                                <span class="overview-issue__icon" aria-hidden="true">
                                    <ui-icon name="{{issue.icon}}" size="17"></ui-icon>
                                </span>
                                <span class="overview-issue__copy">
                                    <strong ng-attr-title="{{issue.name}}">{{issue.name}}</strong>
                                    <small>{{issue.typeLabel}} · {{issue.description}}</small>
                                </span>
                                <status-tag status="issue.status" label="{{issue.statusLabel}}"></status-tag>
                                <ui-icon name="chevron-right" size="15"></ui-icon>
                            </button>
                        </div>

                        <div class="overview-calm" ng-if="!$ctrl.issues.length">
                            <span class="overview-calm__icon" aria-hidden="true"><ui-icon name="check" size="25"></ui-icon></span>
                            <strong>运行平稳</strong>
                            <p>当前 Proxy、Group 与 Sentinel 均未发现需要处理的异常。</p>
                            <small ng-if="$ctrl.updatedTime">最近检查于 {{$ctrl.updatedTime}}</small>
                        </div>
                    </section>
                </div>

                <section class="panel overview-resources" aria-labelledby="overview-resources-title">
                    <div class="card-header">
                        <div>
                            <h2 class="card-title" id="overview-resources-title">资源健康</h2>
                            <p>按名称、状态与类型定位资源，点击卡片进入对应详情。</p>
                        </div>
                    </div>

                    <filter-bar filters="$ctrl.filters" status-options="$ctrl.statusOptions"
                        type-options="$ctrl.typeOptions" result-count="$ctrl.filteredResources.length"
                        refreshing="$ctrl.refreshing" search-placeholder="搜索资源名称、地址或机房"
                        on-change="$ctrl.filtersChanged(filters)" on-refresh="$ctrl.manualRefresh()"></filter-bar>

                    <div class="resource-grid overview-resource-grid" ng-if="$ctrl.filteredResources.length">
                        <button class="resource-card overview-resource-card" type="button"
                            ng-repeat="resource in $ctrl.filteredResources track by resource.key"
                            ng-click="$ctrl.openResource(resource)" ng-attr-title="查看 {{resource.name}} 详情">
                            <span class="overview-resource-card__header">
                                <span class="overview-resource-card__icon" ng-class="'overview-resource-card__icon--' + resource.type"
                                    aria-hidden="true"><ui-icon name="{{resource.icon}}" size="19"></ui-icon></span>
                                <span class="overview-resource-card__heading">
                                    <small>{{resource.typeLabel}}</small>
                                    <strong class="overview-resource-card__name" ng-attr-title="{{resource.name}}">{{resource.name}}</strong>
                                </span>
                                <status-tag status="resource.status" label="{{resource.statusLabel}}"></status-tag>
                            </span>
                            <span class="overview-resource-card__description" ng-attr-title="{{resource.description}}">{{resource.description}}</span>
                            <span class="overview-resource-card__metrics">
                                <span class="overview-resource-card__metric" ng-repeat="metric in resource.metrics track by $index">
                                    <small>{{metric.label}}</small>
                                    <span><strong>{{metric.value}}</strong><em ng-if="metric.unit">{{metric.unit}}</em></span>
                                </span>
                            </span>
                            <span class="overview-resource-card__footer">
                                <span>查看详情</span><ui-icon name="chevron-right" size="14"></ui-icon>
                            </span>
                        </button>
                    </div>

                    <empty-state ng-if="!$ctrl.resources.length" icon="database" title="尚未发现可监控资源"
                        description="当前集群还没有上报 Proxy、Group 或 Sentinel 资源。"></empty-state>
                    <empty-state ng-if="$ctrl.resources.length && !$ctrl.filteredResources.length"
                        icon="search" title="没有匹配的资源" description="请调整关键词、状态或类型筛选条件。"
                        action-label="清除筛选" on-action="$ctrl.clearFilters()"></empty-state>
                </section>
            </div>

            <resource-drawer open="$ctrl.chartExpanded" title="'集群 QPS 趋势'"
                subtitle="$ctrl.product + ' · 最近采样记录'" show-submit="false" width="900"
                on-close="$ctrl.closeChart()">
                <trend-chart series="$ctrl.qpsSeries" height="420" unit="次/秒"></trend-chart>
            </resource-drawer>

            <style>
                .overview-page .overview-auto-controls {
                    display: flex;
                    align-items: center;
                    gap: 8px;
                }
                .overview-page .overview-interval-control {
                    display: flex;
                    height: 34px;
                    align-items: center;
                    gap: 8px;
                    padding: 0 8px 0 11px;
                    border: 1px solid var(--border);
                    border-radius: 8px;
                    background: var(--surface);
                    color: var(--text-secondary);
                    font-size: 11px;
                }
                .overview-page .overview-interval-control select {
                    border: 0;
                    outline: 0;
                    background: transparent;
                    color: var(--text-primary);
                    font-size: 11px;
                    cursor: pointer;
                }
                .overview-page .overview-inline-alert {
                    margin-bottom: 12px;
                }
                .overview-page .overview-inline-alert .button {
                    margin-top: 8px;
                }
                .overview-page .overview-refresh-feedback {
                    display: flex;
                    min-height: 24px;
                    align-items: center;
                    justify-content: flex-end;
                    gap: 6px;
                    margin: -10px 0 10px;
                    color: var(--text-tertiary);
                    font-size: 11px;
                }
                .overview-page .overview-metrics {
                    display: grid;
                    grid-template-columns: minmax(280px, 1.05fr) minmax(0, 2fr);
                    gap: 16px;
                    margin-bottom: 20px;
                }
                .overview-page .overview-primary-metric,
                .overview-page .overview-secondary-metrics > metric-card {
                    min-width: 0;
                }
                .overview-page .overview-primary-metric .metric-card {
                    height: 100%;
                    min-height: 316px;
                    padding: 25px 26px;
                }
                .overview-page .overview-primary-metric .metric-card__value-row {
                    min-height: 82px;
                    align-items: center;
                    margin-top: 20px;
                }
                .overview-page .overview-primary-metric .metric-card__value {
                    font-size: clamp(40px, 4vw, 58px);
                    letter-spacing: -1.8px;
                }
                .overview-page .overview-primary-metric .metric-card__unit {
                    font-size: 13px;
                }
                .overview-page .overview-primary-metric .metric-card__footer {
                    margin-top: 18px;
                }
                .overview-page .overview-secondary-metrics {
                    display: grid;
                    min-width: 0;
                    grid-template-columns: repeat(2, minmax(0, 1fr));
                    gap: 16px;
                }
                .overview-page .overview-secondary-metrics .metric-card {
                    height: 100%;
                }
                .overview-page .overview-trend-grid {
                    align-items: stretch;
                    margin-bottom: 20px;
                }
                .overview-page .overview-trend-grid > chart-card,
                .overview-page .overview-trend-grid .chart-card {
                    height: 100%;
                }
                .overview-page .overview-attention {
                    display: flex;
                    min-width: 0;
                    min-height: 371px;
                    flex-direction: column;
                    padding: 19px 20px 16px;
                }
                .overview-page .overview-attention__header {
                    display: flex;
                    align-items: flex-start;
                    justify-content: space-between;
                    gap: 12px;
                    margin-bottom: 12px;
                }
                .overview-page .overview-eyebrow {
                    display: block;
                    margin-bottom: 3px;
                    color: var(--text-tertiary);
                    font-size: 9px;
                    font-weight: 600;
                    letter-spacing: 1.5px;
                }
                .overview-page .overview-attention h2 {
                    margin: 0;
                    color: var(--text-primary);
                    font-size: 14px;
                    font-weight: 600;
                    line-height: 22px;
                }
                .overview-page .overview-attention__header p {
                    margin: 3px 0 0;
                    color: var(--text-tertiary);
                    font-size: 10px;
                }
                .overview-page .overview-issue-count {
                    display: inline-flex;
                    align-items: baseline;
                    gap: 3px;
                    padding: 4px 8px;
                    border-radius: 6px;
                    background: var(--warning-soft);
                    color: var(--warning-text);
                    font-size: 12px;
                }
                .overview-page .overview-issue-count strong {
                    font-size: 15px;
                    font-variant-numeric: tabular-nums;
                }
                .overview-page .overview-issue-list {
                    max-height: 286px;
                    min-height: 0;
                    overflow: auto;
                    padding-right: 2px;
                }
                .overview-page .overview-issue {
                    display: flex;
                    width: 100%;
                    min-width: 0;
                    align-items: center;
                    gap: 9px;
                    padding: 12px 6px;
                    border-top: 1px solid var(--border);
                    background: transparent;
                    color: var(--text-tertiary);
                    text-align: left;
                }
                .overview-page .overview-issue:hover,
                .overview-page .overview-issue:focus-visible {
                    background: #FAFAFD;
                    color: var(--primary);
                }
                .overview-page .overview-issue__icon {
                    display: inline-flex;
                    width: 30px;
                    height: 30px;
                    flex: 0 0 30px;
                    align-items: center;
                    justify-content: center;
                    border-radius: 8px;
                    background: var(--warning-soft);
                    color: #A46C16;
                }
                .overview-page .overview-issue__copy {
                    display: block;
                    min-width: 0;
                    flex: 1;
                }
                .overview-page .overview-issue__copy strong,
                .overview-page .overview-issue__copy small {
                    display: block;
                    overflow: hidden;
                    text-overflow: ellipsis;
                    white-space: nowrap;
                }
                .overview-page .overview-issue__copy strong {
                    color: var(--text-primary);
                    font-size: 11px;
                    font-weight: 500;
                }
                .overview-page .overview-issue__copy small {
                    margin-top: 3px;
                    color: var(--text-tertiary);
                    font-size: 9px;
                }
                .overview-page .overview-calm {
                    display: flex;
                    min-height: 260px;
                    flex: 1;
                    align-items: center;
                    justify-content: center;
                    flex-direction: column;
                    padding: 24px;
                    border: 1px solid #E1F2ED;
                    border-radius: 10px;
                    background: radial-gradient(circle at 50% 20%, #F2FBF8 0, #FAFCFB 70%);
                    text-align: center;
                }
                .overview-page .overview-calm__icon {
                    display: inline-flex;
                    width: 50px;
                    height: 50px;
                    align-items: center;
                    justify-content: center;
                    margin-bottom: 13px;
                    border: 1px solid #CFEFE8;
                    border-radius: 50%;
                    background: #FFFFFF;
                    color: var(--success);
                    box-shadow: 0 6px 18px rgba(0, 192, 162, 0.09);
                }
                .overview-page .overview-calm > strong {
                    color: #167C6C;
                    font-size: 14px;
                    font-weight: 600;
                }
                .overview-page .overview-calm p {
                    max-width: 240px;
                    margin: 7px 0 12px;
                    color: var(--text-secondary);
                    font-size: 11px;
                    line-height: 18px;
                }
                .overview-page .overview-calm small {
                    color: var(--text-tertiary);
                    font-size: 10px;
                }
                .overview-page .overview-resources .card-header p {
                    margin: 4px 0 0;
                    color: var(--text-tertiary);
                    font-size: 11px;
                }
                .overview-page .overview-resource-grid {
                    margin-top: 18px;
                }
                .overview-page .overview-resource-card {
                    display: block;
                    width: 100%;
                    color: var(--text-primary);
                    font: inherit;
                    text-align: left;
                    cursor: pointer;
                }
                .overview-page .overview-resource-card:focus-visible {
                    border-color: rgba(86, 100, 255, 0.55);
                    outline: none;
                    box-shadow: var(--focus-ring);
                }
                .overview-page .overview-resource-card__header {
                    display: flex;
                    min-width: 0;
                    align-items: center;
                    gap: 10px;
                }
                .overview-page .overview-resource-card__icon {
                    display: inline-flex;
                    width: 36px;
                    height: 36px;
                    flex: 0 0 36px;
                    align-items: center;
                    justify-content: center;
                    border-radius: 9px;
                    background: #EEEFFF;
                    color: var(--primary);
                }
                .overview-page .overview-resource-card__icon--group {
                    background: #E9F8F5;
                    color: #008E79;
                }
                .overview-page .overview-resource-card__icon--sentinel {
                    background: #FFF4E4;
                    color: #A46C16;
                }
                .overview-page .overview-resource-card__heading {
                    display: block;
                    min-width: 0;
                    flex: 1;
                }
                .overview-page .overview-resource-card__heading small {
                    display: block;
                    margin-bottom: 2px;
                    color: var(--text-tertiary);
                    font-size: 9px;
                    letter-spacing: 0.4px;
                }
                .overview-page .overview-resource-card__name {
                    display: block;
                    overflow: hidden;
                    color: var(--text-primary);
                    font-size: 12px;
                    font-weight: 600;
                    text-overflow: ellipsis;
                    white-space: nowrap;
                }
                .overview-page .overview-resource-card__description {
                    display: -webkit-box;
                    min-height: 36px;
                    overflow: hidden;
                    margin-top: 14px;
                    color: var(--text-secondary);
                    font-size: 11px;
                    line-height: 18px;
                    -webkit-box-orient: vertical;
                    -webkit-line-clamp: 2;
                }
                .overview-page .overview-resource-card__metrics {
                    display: flex;
                    margin-top: 15px;
                    padding-top: 13px;
                    border-top: 1px solid var(--border);
                }
                .overview-page .overview-resource-card__metric {
                    display: block;
                    min-width: 0;
                    flex: 1;
                }
                .overview-page .overview-resource-card__metric + .overview-resource-card__metric {
                    padding-left: 14px;
                    border-left: 1px solid var(--border);
                }
                .overview-page .overview-resource-card__metric > small {
                    display: block;
                    margin-bottom: 4px;
                    color: var(--text-tertiary);
                    font-size: 9px;
                }
                .overview-page .overview-resource-card__metric > span {
                    display: flex;
                    min-width: 0;
                    align-items: baseline;
                    gap: 5px;
                }
                .overview-page .overview-resource-card__metric strong {
                    overflow: hidden;
                    color: var(--text-primary);
                    font-size: 17px;
                    font-weight: 600;
                    font-variant-numeric: tabular-nums;
                    text-overflow: ellipsis;
                    white-space: nowrap;
                }
                .overview-page .overview-resource-card__metric em {
                    flex: 0 0 auto;
                    color: var(--text-tertiary);
                    font-size: 9px;
                    font-style: normal;
                }
                .overview-page .overview-resource-card__footer {
                    display: flex;
                    align-items: center;
                    justify-content: flex-end;
                    gap: 2px;
                    margin-top: 12px;
                    color: var(--text-tertiary);
                    font-size: 10px;
                }
                .overview-page .overview-resource-card:hover .overview-resource-card__footer {
                    color: var(--primary);
                }
                .overview-page .overview-resources > empty-state {
                    min-height: 240px;
                }
                @media (max-width: 1180px) {
                    .overview-page .overview-metrics {
                        grid-template-columns: minmax(0, 1fr);
                    }
                    .overview-page .overview-primary-metric .metric-card {
                        min-height: 190px;
                    }
                    .overview-page .overview-primary-metric .metric-card__value-row {
                        min-height: 62px;
                        margin-top: 10px;
                    }
                }
                @media (max-width: 720px) {
                    .overview-page .overview-auto-controls {
                        width: 100%;
                        flex-wrap: wrap;
                    }
                    .overview-page .overview-secondary-metrics {
                        grid-template-columns: minmax(0, 1fr);
                    }
                    .overview-page .overview-attention {
                        min-height: 340px;
                    }
                    .overview-page .overview-issue .status-tag {
                        display: none;
                    }
                }
            </style>
        </div>
    `,
    controller: ['Api', 'ClusterStore', 'Format', 'Preferences', 'Router', '$scope', '$timeout',
        function (Api, ClusterStore, Format, Preferences, Router, $scope, $timeout) {
            var ctrl = this;
            var timer = null;
            var requestSequence = 0;
            var inFlight = false;
            var initialized = false;
            var destroyed = false;
            var listeners = [];
            var allowedIntervals = [5000, 10000, 30000, 60000];
            var statusValues = ['HEALTHY', 'WARNING', 'ERROR', 'TIMEOUT', 'PENDING'];
            var typeValues = ['proxy', 'group', 'sentinel'];

            ctrl.loaded = false;
            ctrl.initialLoading = true;
            ctrl.refreshing = false;
            ctrl.paused = false;
            ctrl.loadError = '';
            ctrl.refreshError = '';
            ctrl.refreshFeedback = '';
            ctrl.description = '正在读取 Dashboard 与 Coordinator 信息';
            ctrl.primaryMetric = {};
            ctrl.secondaryMetrics = [];
            ctrl.qpsSeries = [];
            ctrl.issues = [];
            ctrl.resources = [];
            ctrl.filteredResources = [];
            ctrl.filters = {search: '', status: '', type: ''};
            ctrl.refreshInterval = 5000;
            ctrl.chartExpanded = false;
            ctrl.statusOptions = [
                {value: 'HEALTHY', label: '健康'},
                {value: 'WARNING', label: '警告'},
                {value: 'ERROR', label: '异常'},
                {value: 'TIMEOUT', label: '超时'},
                {value: 'PENDING', label: '待处理'}
            ];
            ctrl.typeOptions = [
                {value: 'proxy', label: 'Proxy'},
                {value: 'group', label: 'Group'},
                {value: 'sentinel', label: 'Sentinel'}
            ];
            ctrl.refreshOptions = allowedIntervals.map(function (interval) {
                return {value: interval, label: Format.number(interval / 1000) + ' 秒'};
            });

            function hasNumber(value) {
                return value !== undefined && value !== null && value !== '' &&
                    !isNaN(Number(value)) && isFinite(Number(value));
            }

            function numberText(value, digits) {
                return hasNumber(value) ? Format.number(value, digits) : Format.placeholder;
            }

            function byteParts(value) {
                if (!hasNumber(value)) {
                    return {value: null, unit: ''};
                }
                var formatted = Format.bytes(value);
                var match = formatted.match(/^(-?[\d.]+)\s+([A-Z]+)$/);
                if (!match) {
                    return {value: null, unit: ''};
                }
                return {value: Number(match[1]), unit: match[2]};
            }

            function numberMetric(label, value, unit) {
                return {
                    label: label,
                    value: numberText(value),
                    unit: hasNumber(value) ? (unit || '') : ''
                };
            }

            function bytesMetric(label, value) {
                var parts = byteParts(value);
                return {
                    label: label,
                    value: parts.value === null ? Format.placeholder : Format.number(parts.value, parts.unit === 'B' ? 0 : 2),
                    unit: parts.unit
                };
            }

            function statusLabel(status) {
                var labels = {
                    HEALTHY: '健康',
                    WARNING: '警告',
                    ERROR: '异常',
                    TIMEOUT: '超时',
                    PENDING: '待处理'
                };
                return labels[String(status || '').toUpperCase()] || '未知';
            }

            function typeLabel(type) {
                return {proxy: 'Proxy', group: 'Group', sentinel: 'Sentinel'}[type] || '资源';
            }

            function typeIcon(type) {
                return {proxy: 'proxy', group: 'database', sentinel: 'shield'}[type] || 'server';
            }

            function preferenceKey(name) {
                return 'overview.' + ctrl.product + '.' + name;
            }

            function loadPreferences() {
                var savedFilters = Preferences.get(preferenceKey('filters'), {});
                savedFilters = angular.isObject(savedFilters) && !angular.isArray(savedFilters) ? savedFilters : {};
                ctrl.filters = {
                    search: typeof savedFilters.search === 'string' ? savedFilters.search : '',
                    status: statusValues.indexOf(savedFilters.status) >= 0 ? savedFilters.status : '',
                    type: typeValues.indexOf(savedFilters.type) >= 0 ? savedFilters.type : ''
                };
                var savedInterval = Number(Preferences.get(preferenceKey('refreshInterval'), 5000));
                ctrl.refreshInterval = allowedIntervals.indexOf(savedInterval) >= 0 ? savedInterval : 5000;
            }

            function saveFilters() {
                Preferences.set(preferenceKey('filters'), angular.copy(ctrl.filters));
            }

            function previousSampleTrend() {
                var points = ClusterStore.history(ctrl.product, 'cluster', 'qps');
                if (points.length < 2) {
                    return null;
                }
                var current = points[points.length - 1][1];
                var previous = points[points.length - 2][1];
                if (!hasNumber(current) || !hasNumber(previous)) {
                    return null;
                }
                var difference = Number(current) - Number(previous);
                if (difference === 0) {
                    return {label: '较上一采样 持平', direction: 'neutral'};
                }
                var sign = difference > 0 ? '+' : '-';
                if (Number(previous) === 0) {
                    return {
                        label: '较上一采样 ' + sign + Format.number(Math.abs(difference)),
                        direction: difference > 0 ? 'up' : 'down'
                    };
                }
                var change = Math.abs(difference / Number(previous)) * 100;
                return {
                    label: '较上一采样 ' + sign + Format.number(change, 1) + '%',
                    direction: difference > 0 ? 'up' : 'down'
                };
            }

            function updateMetrics() {
                var totals = ctrl.cluster.totals || {};
                var proxyCount = (ctrl.cluster.proxies || []).length;
                var memory = byteParts(totals.memory);
                var resourceCount = hasNumber(totals.resources) ? Number(totals.resources) : null;
                var issueCount = hasNumber(totals.issues) ? Number(totals.issues) : null;
                var healthCaption;
                if (resourceCount === null) {
                    healthCaption = '资源统计暂未上报';
                } else if (resourceCount === 0) {
                    healthCaption = '尚未发现可监控资源';
                } else if (issueCount > 0) {
                    healthCaption = '共 ' + numberText(resourceCount) + ' 项资源 · ' + numberText(issueCount) + ' 项异常';
                } else {
                    healthCaption = '共 ' + numberText(resourceCount) + ' 项资源 · 未发现异常';
                }
                ctrl.primaryMetric = {
                    key: 'qps',
                    title: '总 QPS',
                    value: hasNumber(totals.qps) ? Number(totals.qps) : null,
                    unit: '次/秒',
                    caption: hasNumber(totals.qps) ?
                        Format.number(proxyCount) + ' 个 Proxy 聚合 · 采样 ' + (ctrl.updatedTime || '等待中') :
                        '等待 Proxy 上报有效采样',
                    trend: previousSampleTrend(),
                    icon: 'activity',
                    theme: 'primary'
                };
                ctrl.secondaryMetrics = [
                    {
                        key: 'sessions',
                        title: '活跃 Session',
                        value: hasNumber(totals.sessions) ? Number(totals.sessions) : null,
                        unit: '个',
                        caption: '所有 Proxy 的当前活跃连接',
                        icon: 'link'
                    },
                    {
                        key: 'memory',
                        title: 'Master 内存',
                        value: memory.value,
                        unit: memory.unit,
                        caption: '仅汇总各 Group 的 Master 节点',
                        icon: 'database'
                    },
                    {
                        key: 'keys',
                        title: 'Keys',
                        value: hasNumber(totals.keys) ? Number(totals.keys) : null,
                        unit: '个',
                        caption: '汇总各 Group Master 的数据库键',
                        icon: 'grid'
                    },
                    {
                        key: 'health',
                        title: '资源健康',
                        value: hasNumber(totals.healthy) ? Number(totals.healthy) : null,
                        unit: '项健康',
                        caption: healthCaption,
                        icon: 'shield'
                    }
                ];
            }

            function buildResources() {
                var resources = [];
                (ctrl.cluster.proxies || []).forEach(function (proxy) {
                    var name = proxy.name || proxy.token || proxy.proxy_addr || '未命名 Proxy';
                    var description = proxy.proxy_addr ? '服务地址 ' + proxy.proxy_addr : '等待 Proxy 上报服务地址';
                    if (proxy.datacenter) {
                        description += ' · 机房 ' + proxy.datacenter;
                    }
                    resources.push({
                        key: 'proxy:' + (proxy.token || name),
                        type: 'proxy',
                        typeLabel: 'Proxy',
                        id: proxy.token,
                        name: name,
                        status: proxy.status || 'UNKNOWN',
                        statusLabel: statusLabel(proxy.status),
                        icon: 'proxy',
                        description: description,
                        metrics: [
                            numberMetric('QPS', proxy.qps, '次/秒'),
                            numberMetric('活跃 Session', proxy.sessions, '个')
                        ],
                        searchText: [name, proxy.token, proxy.proxy_addr, proxy.admin_addr, proxy.hostname,
                            proxy.datacenter, 'Proxy', statusLabel(proxy.status), description].join(' ').toLowerCase()
                    });
                });
                (ctrl.cluster.groups || []).forEach(function (group) {
                    var name = group.name || ('Group-' + group.id);
                    var description = 'Redis 主从拓扑与复制状态';
                    if (group.isPromoting) {
                        description = '正在切换 Master，请关注拓扑变化';
                    } else if (group.out_of_sync) {
                        description = '配置等待同步，请检查 Group 状态';
                    }
                    var addresses = (group.servers || []).map(function (server) { return server.server; });
                    resources.push({
                        key: 'group:' + group.id,
                        type: 'group',
                        typeLabel: 'Group',
                        id: group.id,
                        name: name,
                        status: group.status || 'UNKNOWN',
                        statusLabel: statusLabel(group.status),
                        icon: 'database',
                        description: description,
                        metrics: [
                            bytesMetric('Master 内存', group.memory),
                            numberMetric('Keys', group.keys, '个')
                        ],
                        searchText: [name, group.id, 'Group', statusLabel(group.status), description]
                            .concat(addresses).join(' ').toLowerCase()
                    });
                });
                (ctrl.cluster.sentinels || []).forEach(function (sentinel) {
                    var name = sentinel.name || sentinel.server || '未命名 Sentinel';
                    var description = sentinel.runidError || '监控 Master 状态与副本拓扑';
                    resources.push({
                        key: 'sentinel:' + (sentinel.server || name),
                        type: 'sentinel',
                        typeLabel: 'Sentinel',
                        id: sentinel.server || name,
                        name: name,
                        status: sentinel.status || 'UNKNOWN',
                        statusLabel: statusLabel(sentinel.status),
                        icon: 'shield',
                        description: description,
                        metrics: [
                            numberMetric('监控 Master', sentinel.masters, '个'),
                            numberMetric('异常 Master', sentinel.mastersDown, '个')
                        ],
                        searchText: [name, sentinel.server, 'Sentinel', statusLabel(sentinel.status), description]
                            .join(' ').toLowerCase()
                    });
                });
                ctrl.resources = resources;
                applyFilters();
            }

            function applyFilters() {
                var search = String(ctrl.filters.search || '').trim().toLowerCase();
                ctrl.filteredResources = ctrl.resources.filter(function (resource) {
                    return (!search || resource.searchText.indexOf(search) >= 0) &&
                        (!ctrl.filters.status || resource.status === ctrl.filters.status) &&
                        (!ctrl.filters.type || resource.type === ctrl.filters.type);
                });
            }

            ctrl.filtersChanged = function (filters) {
                filters = filters || {};
                ctrl.filters = {
                    search: typeof filters.search === 'string' ? filters.search : '',
                    status: statusValues.indexOf(filters.status) >= 0 ? filters.status : '',
                    type: typeValues.indexOf(filters.type) >= 0 ? filters.type : ''
                };
                applyFilters();
                saveFilters();
            };

            ctrl.clearFilters = function () {
                ctrl.filters = {search: '', status: '', type: ''};
                applyFilters();
                saveFilters();
            };

            function updateIssues() {
                ctrl.issues = (ctrl.cluster.issues || []).map(function (issue) {
                    return {
                        type: issue.type,
                        id: issue.id,
                        name: issue.name || issue.id || '未命名资源',
                        status: issue.status || 'WARNING',
                        statusLabel: statusLabel(issue.status || 'WARNING'),
                        typeLabel: typeLabel(issue.type),
                        icon: typeIcon(issue.type),
                        description: issue.description || '资源状态需要检查'
                    };
                });
                ctrl.issueCount = Format.number(ctrl.issues.length);
            }

            function updateDescription() {
                var model = ctrl.cluster.model || {};
                var config = ctrl.cluster.config || {};
                var dashboardAddress = model.admin_addr || '地址未上报';
                var coordinator = config.coordinator_name || '信息未上报';
                var coordinatorAddress = config.coordinator_addr ? '（' + config.coordinator_addr + '）' : '';
                ctrl.description = 'Dashboard ' + dashboardAddress + ' · Coordinator ' + coordinator + coordinatorAddress;
            }

            function updateView() {
                ctrl.loaded = !!ctrl.cluster.updatedAt;
                ctrl.updatedTime = ctrl.cluster.updatedAt ? Format.time(ctrl.cluster.updatedAt) : '';
                updateDescription();
                updateMetrics();
                ctrl.qpsSeries = [{
                    name: '集群 QPS',
                    data: angular.copy(ClusterStore.history(ctrl.product, 'cluster', 'qps'))
                }];
                updateIssues();
                buildResources();
            }

            function cancelTimer() {
                if (timer) {
                    $timeout.cancel(timer);
                    timer = null;
                }
            }

            function scheduleNext() {
                cancelTimer();
                if (destroyed || ctrl.paused || inFlight || !ctrl.loaded || !ctrl.product) {
                    return;
                }
                timer = $timeout(function () {
                    timer = null;
                    if (!destroyed && !ctrl.paused && ctrl.product) {
                        refresh({automatic: true});
                    }
                }, ctrl.refreshInterval);
            }

            function isCurrent(sequence, product) {
                return !destroyed && sequence === requestSequence && product === ctrl.product;
            }

            function loadInitial() {
                if (destroyed || inFlight || !ctrl.product) {
                    return;
                }
                cancelTimer();
                var product = ctrl.product;
                var sequence = ++requestSequence;
                inFlight = true;
                ctrl.loaded = !!ctrl.cluster.updatedAt;
                ctrl.initialLoading = !ctrl.loaded;
                ctrl.refreshing = false;
                ctrl.loadError = '';
                ctrl.refreshError = '';
                if (ctrl.initialLoading) {
                    ctrl.refreshFeedback = '';
                }
                return ClusterStore.load(product).then(function (state) {
                    if (!isCurrent(sequence, product)) {
                        return;
                    }
                    ctrl.cluster = state;
                    updateView();
                    ctrl.loadError = '';
                    ctrl.refreshFeedback = '数据加载完成 · ' + Format.time(state.updatedAt || new Date());
                }).catch(function (response) {
                    if (!isCurrent(sequence, product)) {
                        return;
                    }
                    ctrl.loadError = Api.errorMessage(response);
                    ctrl.refreshFeedback = '';
                }).finally(function () {
                    if (!isCurrent(sequence, product)) {
                        return;
                    }
                    inFlight = false;
                    ctrl.initialLoading = false;
                    ctrl.refreshing = false;
                    scheduleNext();
                });
            }

            function refresh(options) {
                options = options || {};
                if (destroyed || inFlight || !ctrl.product) {
                    return;
                }
                if (!ctrl.loaded) {
                    return loadInitial();
                }
                cancelTimer();
                var product = ctrl.product;
                var sequence = ++requestSequence;
                inFlight = true;
                ctrl.refreshing = true;
                ctrl.refreshError = '';
                ctrl.refreshFeedback = (options.manual ? '正在手动刷新' : '正在后台刷新') + '，当前数据保持可见';
                return ClusterStore.refresh(product).then(function (state) {
                    if (!isCurrent(sequence, product)) {
                        return;
                    }
                    ctrl.cluster = state;
                    updateView();
                    ctrl.refreshError = '';
                    ctrl.refreshFeedback = (options.manual ? '手动刷新完成' : '后台刷新完成') + ' · ' +
                        Format.time(state.updatedAt || new Date());
                }).catch(function (response) {
                    if (!isCurrent(sequence, product)) {
                        return;
                    }
                    ctrl.refreshError = Api.errorMessage(response);
                    ctrl.refreshFeedback = (options.manual ? '手动' : '后台') + '刷新失败，已保留原数据';
                }).finally(function () {
                    if (!isCurrent(sequence, product)) {
                        return;
                    }
                    inFlight = false;
                    ctrl.refreshing = false;
                    scheduleNext();
                });
            }

            ctrl.manualRefresh = function () {
                return refresh({manual: true});
            };

            ctrl.retry = function () {
                return loadInitial();
            };

            ctrl.toggleAutoRefresh = function () {
                ctrl.paused = !ctrl.paused;
                if (ctrl.paused) {
                    cancelTimer();
                    ctrl.refreshFeedback = '自动刷新已暂停，仍可手动刷新';
                } else {
                    ctrl.refreshFeedback = '自动刷新已继续 · 每 ' + Format.number(ctrl.refreshInterval / 1000) + ' 秒采样';
                    scheduleNext();
                }
            };

            ctrl.changeRefreshInterval = function () {
                var interval = Number(ctrl.refreshInterval);
                ctrl.refreshInterval = allowedIntervals.indexOf(interval) >= 0 ? interval : 5000;
                Preferences.set(preferenceKey('refreshInterval'), ctrl.refreshInterval);
                ctrl.refreshFeedback = '自动刷新间隔已设为 ' + Format.number(ctrl.refreshInterval / 1000) + ' 秒';
                scheduleNext();
            };

            ctrl.loadErrorTitle = function () {
                return ctrl.cluster && ctrl.cluster.forbidden ? '没有权限访问此集群' : '无法加载集群总览';
            };

            function goToDetail(type, id) {
                var pages = {proxy: 'proxy-detail', group: 'group-detail', sentinel: 'sentinel-detail'};
                if (!destroyed && ctrl.product && pages[type] && id !== undefined && id !== null && id !== '') {
                    Router.go(ctrl.product, pages[type], id);
                }
            }

            ctrl.openIssue = function (issue) {
                if (issue) {
                    goToDetail(issue.type, issue.id);
                }
            };

            ctrl.openResource = function (resource) {
                if (resource) {
                    goToDetail(resource.type, resource.id);
                }
            };

            ctrl.expandChart = function () {
                ctrl.chartExpanded = true;
            };

            ctrl.closeChart = function () {
                ctrl.chartExpanded = false;
            };

            function resetForProduct() {
                cancelTimer();
                requestSequence++;
                inFlight = false;
                ctrl.cluster = ClusterStore.state(ctrl.product || 'default');
                ctrl.loaded = false;
                ctrl.initialLoading = true;
                ctrl.refreshing = false;
                ctrl.paused = false;
                ctrl.loadError = '';
                ctrl.refreshError = '';
                ctrl.refreshFeedback = '';
                ctrl.chartExpanded = false;
                loadPreferences();
                updateView();
                ctrl.initialLoading = !ctrl.loaded;
            }

            ctrl.$onInit = function () {
                initialized = true;
                resetForProduct();
                listeners.push($scope.$on('cluster:updated', function (event, payload) {
                    if (destroyed || !payload || payload.product !== ctrl.product) {
                        return;
                    }
                    ctrl.cluster = payload.state;
                    ctrl.initialLoading = false;
                    ctrl.loadError = '';
                    ctrl.refreshError = '';
                    updateView();
                    if (!inFlight) {
                        ctrl.refreshFeedback = '数据已更新 · ' + (ctrl.updatedTime || Format.time(new Date()));
                    }
                }));
                listeners.push($scope.$on('cluster:refreshRequested', function (event, payload) {
                    if (!destroyed && payload && payload.product === ctrl.product) {
                        refresh({manual: true});
                    }
                }));
                loadInitial();
            };

            ctrl.$onChanges = function (changes) {
                if (initialized && changes.product && !changes.product.isFirstChange() &&
                    changes.product.currentValue !== changes.product.previousValue) {
                    resetForProduct();
                    loadInitial();
                }
            };

            ctrl.$onDestroy = function () {
                destroyed = true;
                initialized = false;
                requestSequence++;
                inFlight = false;
                cancelTimer();
                listeners.forEach(function (deregister) { deregister(); });
                listeners = [];
                ctrl.chartExpanded = false;
            };
        }
    ]
});
