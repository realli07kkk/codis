'use strict';

/*
 * Shared UI primitives for the Codis console. All dynamic copy is rendered by
 * AngularJS interpolation; only the closed, locally-defined SVG icon set is
 * marked as trusted markup.
 */
(function () {
    var iconId = 0;
    var chartColors = ['#5664FF', '#00C0A2', '#8B7CF6', '#D99A32', '#4A90E2', '#D66A8A'];
    var icons = {
        plus: '<path d="M12 5v14M5 12h14"/>',
        search: '<circle cx="11" cy="11" r="6.5"/><path d="m16 16 4 4"/>',
        filter: '<path d="M4 6h16M7 12h10M10 18h4"/>',
        refresh: '<path d="M20 7v5h-5"/><path d="M19 12a7 7 0 1 0-1.7 5"/>',
        more: '<circle cx="5" cy="12" r="1.2" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="1.2" fill="currentColor" stroke="none"/><circle cx="19" cy="12" r="1.2" fill="currentColor" stroke="none"/>',
        edit: '<path d="m14.5 5.5 4 4M4 20l4.2-1 11-11a2.8 2.8 0 0 0-4-4l-11 11L4 20Z"/>',
        trash: '<path d="M4 7h16M9 7V4h6v3M6.5 7l1 13h9l1-13M10 11v5M14 11v5"/>',
        eye: '<path d="M2.5 12s3.5-6 9.5-6 9.5 6 9.5 6-3.5 6-9.5 6-9.5-6-9.5-6Z"/><circle cx="12" cy="12" r="2.5"/>',
        close: '<path d="m6 6 12 12M18 6 6 18"/>',
        check: '<path d="m5 12.5 4.5 4.5L19 7.5"/>',
        alert: '<path d="M10.3 4.2 2.8 18a2 2 0 0 0 1.8 3h14.8a2 2 0 0 0 1.8-3L13.7 4.2a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4M12 17h.01"/>',
        info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/>',
        database: '<ellipse cx="12" cy="5.5" rx="8" ry="3.5"/><path d="M4 5.5v6c0 1.9 3.6 3.5 8 3.5s8-1.6 8-3.5v-6M4 11.5v6c0 1.9 3.6 3.5 8 3.5s8-1.6 8-3.5v-6"/>',
        server: '<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01M11 6.5h6M11 17.5h6"/>',
        proxy: '<rect x="8" y="3" width="8" height="6" rx="2"/><rect x="3" y="16" width="7" height="5" rx="1.5"/><rect x="14" y="16" width="7" height="5" rx="1.5"/><path d="M12 9v4M6.5 16v-3h11v3"/>',
        slots: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><path d="M14 17.5h7M17.5 14v7"/>',
        shield: '<path d="M12 3 20 6v5c0 5-3.2 8.2-8 10-4.8-1.8-8-5-8-10V6l8-3Z"/><path d="m8.5 12 2.2 2.2 4.8-5"/>',
        lock: '<rect x="4" y="10" width="16" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3M12 14v3"/>',
        analysis: '<path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/><path d="m3 7 6-4 6 7 6-5"/>',
        history: '<path d="M3 4v5h5"/><path d="M3.5 9A9 9 0 1 1 4 15M12 7v5l3 2"/>',
        grid: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
        activity: '<path d="M3 12h4l2.5-7 5 14 2.5-7h4"/>',
        help: '<circle cx="12" cy="12" r="9"/><path d="M9.6 9a2.5 2.5 0 1 1 3.7 2.2c-.9.5-1.3 1-1.3 2.3M12 17h.01"/>',
        panel: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16M6.5 8h.01M6.5 12h.01"/>',
        'chevron-down': '<path d="m6 9 6 6 6-6"/>',
        'chevron-left': '<path d="m15 18-6-6 6-6"/>',
        'chevron-right': '<path d="m9 18 6-6-6-6"/>',
        'chevrons-left': '<path d="m11 17-5-5 5-5M18 17l-5-5 5-5"/>',
        'chevrons-right': '<path d="m6 7 5 5-5 5M13 7l5 5-5 5"/>',
        expand: '<path d="M8 3H3v5M16 3h5v5M8 21H3v-5M16 21h5v-5"/><path d="m3 8 6-6M15 3l6 6M3 16l6 6M15 21l6-6"/>',
        upload: '<path d="M12 16V3M7 8l5-5 5 5M4 15v5h16v-5"/>',
        download: '<path d="M12 3v13M7 11l5 5 5-5M4 20h16"/>',
        play: '<path d="m8 4 12 8-12 8V4Z"/>',
        pause: '<path d="M8 5v14M16 5v14"/>',
        stop: '<rect x="5" y="5" width="14" height="14" rx="2"/>',
        save: '<path d="M4 3h13l3 3v15H4V3Z"/><path d="M8 3v6h8V3M8 21v-7h8v7"/>',
        settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3 1.7 1.7 0 0 0 1-1.6v-.2h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z"/>',
        file: '<path d="M6 3h8l4 4v14H6V3Z"/><path d="M14 3v5h4M9 12h6M9 16h6"/>',
        clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
        user: '<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>',
        link: '<path d="m10 14 4-4M8 16l-1 1a3.5 3.5 0 0 1-5-5l3-3a3.5 3.5 0 0 1 5 0M16 8l1-1a3.5 3.5 0 0 1 5 5l-3 3a3.5 3.5 0 0 1-5 0" transform="translate(-1)"/>',
        columns: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16M15 4v16"/>',
        fallback: '<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 12h8M12 8v8"/>'
    };

    function present(value) {
        return value !== undefined && value !== null && value !== '';
    }

    function numberText(value, maximumFractionDigits) {
        if (!present(value) || isNaN(Number(value))) {
            return '—';
        }
        return Number(value).toLocaleString('zh-CN', {
            minimumFractionDigits: 0,
            maximumFractionDigits: maximumFractionDigits === undefined ? 2 : maximumFractionDigits
        });
    }

    function focusable(container) {
        if (!container) {
            return [];
        }
        var nodes = container.querySelectorAll('a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])');
        return Array.prototype.filter.call(nodes, function (node) {
            return node.getAttribute('aria-hidden') !== 'true' &&
                (node.offsetWidth > 0 || node.offsetHeight > 0 || node.getClientRects().length > 0);
        });
    }

    function trapTab(event, container) {
        if (event.key !== 'Tab' && event.keyCode !== 9) {
            return;
        }
        var items = focusable(container);
        if (!items.length) {
            event.preventDefault();
            container.focus();
            return;
        }
        var first = items[0];
        var last = items[items.length - 1];
        var active = container.ownerDocument.activeElement;
        if (event.shiftKey && (active === first || !container.contains(active))) {
            event.preventDefault();
            last.focus();
        } else if (!event.shiftKey && (active === last || !container.contains(active))) {
            event.preventDefault();
            first.focus();
        }
    }

    dashboard.factory('UiOverlayStack', [function () {
        var stack = [];
        return {
            push: function (token) {
                this.remove(token);
                stack.push(token);
            },
            remove: function (token) {
                var index = stack.indexOf(token);
                if (index >= 0) {
                    stack.splice(index, 1);
                }
            },
            isTop: function (token) {
                return stack.length > 0 && stack[stack.length - 1] === token;
            }
        };
    }]);

    dashboard.component('uiIcon', {
        bindings: {
            name: '@?',
            size: '@?',
            label: '@?'
        },
        template: `
            <svg class="ui-icon-svg" ng-attr-width="{{$ctrl.pixelSize}}" ng-attr-height="{{$ctrl.pixelSize}}"
                viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"
                stroke-linecap="round" stroke-linejoin="round" focusable="false"
                ng-attr-role="{{$ctrl.label ? 'img' : undefined}}"
                ng-attr-aria-label="{{$ctrl.label || undefined}}"
                ng-attr-aria-hidden="{{$ctrl.label ? undefined : 'true'}}" ng-bind-html="$ctrl.markup"></svg>
        `,
        controller: ['$sce', function ($sce) {
            var ctrl = this;
            ctrl.$onChanges = function () {
                var name = String(ctrl.name || '').toLowerCase();
                ctrl.markup = $sce.trustAsHtml(icons[name] || icons.fallback);
                var size = Number(ctrl.size);
                ctrl.pixelSize = isFinite(size) && size > 0 ? Math.min(size, 128) : 18;
            };
        }]
    });

    dashboard.component('pageHeader', {
        bindings: {
            title: '@?',
            description: '@?'
        },
        transclude: true,
        template: `
            <div class="page-header-row">
                <div class="page-header-copy">
                    <h1>{{$ctrl.title}}</h1>
                    <p ng-if="$ctrl.description">{{$ctrl.description}}</p>
                </div>
                <div class="page-header-actions" ng-transclude></div>
            </div>
        `
    });

    dashboard.component('metricCard', {
        bindings: {
            metric: '<'
        },
        template: `
            <article class="metric-card" ng-class="{'metric-card--primary': $ctrl.metric.theme === 'primary'}">
                <div class="metric-card__geometry" aria-hidden="true"></div>
                <div class="metric-card__topline">
                    <span class="metric-card__title">{{$ctrl.metric.title || '未命名指标'}}</span>
                    <span class="metric-card__icon" ng-if="$ctrl.metric.icon">
                        <ui-icon name="{{$ctrl.metric.icon}}" size="19"></ui-icon>
                    </span>
                </div>
                <div class="metric-card__value-row">
                    <strong class="metric-card__value">{{$ctrl.displayValue()}}</strong>
                    <span class="metric-card__unit" ng-if="$ctrl.metric.unit && $ctrl.hasValue()">{{$ctrl.metric.unit}}</span>
                </div>
                <div class="metric-card__footer">
                    <span class="metric-card__caption">{{$ctrl.metric.caption}}</span>
                    <span class="metric-card__trend" ng-if="$ctrl.trendText()" ng-class="$ctrl.trendClass()">
                        <span aria-hidden="true">{{$ctrl.trendArrow()}}</span>{{$ctrl.trendText()}}
                    </span>
                    <status-tag ng-if="$ctrl.metric.status" status="$ctrl.metric.status"></status-tag>
                </div>
            </article>
        `,
        controller: [function () {
            var ctrl = this;
            ctrl.hasValue = function () {
                return ctrl.metric && present(ctrl.metric.value) && !isNaN(Number(ctrl.metric.value));
            };
            ctrl.displayValue = function () {
                if (!ctrl.metric || !present(ctrl.metric.value)) {
                    return '—';
                }
                if (!isNaN(Number(ctrl.metric.value))) {
                    return numberText(ctrl.metric.value);
                }
                return String(ctrl.metric.value);
            };
            ctrl.trendText = function () {
                if (!ctrl.metric || !present(ctrl.metric.trend)) {
                    return '';
                }
                if (angular.isObject(ctrl.metric.trend)) {
                    return ctrl.metric.trend.label || (present(ctrl.metric.trend.value) ? String(ctrl.metric.trend.value) : '');
                }
                return String(ctrl.metric.trend);
            };
            ctrl.trendDirection = function () {
                if (!ctrl.metric) {
                    return 'neutral';
                }
                var trend = ctrl.metric.trend;
                var direction = angular.isObject(trend) ? trend.direction : '';
                if (direction) {
                    return direction;
                }
                var text = ctrl.trendText();
                return /^[-−▼]/.test(text) ? 'down' : (/^[+＋▲]/.test(text) ? 'up' : 'neutral');
            };
            ctrl.trendClass = function () {
                return 'metric-card__trend--' + ctrl.trendDirection();
            };
            ctrl.trendArrow = function () {
                var direction = ctrl.trendDirection();
                return direction === 'up' ? '↗ ' : (direction === 'down' ? '↘ ' : '');
            };
        }]
    });

    dashboard.component('statusTag', {
        bindings: {
            status: '<',
            value: '<',
            label: '@?'
        },
        template: `
            <span class="status-tag" ng-class="'status-tag--' + $ctrl.tone" ng-attr-aria-label="状态：{{$ctrl.displayLabel}}">
                <span class="status-tag__dot" aria-hidden="true"></span>
                <ui-icon name="{{$ctrl.iconName}}" size="13"></ui-icon>
                <span>{{$ctrl.displayLabel}}</span>
            </span>
        `,
        controller: [function () {
            var ctrl = this;
            ctrl.$onChanges = update;
            ctrl.$onInit = update;
            function update() {
                var value = present(ctrl.status) ? ctrl.status : ctrl.value;
                var normalized = present(value) ? String(value).trim().toUpperCase().replace(/[\s-]+/g, '_') : 'UNKNOWN';
                var success = ['HEALTHY', 'ONLINE', 'SUCCEEDED', 'SUCCESS', 'DONE', 'ENABLED', 'OK', 'ACTIVE', 'NORMAL', 'COMPLETED', 'UP', 'MASTER'];
                var warning = ['WARNING', 'WARN', 'TIMEOUT', 'PENDING', 'RUNNING', 'QUEUED', 'PROCESSING', 'SYNCING', 'IN_PROGRESS', 'DEGRADED'];
                var error = ['ERROR', 'FAILED', 'FAILURE', 'CANCELED', 'CANCELLED', 'OFFLINE', 'DOWN', 'CRITICAL', 'OUT_OF_SYNC'];
                if (success.indexOf(normalized) >= 0) {
                    ctrl.tone = 'success';
                    ctrl.iconName = 'check';
                } else if (warning.indexOf(normalized) >= 0) {
                    ctrl.tone = 'warning';
                    ctrl.iconName = 'clock';
                } else if (error.indexOf(normalized) >= 0) {
                    ctrl.tone = 'error';
                    ctrl.iconName = 'alert';
                } else {
                    ctrl.tone = 'neutral';
                    ctrl.iconName = 'info';
                }
                ctrl.displayLabel = ctrl.label || (present(value) ? String(value) : '未知');
            }
        }]
    });

    dashboard.component('filterBar', {
        bindings: {
            filters: '=',
            statusOptions: '<',
            typeOptions: '<',
            resultCount: '<',
            refreshing: '<',
            searchPlaceholder: '@?',
            onChange: '&',
            onRefresh: '&'
        },
        template: `
            <div class="filter-bar">
                <label class="filter-search">
                    <span class="sr-only">搜索</span>
                    <ui-icon name="search" size="17"></ui-icon>
                    <input type="search" ng-model="$ctrl.filters.search" ng-change="$ctrl.changed()"
                        placeholder="{{$ctrl.searchPlaceholder || '搜索名称、地址或关键字'}}" autocomplete="off">
                    <button type="button" class="filter-search__clear" ng-if="$ctrl.filters.search"
                        ng-click="$ctrl.clearSearch()" aria-label="清空搜索"><ui-icon name="close" size="14"></ui-icon></button>
                </label>
                <label class="filter-select">
                    <span class="sr-only">状态筛选</span>
                    <ui-icon class="filter-select__leading" name="filter" size="15"></ui-icon>
                    <select ng-model="$ctrl.filters.status" ng-change="$ctrl.changed()"
                        ng-options="option.value as option.label for option in $ctrl.statusChoices" aria-label="状态筛选"></select>
                    <ui-icon class="filter-select__trailing" name="chevron-down" size="13"></ui-icon>
                </label>
                <label class="filter-select">
                    <span class="sr-only">类型筛选</span>
                    <select ng-model="$ctrl.filters.type" ng-change="$ctrl.changed()"
                        ng-options="option.value as option.label for option in $ctrl.typeChoices" aria-label="类型筛选"></select>
                    <ui-icon class="filter-select__trailing" name="chevron-down" size="13"></ui-icon>
                </label>
                <span class="filter-result-count" aria-live="polite">共 <strong>{{$ctrl.countLabel()}}</strong> 条结果</span>
                <button type="button" class="button button-secondary filter-refresh" ng-click="$ctrl.refresh()" ng-disabled="$ctrl.refreshing">
                    <ui-icon name="refresh" size="16" ng-class="{'is-spinning': $ctrl.refreshing}"></ui-icon>
                    <span>{{$ctrl.refreshing ? '刷新中' : '刷新'}}</span>
                </button>
            </div>
        `,
        controller: [function () {
            var ctrl = this;
            ctrl.statusChoices = [{value: '', label: '全部状态'}];
            ctrl.typeChoices = [{value: '', label: '全部类型'}];
            function updateChoices() {
                ctrl.statusChoices = [{value: '', label: '全部状态'}].concat(angular.isArray(ctrl.statusOptions) ? ctrl.statusOptions : []);
                ctrl.typeChoices = [{value: '', label: '全部类型'}].concat(angular.isArray(ctrl.typeOptions) ? ctrl.typeOptions : []);
            }
            ctrl.$onInit = function () {
                updateChoices();
                if (!ctrl.filters) {
                    ctrl.filters = {search: '', status: '', type: ''};
                }
            };
            ctrl.$onChanges = updateChoices;
            ctrl.changed = function () {
                ctrl.onChange({filters: angular.copy(ctrl.filters)});
            };
            ctrl.clearSearch = function () {
                ctrl.filters.search = '';
                ctrl.changed();
            };
            ctrl.refresh = function () {
                if (!ctrl.refreshing) {
                    ctrl.onRefresh({filters: angular.copy(ctrl.filters)});
                }
            };
            ctrl.countLabel = function () {
                return present(ctrl.resultCount) ? numberText(ctrl.resultCount, 0) : '—';
            };
        }]
    });

    dashboard.component('codisDataTable', {
        bindings: {
            rows: '<',
            columns: '<',
            loading: '<',
            error: '<',
            selected: '=',
            page: '=',
            pageSize: '<',
            sortKey: '=',
            sortDirection: '=',
            selectable: '<',
            rowKey: '@?',
            emptyTitle: '@?',
            emptyDescription: '@?',
            onRetry: '&',
            onOpen: '&',
            onRowAction: '&'
        },
        template: `
            <div class="data-table-shell">
                <div class="table-selection-strip" ng-if="$ctrl.selectable && $ctrl.selected.length">
                    <ui-icon name="check" size="15"></ui-icon>
                    <span>已选择 <strong>{{$ctrl.selected.length}}</strong> 项</span>
                    <button type="button" class="text-button" ng-click="$ctrl.clearSelection()">清除选择</button>
                </div>
                <div class="data-table-state" ng-if="$ctrl.loading">
                    <loading-skeleton variant="table"></loading-skeleton>
                </div>
                <div class="data-table-state" ng-if="!$ctrl.loading && $ctrl.error">
                    <empty-state icon="alert" title="数据加载失败" description="{{$ctrl.error}}" action-label="重新加载" on-action="$ctrl.retry()"></empty-state>
                </div>
                <div class="data-table-state" ng-if="!$ctrl.loading && !$ctrl.error && !$ctrl.totalItems">
                    <empty-state icon="database" title="{{$ctrl.emptyTitle || '暂无数据'}}"
                        description="{{$ctrl.emptyDescription || '当前没有记录，或没有符合筛选条件的结果。'}}"></empty-state>
                </div>
                <div class="data-table-scroll" ng-if="!$ctrl.loading && !$ctrl.error && $ctrl.totalItems">
                    <table class="data-table">
                        <caption class="sr-only">数据列表，共 {{$ctrl.totalItems}} 条记录</caption>
                        <thead>
                            <tr>
                                <th class="table-select-cell" ng-if="$ctrl.selectable">
                                    <input type="checkbox" class="data-table-select-all" ng-checked="$ctrl.allPageSelected()"
                                        ng-click="$ctrl.togglePage()" ng-attr-aria-label="选择当前页全部记录"
                                        ng-attr-aria-checked="{{$ctrl.allPageSelected() ? 'true' : ($ctrl.somePageSelected() ? 'mixed' : 'false')}}">
                                </th>
                                <th ng-repeat="column in $ctrl.columns track by $index" ng-style="$ctrl.columnStyle(column)"
                                    ng-class="{'is-numeric': $ctrl.isNumeric(column), 'is-actions': column.type === 'actions'}" ng-attr-aria-sort="{{$ctrl.sortAria(column)}}">
                                    <button type="button" class="table-sort-button" ng-if="column.sortable" ng-click="$ctrl.sort(column)">
                                        <span>{{column.label}}</span>
                                        <ui-icon name="chevron-down" size="12" ng-class="{'is-sorted': $ctrl.sortKey === column.key, 'is-ascending': $ctrl.sortKey === column.key && $ctrl.sortDirection === 'asc'}"></ui-icon>
                                    </button>
                                    <span ng-if="!column.sortable">{{column.label}}</span>
                                </th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr ng-repeat="row in $ctrl.viewRows track by $index" ng-class="{'is-selected': $ctrl.isSelected(row)}">
                                <td class="table-select-cell" ng-if="$ctrl.selectable">
                                    <input type="checkbox" ng-checked="$ctrl.isSelected(row)" ng-click="$ctrl.toggleRow(row)"
                                        ng-attr-aria-label="选择记录 {{$ctrl.keyFor(row)}}">
                                </td>
                                <td ng-repeat="column in $ctrl.columns track by $index"
                                    ng-class="{'is-numeric': $ctrl.isNumeric(column), 'is-actions': column.type === 'actions'}">
                                    <div class="table-cell" ng-switch="column.type || 'text'">
                                        <span ng-switch-when="number" class="table-number">{{$ctrl.formatValue(row, column)}}</span>
                                        <span ng-switch-when="bytes" class="table-number">{{$ctrl.formatValue(row, column)}}</span>
                                        <span ng-switch-when="percent" class="table-number">{{$ctrl.formatValue(row, column)}}</span>
                                        <span ng-switch-when="date" class="table-date" ng-attr-title="{{$ctrl.formatValue(row, column)}}">{{$ctrl.formatValue(row, column)}}</span>
                                        <status-tag ng-switch-when="status" status="$ctrl.cellValue(row, column)"></status-tag>
                                        <sparkline ng-switch-when="sparkline" data="$ctrl.trendValue(row, column)"></sparkline>
                                        <button type="button" ng-switch-when="link" class="table-link" ng-click="$ctrl.open(row, column)"
                                            ng-attr-title="{{$ctrl.textValue(row, column)}}">
                                            <span>{{$ctrl.textValue(row, column)}}</span>
                                        </button>
                                        <div ng-switch-when="actions" class="table-actions">
                                            <button type="button" ng-repeat="action in $ctrl.actionsFor(row, column) track by action.name"
                                                class="table-action" ng-class="{'table-action--danger': action.danger}"
                                                ng-disabled="$ctrl.actionDisabled(action, row)" ng-click="$ctrl.rowAction(action, row)"
                                                ng-attr-title="{{action.title || action.label}}" ng-attr-aria-label="{{action.label}}">
                                                <ui-icon ng-if="action.icon" name="{{action.icon}}" size="15"></ui-icon>
                                                <span ng-if="action.label">{{action.label}}</span>
                                            </button>
                                        </div>
                                        <span ng-switch-default class="table-text" ng-attr-title="{{$ctrl.textValue(row, column)}}">{{$ctrl.textValue(row, column)}}</span>
                                    </div>
                                </td>
                            </tr>
                        </tbody>
                    </table>
                </div>
                <div class="table-pagination" ng-if="!$ctrl.loading && !$ctrl.error && $ctrl.totalItems">
                    <span class="table-pagination__summary">{{$ctrl.pageStart}}–{{$ctrl.pageEnd}} / 共 {{$ctrl.totalItems}} 条</span>
                    <nav class="table-pagination__pages" aria-label="表格分页">
                        <button type="button" class="page-button" ng-disabled="$ctrl.page <= 1" ng-click="$ctrl.goToPage($ctrl.page - 1)" aria-label="上一页">
                            <ui-icon name="chevron-left" size="15"></ui-icon>
                        </button>
                        <span ng-repeat="item in $ctrl.paginationItems track by $index" class="page-item">
                            <button ng-if="!item.ellipsis" type="button" class="page-button" ng-class="{'is-current': item.value === $ctrl.page}"
                                ng-click="$ctrl.goToPage(item.value)" ng-attr-aria-current="{{item.value === $ctrl.page ? 'page' : undefined}}"
                                ng-attr-aria-label="第 {{item.value}} 页">{{item.value}}</button>
                            <span ng-if="item.ellipsis" class="page-ellipsis" aria-hidden="true">…</span>
                        </span>
                        <button type="button" class="page-button" ng-disabled="$ctrl.page >= $ctrl.totalPages" ng-click="$ctrl.goToPage($ctrl.page + 1)" aria-label="下一页">
                            <ui-icon name="chevron-right" size="15"></ui-icon>
                        </button>
                    </nav>
                </div>
            </div>
        `,
        controller: ['Format', '$element', function (Format, $element) {
            var ctrl = this;
            var actionCache = [];
            ctrl.rows = [];
            ctrl.columns = [];
            ctrl.viewRows = [];
            ctrl.paginationItems = [];
            ctrl.totalItems = 0;

            ctrl.$onInit = function () {
                setDefaults();
                refresh();
            };
            ctrl.$onChanges = function () {
                setDefaults();
                refresh();
            };
            ctrl.$doCheck = function () {
                var checkbox = $element[0].querySelector('.data-table-select-all');
                if (checkbox) {
                    checkbox.indeterminate = ctrl.somePageSelected() && !ctrl.allPageSelected();
                }
            };

            function setDefaults() {
                if (!angular.isArray(ctrl.rows)) {
                    ctrl.rows = [];
                }
                if (!angular.isArray(ctrl.columns)) {
                    ctrl.columns = [];
                }
                if (!angular.isArray(ctrl.selected)) {
                    ctrl.selected = [];
                }
                var size = Number(ctrl.pageSize);
                ctrl.pageSize = isFinite(size) && size > 0 ? Math.floor(size) : 10;
                var page = Number(ctrl.page);
                ctrl.page = isFinite(page) && page > 0 ? Math.floor(page) : 1;
                ctrl.sortDirection = ctrl.sortDirection === 'desc' ? 'desc' : 'asc';
                ctrl.keyName = ctrl.rowKey || 'id';
            }

            function compareValues(left, right, column) {
                var leftMissing = !present(left);
                var rightMissing = !present(right);
                if (leftMissing || rightMissing) {
                    return leftMissing === rightMissing ? 0 : (leftMissing ? 1 : -1);
                }
                if (ctrl.isNumeric(column) || column.type === 'date') {
                    var leftNumber = column.type === 'date' ? new Date(left).getTime() : Number(left);
                    var rightNumber = column.type === 'date' ? new Date(right).getTime() : Number(right);
                    if (isNaN(leftNumber) || isNaN(rightNumber)) {
                        return String(left).localeCompare(String(right), 'zh-CN', {numeric: true});
                    }
                    return leftNumber === rightNumber ? 0 : (leftNumber < rightNumber ? -1 : 1);
                }
                return String(left).localeCompare(String(right), 'zh-CN', {numeric: true, sensitivity: 'base'});
            }

            function refresh() {
                var entries = ctrl.rows.map(function (row, index) {
                    return {row: row, index: index};
                });
                var sortColumn = null;
                ctrl.columns.forEach(function (column) {
                    if (column.key === ctrl.sortKey && column.sortable) {
                        sortColumn = column;
                    }
                });
                if (sortColumn) {
                    var direction = ctrl.sortDirection === 'desc' ? -1 : 1;
                    entries.sort(function (left, right) {
                        var leftValue = ctrl.cellValue(left.row, sortColumn);
                        var rightValue = ctrl.cellValue(right.row, sortColumn);
                        if (!present(leftValue) || !present(rightValue)) {
                            if (!present(leftValue) && !present(rightValue)) {
                                return left.index - right.index;
                            }
                            return present(leftValue) ? -1 : 1;
                        }
                        var compared = compareValues(leftValue, rightValue, sortColumn);
                        return compared ? compared * direction : left.index - right.index;
                    });
                }
                ctrl.totalItems = entries.length;
                ctrl.totalPages = Math.max(1, Math.ceil(ctrl.totalItems / ctrl.pageSize));
                if (ctrl.page > ctrl.totalPages) {
                    ctrl.page = ctrl.totalPages;
                }
                if (ctrl.page < 1) {
                    ctrl.page = 1;
                }
                var start = (ctrl.page - 1) * ctrl.pageSize;
                ctrl.viewRows = entries.slice(start, start + ctrl.pageSize).map(function (entry) {
                    return entry.row;
                });
                ctrl.pageStart = ctrl.totalItems ? start + 1 : 0;
                ctrl.pageEnd = Math.min(start + ctrl.pageSize, ctrl.totalItems);
                buildPagination();
            }

            function buildPagination() {
                var pages = [];
                var total = ctrl.totalPages;
                var current = ctrl.page;
                if (total <= 7) {
                    for (var page = 1; page <= total; page++) {
                        pages.push({value: page});
                    }
                } else {
                    pages.push({value: 1});
                    if (current > 4) {
                        pages.push({ellipsis: true});
                    }
                    var start;
                    var end;
                    if (current <= 4) {
                        start = 2;
                        end = 5;
                    } else if (current >= total - 3) {
                        start = total - 4;
                        end = total - 1;
                    } else {
                        start = current - 1;
                        end = current + 1;
                    }
                    for (var value = start; value <= end; value++) {
                        pages.push({value: value});
                    }
                    if (current < total - 3) {
                        pages.push({ellipsis: true});
                    }
                    pages.push({value: total});
                }
                ctrl.paginationItems = pages;
            }

            ctrl.cellValue = function (row, column) {
                if (angular.isFunction(column.value)) {
                    return column.value(row);
                }
                return row ? row[column.key] : undefined;
            };
            ctrl.textValue = function (row, column) {
                var value = ctrl.cellValue(row, column);
                if (!present(value)) {
                    return '—';
                }
                if (angular.isObject(value)) {
                    return value.label || value.name || angular.toJson(value);
                }
                return String(value);
            };
            ctrl.formatValue = function (row, column) {
                var value = ctrl.cellValue(row, column);
                var formatted;
                if (column.type === 'bytes') {
                    formatted = Format.bytes(value);
                } else if (column.type === 'percent') {
                    formatted = !present(value) || isNaN(Number(value)) ? '—' : numberText(value, 1) + '%';
                } else if (column.type === 'date') {
                    formatted = Format.date(value);
                } else {
                    formatted = numberText(value);
                }
                return column.unit && formatted !== '—' ? formatted + ' ' + column.unit : formatted;
            };
            ctrl.trendValue = function (row, column) {
                if (angular.isFunction(column.trend)) {
                    return column.trend(row);
                }
                return ctrl.cellValue(row, column);
            };
            ctrl.actionsFor = function (row, column) {
                var next = angular.isFunction(column.actions) ? column.actions(row) : column.actions;
                next = angular.isArray(next) ? next : [];
                var cached = null;
                for (var index = 0; index < actionCache.length; index++) {
                    if (actionCache[index].row === row && actionCache[index].column === column) {
                        cached = actionCache[index];
                        break;
                    }
                }
                if (!cached) {
                    cached = {row: row, column: column, actions: next};
                    actionCache.push(cached);
                    return cached.actions;
                }
                var sameStructure = cached.actions.length === next.length && next.every(function (action, index) {
                    return cached.actions[index].name === action.name;
                });
                if (!sameStructure) {
                    cached.actions = next;
                } else {
                    next.forEach(function (action, index) {
                        angular.extend(cached.actions[index], action);
                    });
                }
                return cached.actions;
            };
            ctrl.actionDisabled = function (action, row) {
                return angular.isFunction(action.disabled) ? !!action.disabled(row) : !!action.disabled;
            };
            ctrl.rowAction = function (action, row) {
                if (!ctrl.actionDisabled(action, row)) {
                    ctrl.onRowAction({action: action.name, row: row, definition: action});
                }
            };
            ctrl.open = function (row, column) {
                ctrl.onOpen({row: row, column: column});
            };
            ctrl.retry = function () {
                ctrl.onRetry();
            };
            ctrl.keyFor = function (row) {
                var value = row;
                String(ctrl.keyName || 'id').split('.').forEach(function (part) {
                    value = value === undefined || value === null ? undefined : value[part];
                });
                return value;
            };
            function selectedIndex(row) {
                var key = ctrl.keyFor(row);
                for (var index = 0; index < ctrl.selected.length; index++) {
                    if (angular.equals(ctrl.selected[index], key)) {
                        return index;
                    }
                }
                return -1;
            }
            ctrl.isSelected = function (row) {
                return selectedIndex(row) >= 0;
            };
            ctrl.toggleRow = function (row) {
                var next = ctrl.selected.slice();
                var index = selectedIndex(row);
                if (index >= 0) {
                    next.splice(index, 1);
                } else {
                    next.push(ctrl.keyFor(row));
                }
                ctrl.selected = next;
            };
            ctrl.allPageSelected = function () {
                return ctrl.viewRows.length > 0 && ctrl.viewRows.every(function (row) {
                    return ctrl.isSelected(row);
                });
            };
            ctrl.somePageSelected = function () {
                return ctrl.viewRows.some(function (row) {
                    return ctrl.isSelected(row);
                });
            };
            ctrl.togglePage = function () {
                var pageKeys = ctrl.viewRows.map(ctrl.keyFor);
                var next;
                if (ctrl.allPageSelected()) {
                    next = ctrl.selected.filter(function (key) {
                        return !pageKeys.some(function (pageKey) { return angular.equals(pageKey, key); });
                    });
                } else {
                    next = ctrl.selected.slice();
                    pageKeys.forEach(function (key) {
                        if (!next.some(function (selectedKey) { return angular.equals(selectedKey, key); })) {
                            next.push(key);
                        }
                    });
                }
                ctrl.selected = next;
            };
            ctrl.clearSelection = function () {
                ctrl.selected = [];
            };
            ctrl.sort = function (column) {
                if (!column.sortable) {
                    return;
                }
                if (ctrl.sortKey === column.key) {
                    ctrl.sortDirection = ctrl.sortDirection === 'asc' ? 'desc' : 'asc';
                } else {
                    ctrl.sortKey = column.key;
                    ctrl.sortDirection = 'asc';
                }
                ctrl.page = 1;
                refresh();
            };
            ctrl.sortAria = function (column) {
                if (ctrl.sortKey !== column.key || !column.sortable) {
                    return undefined;
                }
                return ctrl.sortDirection === 'desc' ? 'descending' : 'ascending';
            };
            ctrl.goToPage = function (page) {
                var next = Number(page);
                if (isFinite(next) && next >= 1 && next <= ctrl.totalPages && next !== ctrl.page) {
                    ctrl.page = Math.floor(next);
                    refresh();
                }
            };
            ctrl.columnStyle = function (column) {
                if (!present(column.width)) {
                    return {};
                }
                return {width: angular.isNumber(column.width) ? column.width + 'px' : String(column.width)};
            };
            ctrl.isNumeric = function (column) {
                return !!column.numeric || ['number', 'bytes', 'percent'].indexOf(column.type) >= 0;
            };
        }]
    });

    function chartHasData(series) {
        return angular.isArray(series) && series.some(function (item) {
            return item && angular.isArray(item.data) && item.data.some(function (point) {
                if (point === null || point === undefined) {
                    return false;
                }
                if (angular.isArray(point)) {
                    return point.length > 1 && point[1] !== null && point[1] !== undefined;
                }
                if (angular.isObject(point)) {
                    return point.y !== null && point.y !== undefined;
                }
                return true;
            });
        });
    }

    dashboard.component('trendChart', {
        bindings: {
            series: '<',
            height: '<',
            unit: '@?'
        },
        template: `
            <div class="trend-chart" ng-style="{height: $ctrl.chartHeight + 'px'}">
                <div class="trend-chart__canvas" ng-style="{visibility: $ctrl.hasData && !$ctrl.unavailable ? 'visible' : 'hidden'}" aria-label="时间趋势图"></div>
                <empty-state ng-if="!$ctrl.hasData" icon="analysis" title="暂无监控数据" description="当前时间范围内还没有可绘制的数据点。"></empty-state>
                <empty-state ng-if="$ctrl.hasData && $ctrl.unavailable" icon="alert" title="图表组件不可用" description="请确认 Highcharts 已正确加载。"></empty-state>
            </div>
        `,
        controller: ['$window', '$element', '$timeout', function ($window, $element, $timeout) {
            var ctrl = this;
            var chart = null;
            var ready = false;
            var resizeObserver = null;
            var resizeFrame = null;
            var resizeUsesTimeout = false;
            var reducedMotion = !!($window.matchMedia && $window.matchMedia('(prefers-reduced-motion: reduce)').matches);

            ctrl.$onInit = function () {
                normalize();
            };
            ctrl.$onChanges = function () {
                normalize();
                if (ready) {
                    syncChart();
                }
            };
            ctrl.$postLink = function () {
                ready = true;
                syncChart();
                observeResize();
            };
            ctrl.$onDestroy = function () {
                destroyChart();
                if (resizeObserver) {
                    resizeObserver.disconnect();
                    resizeObserver = null;
                }
                angular.element($window).off('resize', scheduleReflow);
                cancelReflow();
            };

            function normalize() {
                var height = Number(ctrl.height);
                ctrl.chartHeight = isFinite(height) && height >= 80 ? height : 240;
                ctrl.hasData = chartHasData(ctrl.series);
                ctrl.unavailable = !$window.Highcharts || !angular.isFunction($window.Highcharts.chart);
            }

            function rgba(color, opacity) {
                if ($window.Highcharts && angular.isFunction($window.Highcharts.color)) {
                    return $window.Highcharts.color(color).setOpacity(opacity).get();
                }
                return color;
            }

            function normalizedSeries() {
                return (ctrl.series || []).map(function (item, index) {
                    var color = chartColors[index % chartColors.length];
                    return angular.extend({}, item, {
                        type: 'areaspline',
                        color: color,
                        lineWidth: 1.25,
                        connectNulls: false,
                        marker: {enabled: angular.isArray(item.data) && item.data.length === 1, radius: 3, symbol: 'circle'},
                        fillColor: {
                            linearGradient: {x1: 0, y1: 0, x2: 0, y2: 1},
                            stops: [[0, rgba(color, 0.2)], [1, rgba(color, 0)]]
                        },
                        states: {hover: {lineWidthPlus: 0, halo: {size: 6}}},
                        animation: reducedMotion ? false : {duration: 300}
                    });
                });
            }

            function options() {
                var series = normalizedSeries();
                return {
                    chart: {
                        type: 'areaspline',
                        height: ctrl.chartHeight,
                        backgroundColor: 'transparent',
                        spacing: [12, 8, 8, 8],
                        animation: reducedMotion ? false : {duration: 300},
                        style: {fontFamily: 'Helvetica Neue, Helvetica, Arial, PingFang SC, Microsoft YaHei, sans-serif'}
                    },
                    title: {text: undefined},
                    subtitle: {text: undefined},
                    credits: {enabled: false},
                    exporting: {enabled: false},
                    accessibility: {enabled: false},
                    legend: {
                        enabled: series.length > 1,
                        align: 'left',
                        verticalAlign: 'top',
                        itemStyle: {color: '#696B82', fontSize: '12px', fontWeight: '400'},
                        itemHoverStyle: {color: '#303248'},
                        itemDistance: 18,
                        symbolHeight: 7,
                        symbolWidth: 7,
                        symbolRadius: 3
                    },
                    xAxis: {
                        type: 'datetime',
                        lineColor: '#DDDDE8',
                        tickColor: '#DDDDE8',
                        gridLineColor: '#ECECF3',
                        labels: {style: {color: '#6B6E82', fontSize: '11px'}},
                        crosshair: {color: '#B9BCE0', dashStyle: 'Dash', width: 1}
                    },
                    yAxis: {
                        title: {text: undefined},
                        gridLineColor: '#E8E8F0',
                        gridLineDashStyle: 'Dash',
                        lineWidth: 0,
                        labels: {style: {color: '#6B6E82', fontSize: '11px'}},
                        minPadding: 0.08,
                        maxPadding: 0.12
                    },
                    tooltip: {
                        shared: true,
                        split: false,
                        useHTML: false,
                        xDateFormat: '%Y-%m-%d %H:%M:%S',
                        valueSuffix: ctrl.unit ? ' ' + ctrl.unit : '',
                        backgroundColor: '#FFFFFF',
                        borderColor: '#E3E3ED',
                        borderRadius: 8,
                        shadow: {color: 'rgba(40, 40, 72, 0.10)', offsetX: 0, offsetY: 4, opacity: 0.12, width: 8},
                        style: {color: '#303248', fontSize: '12px'},
                        headerFormat: '<span style="font-size:11px;color:#6B6E82">{point.key}</span><br/>',
                        pointFormat: '<span style="color:{point.color}">●</span> {series.name}: <b>{point.y}</b><br/>'
                    },
                    plotOptions: {
                        series: {
                            connectNulls: false,
                            animation: reducedMotion ? false : {duration: 300},
                            stickyTracking: false
                        },
                        areaspline: {threshold: null}
                    },
                    series: series
                };
            }

            function syncChart() {
                if (!ctrl.hasData || ctrl.unavailable) {
                    destroyChart();
                    return;
                }
                if (chart) {
                    chart.update(options(), true, true);
                } else {
                    var canvas = $element[0].querySelector('.trend-chart__canvas');
                    if (canvas) {
                        chart = $window.Highcharts.chart(canvas, options());
                    }
                }
            }

            function destroyChart() {
                if (chart) {
                    chart.destroy();
                    chart = null;
                }
            }

            function cancelReflow() {
                if (resizeFrame !== null) {
                    if (resizeUsesTimeout) {
                        $timeout.cancel(resizeFrame);
                    } else if ($window.cancelAnimationFrame) {
                        $window.cancelAnimationFrame(resizeFrame);
                    }
                    resizeFrame = null;
                }
            }

            function scheduleReflow() {
                if (!chart || resizeFrame !== null) {
                    return;
                }
                var callback = function () {
                    resizeFrame = null;
                    if (chart) {
                        chart.reflow();
                    }
                };
                resizeUsesTimeout = !$window.requestAnimationFrame;
                resizeFrame = $window.requestAnimationFrame ? $window.requestAnimationFrame(callback) : $timeout(callback, 50);
            }

            function observeResize() {
                if ($window.ResizeObserver) {
                    resizeObserver = new $window.ResizeObserver(scheduleReflow);
                    resizeObserver.observe($element[0]);
                } else {
                    angular.element($window).on('resize', scheduleReflow);
                }
            }
        }]
    });

    dashboard.component('sparkline', {
        bindings: {
            data: '<'
        },
        template: `
            <span class="sparkline" ng-if="$ctrl.segments.length">
                <svg viewBox="0 0 100 32" preserveAspectRatio="none" role="img" aria-label="迷你趋势图">
                    <path ng-repeat="segment in $ctrl.segments track by $index" class="sparkline__area" ng-attr-d="{{segment.area}}"></path>
                    <path ng-repeat="segment in $ctrl.segments track by $index" class="sparkline__line" ng-attr-d="{{segment.line}}"></path>
                    <circle ng-repeat="segment in $ctrl.segments track by $index" ng-if="segment.points.length === 1"
                        class="sparkline__point" ng-attr-cx="{{segment.points[0].x}}" ng-attr-cy="{{segment.points[0].y}}" r="1.8"></circle>
                </svg>
            </span>
            <span class="sparkline-empty" ng-if="!$ctrl.segments.length">—</span>
        `,
        controller: [function () {
            var ctrl = this;
            ctrl.segments = [];
            ctrl.$onChanges = build;
            ctrl.$onInit = build;
            function build() {
                var values = (ctrl.data || []).map(function (point) {
                    var value = angular.isArray(point) ? point[1] : (angular.isObject(point) ? point.y : point);
                    return present(value) && !isNaN(Number(value)) ? Number(value) : null;
                });
                var numeric = values.filter(function (value) { return value !== null; });
                ctrl.segments = [];
                if (!numeric.length) {
                    return;
                }
                var min = Math.min.apply(Math, numeric);
                var max = Math.max.apply(Math, numeric);
                var range = max - min || 1;
                var segment = [];
                values.forEach(function (value, index) {
                    if (value === null) {
                        addSegment(segment);
                        segment = [];
                        return;
                    }
                    var x = values.length === 1 ? 50 : (index / (values.length - 1)) * 100;
                    var y = range === 0 ? 16 : 28 - ((value - min) / range) * 24;
                    if (max === min) {
                        y = 16;
                    }
                    segment.push({x: Number(x.toFixed(2)), y: Number(y.toFixed(2))});
                });
                addSegment(segment);
            }
            function addSegment(points) {
                if (!points.length) {
                    return;
                }
                var line = points.map(function (point, index) {
                    return (index ? 'L' : 'M') + point.x + ' ' + point.y;
                }).join(' ');
                var area = line;
                if (points.length > 1) {
                    area += ' L' + points[points.length - 1].x + ' 32 L' + points[0].x + ' 32 Z';
                }
                ctrl.segments.push({points: points, line: line, area: area});
            }
        }]
    });

    dashboard.component('chartCard', {
        bindings: {
            title: '@?',
            unit: '@?',
            subtitle: '@?',
            onExpand: '&'
        },
        transclude: true,
        template: `
            <section class="chart-card">
                <header class="chart-card__header">
                    <div>
                        <div class="chart-card__title-row"><h2>{{$ctrl.title}}</h2><span ng-if="$ctrl.unit">{{$ctrl.unit}}</span></div>
                        <p ng-if="$ctrl.subtitle">{{$ctrl.subtitle}}</p>
                    </div>
                    <button type="button" class="icon-button" ng-click="$ctrl.onExpand()" ng-attr-aria-label="放大{{$ctrl.title || '图表'}}" title="放大图表">
                        <ui-icon name="expand" size="16"></ui-icon>
                    </button>
                </header>
                <div class="chart-card__content" ng-transclude></div>
            </section>
        `
    });

    dashboard.component('resourceDrawer', {
        bindings: {
            open: '<',
            title: '<',
            subtitle: '<',
            width: '<',
            submitting: '<',
            submitDisabled: '<',
            submitLabel: '@?',
            showSubmit: '<',
            error: '<',
            onClose: '&',
            onSubmit: '&'
        },
        transclude: true,
        template: `
            <div class="drawer-backdrop" ng-if="$ctrl.open" ng-click="$ctrl.backdrop($event)">
                <aside class="resource-drawer" role="dialog" aria-modal="true" ng-attr-aria-labelledby="{{$ctrl.titleId}}"
                    ng-attr-aria-describedby="{{$ctrl.subtitle ? $ctrl.subtitleId : undefined}}"
                    ng-style="{width: $ctrl.drawerWidth + 'px'}" tabindex="-1">
                    <header class="resource-drawer__header">
                        <div class="resource-drawer__heading">
                            <h2 id="{{$ctrl.titleId}}">{{$ctrl.title || '资源详情'}}</h2>
                            <p ng-if="$ctrl.subtitle" id="{{$ctrl.subtitleId}}">{{$ctrl.subtitle}}</p>
                        </div>
                        <button type="button" class="icon-button drawer-close" ng-click="$ctrl.close()" ng-disabled="$ctrl.submitting" aria-label="关闭抽屉">
                            <ui-icon name="close" size="19"></ui-icon>
                        </button>
                    </header>
                    <div class="resource-drawer__body">
                        <div class="inline-alert inline-alert--error" ng-if="$ctrl.error" role="alert">
                            <ui-icon name="alert" size="17"></ui-icon><span>{{$ctrl.error}}</span>
                        </div>
                        <div ng-transclude></div>
                    </div>
                    <footer class="resource-drawer__footer">
                        <button type="button" class="button button-secondary" ng-click="$ctrl.close()" ng-disabled="$ctrl.submitting">取消</button>
                        <button type="button" class="button button-primary" ng-if="$ctrl.shouldShowSubmit()" ng-click="$ctrl.submit()"
                            ng-disabled="$ctrl.submitting || $ctrl.submitDisabled">
                            <ui-icon name="refresh" size="15" class="is-spinning" ng-if="$ctrl.submitting"></ui-icon>
                            <ui-icon name="save" size="15" ng-if="!$ctrl.submitting"></ui-icon>
                            <span>{{$ctrl.submitting ? '提交中…' : ($ctrl.submitLabel || '保存')}}</span>
                        </button>
                    </footer>
                </aside>
            </div>
        `,
        controller: ['$document', '$element', '$scope', '$timeout', 'UiOverlayStack', function ($document, $element, $scope, $timeout, UiOverlayStack) {
            var ctrl = this;
            var ready = false;
            var active = false;
            var focusTimer = null;
            var previousFocus = null;
            var token = {};
            var id = ++iconId;
            ctrl.titleId = 'resource-drawer-title-' + id;
            ctrl.subtitleId = 'resource-drawer-subtitle-' + id;

            ctrl.$onChanges = function (changes) {
                var width = Number(ctrl.width);
                ctrl.drawerWidth = isFinite(width) && width >= 280 ? Math.min(width, 1600) : 760;
                if (changes.open && ready) {
                    if (ctrl.open) {
                        activate();
                    } else {
                        deactivate();
                    }
                }
            };
            ctrl.$postLink = function () {
                ready = true;
                $document.on('keydown', onKeydown);
                if (ctrl.open) {
                    activate();
                }
            };
            ctrl.$onDestroy = function () {
                $document.off('keydown', onKeydown);
                if (focusTimer) {
                    $timeout.cancel(focusTimer);
                }
                deactivate();
            };
            ctrl.shouldShowSubmit = function () {
                return ctrl.showSubmit !== false;
            };
            ctrl.close = function () {
                if (ctrl.open && !ctrl.submitting) {
                    ctrl.onClose();
                }
            };
            ctrl.submit = function () {
                if (!ctrl.submitting && !ctrl.submitDisabled) {
                    ctrl.onSubmit();
                }
            };
            ctrl.backdrop = function (event) {
                if (event.target === event.currentTarget) {
                    ctrl.close();
                }
            };
            function activate() {
                if (active) {
                    return;
                }
                active = true;
                previousFocus = $document[0].activeElement;
                UiOverlayStack.push(token);
                angular.element($document[0].body).addClass('drawer-open');
                focusTimer = $timeout(function () {
                    var dialog = $element[0].querySelector('.resource-drawer');
                    if (!dialog) {
                        return;
                    }
                    var body = dialog.querySelector('.resource-drawer__body');
                    var firstInput = body && focusable(body).filter(function (node) {
                        return /^(INPUT|SELECT|TEXTAREA)$/.test(node.tagName);
                    })[0];
                    var target = firstInput || focusable(dialog)[0] || dialog;
                    target.focus();
                }, 0, false);
            }
            function deactivate() {
                if (!active) {
                    return;
                }
                active = false;
                UiOverlayStack.remove(token);
                angular.element($document[0].body).removeClass('drawer-open');
                if (previousFocus && angular.isFunction(previousFocus.focus) && $document[0].contains(previousFocus)) {
                    previousFocus.focus();
                }
                previousFocus = null;
            }
            function onKeydown(event) {
                if (!ctrl.open || !active || !UiOverlayStack.isTop(token)) {
                    return;
                }
                var dialog = $element[0].querySelector('.resource-drawer');
                if (event.key === 'Escape' || event.keyCode === 27) {
                    if (!ctrl.submitting) {
                        event.preventDefault();
                        $scope.$evalAsync(function () { ctrl.close(); });
                    }
                } else if (dialog) {
                    trapTab(event, dialog);
                }
            }
        }]
    });

    dashboard.factory('Confirm', ['$q', '$rootScope', function ($q, $rootScope) {
        var state = {current: null};
        function digest() {
            $rootScope.$evalAsync(angular.noop);
        }
        function detailsText(details) {
            if (!present(details)) {
                return '';
            }
            if (angular.isArray(details)) {
                return details.join('\n');
            }
            if (angular.isObject(details)) {
                return angular.toJson(details, 2);
            }
            return String(details);
        }
        function open(options) {
            options = options || {};
            if (state.current) {
                state.current.deferred.reject('superseded');
            }
            var deferred = $q.defer();
            state.current = {
                title: options.title || '确认操作',
                message: present(options.message) ? String(options.message) : '',
                details: detailsText(options.details),
                confirmText: options.confirmText || '确认',
                danger: !!options.danger,
                deferred: deferred
            };
            digest();
            return deferred.promise;
        }
        function settle(method, value) {
            var current = state.current;
            if (!current) {
                return;
            }
            state.current = null;
            current.deferred[method](value);
            digest();
        }
        return {
            state: state,
            open: open,
            confirm: function () { settle('resolve', true); },
            cancel: function (reason) { settle('reject', reason || 'cancelled'); }
        };
    }]);

    dashboard.component('confirmDialog', {
        template: `
            <div class="confirm-backdrop" ng-if="$ctrl.state.current" ng-click="$ctrl.backdrop($event)">
                <section class="confirm-dialog" ng-class="{'confirm-dialog--danger': $ctrl.state.current.danger}"
                    role="alertdialog" aria-modal="true" aria-labelledby="confirm-dialog-title" aria-describedby="confirm-dialog-message" tabindex="-1">
                    <div class="confirm-dialog__icon"><ui-icon name="{{$ctrl.state.current.danger ? 'alert' : 'info'}}" size="22"></ui-icon></div>
                    <div class="confirm-dialog__content">
                        <h2 id="confirm-dialog-title">{{$ctrl.state.current.title}}</h2>
                        <p id="confirm-dialog-message">{{$ctrl.state.current.message}}</p>
                        <pre class="confirm-dialog__details" ng-if="$ctrl.state.current.details">{{$ctrl.state.current.details}}</pre>
                    </div>
                    <div class="confirm-dialog__actions">
                        <button type="button" class="button button-secondary confirm-cancel" ng-click="$ctrl.cancel()">取消</button>
                        <button type="button" class="button confirm-accept" ng-class="$ctrl.state.current.danger ? 'button-danger' : 'button-primary'" ng-click="$ctrl.confirm()">
                            {{$ctrl.state.current.confirmText}}
                        </button>
                    </div>
                </section>
            </div>
        `,
        controller: ['Confirm', '$document', '$element', '$scope', '$timeout', 'UiOverlayStack',
            function (Confirm, $document, $element, $scope, $timeout, UiOverlayStack) {
                var ctrl = this;
                var active = false;
                var previousFocus = null;
                var focusTimer = null;
                var token = {};
                ctrl.state = Confirm.state;

                ctrl.$postLink = function () {
                    $document.on('keydown', onKeydown);
                    $scope.$watch(function () { return Confirm.state.current; }, function (current) {
                        if (current && !active) {
                            activate();
                        } else if (!current && active) {
                            deactivate();
                        } else if (current) {
                            focusDialog();
                        }
                    });
                    if (Confirm.state.current) {
                        activate();
                    }
                };
                ctrl.$onDestroy = function () {
                    $document.off('keydown', onKeydown);
                    if (focusTimer) {
                        $timeout.cancel(focusTimer);
                    }
                    deactivate();
                };
                ctrl.confirm = function () { Confirm.confirm(); };
                ctrl.cancel = function () { Confirm.cancel('cancelled'); };
                ctrl.backdrop = function (event) {
                    if (event.target === event.currentTarget) {
                        Confirm.cancel('backdrop');
                    }
                };
                function activate() {
                    active = true;
                    previousFocus = $document[0].activeElement;
                    UiOverlayStack.push(token);
                    angular.element($document[0].body).addClass('confirm-open');
                    focusDialog();
                }
                function deactivate() {
                    if (!active) {
                        return;
                    }
                    active = false;
                    UiOverlayStack.remove(token);
                    angular.element($document[0].body).removeClass('confirm-open');
                    if (previousFocus && angular.isFunction(previousFocus.focus) && $document[0].contains(previousFocus)) {
                        previousFocus.focus();
                    }
                    previousFocus = null;
                }
                function focusDialog() {
                    if (focusTimer) {
                        $timeout.cancel(focusTimer);
                    }
                    focusTimer = $timeout(function () {
                        var dialog = $element[0].querySelector('.confirm-dialog');
                        if (!dialog) {
                            return;
                        }
                        var target = dialog.querySelector(Confirm.state.current && Confirm.state.current.danger ? '.confirm-cancel' : '.confirm-accept');
                        (target || focusable(dialog)[0] || dialog).focus();
                    }, 0, false);
                }
                function onKeydown(event) {
                    if (!Confirm.state.current || !active || !UiOverlayStack.isTop(token)) {
                        return;
                    }
                    var dialog = $element[0].querySelector('.confirm-dialog');
                    if (event.key === 'Escape' || event.keyCode === 27) {
                        event.preventDefault();
                        Confirm.cancel('escape');
                    } else if (dialog) {
                        trapTab(event, dialog);
                    }
                }
            }
        ]
    });

    dashboard.factory('Toast', ['$timeout', '$rootScope', function ($timeout, $rootScope) {
        var nextId = 0;
        var items = [];
        function push(type, message) {
            var item = {
                id: ++nextId,
                type: type,
                message: present(message) ? String(message) : '操作已完成',
                icon: type === 'success' ? 'check' : (type === 'error' ? 'alert' : 'info'),
                timer: null
            };
            items.push(item);
            item.timer = $timeout(function () { dismiss(item.id); }, type === 'error' ? 6500 : 4200);
            $rootScope.$evalAsync(angular.noop);
            return item.id;
        }
        function dismiss(id) {
            for (var index = items.length - 1; index >= 0; index--) {
                if (items[index].id === id) {
                    if (items[index].timer) {
                        $timeout.cancel(items[index].timer);
                    }
                    items.splice(index, 1);
                }
            }
        }
        return {
            items: items,
            success: function (message) { return push('success', message); },
            error: function (message) { return push('error', message); },
            info: function (message) { return push('info', message); },
            dismiss: dismiss
        };
    }]);

    dashboard.component('toastStack', {
        template: `
            <div class="toast-stack" aria-live="polite" aria-atomic="false" aria-label="操作通知">
                <div class="toast toast--{{toast.type}}" ng-repeat="toast in $ctrl.items track by toast.id" ng-attr-role="{{toast.type === 'error' ? 'alert' : 'status'}}">
                    <span class="toast__icon"><ui-icon name="{{toast.icon}}" size="17"></ui-icon></span>
                    <span class="toast__message">{{toast.message}}</span>
                    <button type="button" class="toast__close" ng-click="$ctrl.dismiss(toast.id)" aria-label="关闭通知">
                        <ui-icon name="close" size="15"></ui-icon>
                    </button>
                </div>
            </div>
        `,
        controller: ['Toast', function (Toast) {
            this.items = Toast.items;
            this.dismiss = Toast.dismiss;
        }]
    });

    dashboard.component('emptyState', {
        bindings: {
            icon: '@?',
            title: '@?',
            description: '@?',
            actionLabel: '@?',
            onAction: '&?'
        },
        template: `
            <div class="empty-state">
                <div class="empty-state__icon"><ui-icon name="{{$ctrl.icon || 'database'}}" size="25"></ui-icon></div>
                <h2>{{$ctrl.title || '暂无数据'}}</h2>
                <p ng-if="$ctrl.description">{{$ctrl.description}}</p>
                <button type="button" class="button button-secondary" ng-if="$ctrl.actionLabel" ng-click="$ctrl.act()">
                    {{$ctrl.actionLabel}}
                </button>
            </div>
        `,
        controller: [function () {
            this.act = function () {
                if (this.onAction) {
                    this.onAction();
                }
            };
        }]
    });

    dashboard.component('loadingSkeleton', {
        bindings: {
            variant: '@?'
        },
        template: `
            <div class="loading-skeleton loading-skeleton--{{$ctrl.variant || 'default'}}" role="status" aria-label="正在加载">
                <span class="sr-only">正在加载…</span>
                <div class="skeleton-page" ng-if="$ctrl.variant === 'page'" aria-hidden="true">
                    <div class="skeleton-block skeleton-heading"></div>
                    <div class="skeleton-block skeleton-subheading"></div>
                    <div class="skeleton-metrics"><div class="skeleton-block" ng-repeat="item in $ctrl.cards track by $index"></div></div>
                    <div class="skeleton-panels"><div class="skeleton-block"></div><div class="skeleton-block"></div></div>
                </div>
                <div class="skeleton-table" ng-if="$ctrl.variant === 'table'" aria-hidden="true">
                    <div class="skeleton-block skeleton-table__header"></div>
                    <div class="skeleton-table__row" ng-repeat="row in $ctrl.rows track by $index">
                        <span class="skeleton-block"></span><span class="skeleton-block"></span><span class="skeleton-block"></span><span class="skeleton-block"></span>
                    </div>
                </div>
                <div class="skeleton-default" ng-if="$ctrl.variant !== 'page' && $ctrl.variant !== 'table'" aria-hidden="true">
                    <div class="skeleton-block skeleton-heading"></div>
                    <div class="skeleton-block" ng-repeat="row in $ctrl.shortRows track by $index"></div>
                </div>
            </div>
        `,
        controller: [function () {
            this.cards = [1, 2, 3, 4];
            this.rows = [1, 2, 3, 4, 5, 6];
            this.shortRows = [1, 2, 3];
        }]
    });
}());
