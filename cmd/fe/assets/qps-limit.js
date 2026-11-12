'use strict';

dashboard.component('qpsLimitPanel', {
    bindings: {
        product: '<'
    },
    template: `
        <section class="qps-limit-panel" aria-label="Proxy 限流配置">
            <loading-skeleton ng-if="$ctrl.initialLoading" variant="page"></loading-skeleton>

            <empty-state ng-if="!$ctrl.initialLoading && !$ctrl.loaded && $ctrl.loadError"
                icon="alert" title="无法获取 Proxy 限流配置" description="{{$ctrl.loadError}}"
                action-label="重新加载" on-action="$ctrl.retry()"></empty-state>

            <div ng-if="$ctrl.loaded">
                <div class="inline-alert inline-alert--error" ng-if="$ctrl.refreshError" role="alert">
                    <ui-icon name="alert" size="18"></ui-icon>
                    <div>
                        <strong>刷新失败，当前展示上次成功加载的数据</strong>
                        <p>{{$ctrl.refreshError}}</p>
                        <button class="button button-secondary" type="button" ng-click="$ctrl.retry()">重试</button>
                    </div>
                </div>

                <div class="panel">
                    <div class="card-header">
                        <div>
                            <h2 class="card-title">Proxy QPS 上限</h2>
                            <p>为整个集群设置每个 Proxy 的请求速率上限，0 表示不限流。</p>
                        </div>
                        <div class="card-actions">
                            <button class="button button-secondary" type="button"
                                ng-click="$ctrl.refresh()" ng-disabled="$ctrl.refreshing || $ctrl.submitting">
                                <ui-icon name="refresh" size="16" ng-class="{'is-spinning': $ctrl.refreshing}"></ui-icon>
                                {{$ctrl.refreshing ? '刷新中' : '刷新'}}
                            </button>
                            <button class="button button-primary" type="button"
                                ng-click="$ctrl.openEditor()" ng-disabled="$ctrl.refreshing || $ctrl.submitting">
                                <ui-icon name="edit" size="16"></ui-icon>编辑限流
                            </button>
                        </div>
                    </div>

                    <div class="metrics-grid">
                        <metric-card ng-repeat="metric in $ctrl.metrics track by metric.key" metric="metric"></metric-card>
                    </div>

                    <div class="card">
                        <div class="form-grid">
                            <div>
                                <span class="form-label">sync_status</span>
                                <div><status-tag status="$ctrl.syncTone()" label="{{$ctrl.syncLabel()}}"></status-tag></div>
                                <p class="form-hint">{{$ctrl.model.syncStatus || '—'}}</p>
                            </div>
                            <div>
                                <span class="form-label">enabled</span>
                                <div><status-tag status="$ctrl.enabledTone()" label="{{$ctrl.enabledLabel()}}"></status-tag></div>
                                <p class="form-hint">由 Dashboard 返回的 enabled 字段决定</p>
                            </div>
                        </div>
                        <div class="inline-alert inline-alert--info">
                            <ui-icon name="info" size="18"></ui-icon>
                            <div>
                                <strong>预计同步状态</strong>
                                <p>{{$ctrl.expectedSyncText()}}</p>
                            </div>
                        </div>
                        <p class="form-hint" ng-if="$ctrl.refreshFeedback" aria-live="polite">
                            {{$ctrl.refreshFeedback}}
                        </p>
                    </div>
                </div>
            </div>

            <resource-drawer open="$ctrl.editorOpen" title="'编辑 Proxy 限流'"
                subtitle="'保存后生成新 revision，并将配置同步到所有 Proxy'"
                width="520" submitting="$ctrl.submitting"
                submit-disabled="$ctrl.submitting || !$ctrl.editor" submit-label="保存并同步"
                show-submit="true" error="$ctrl.drawerError"
                on-close="$ctrl.closeEditor()" on-submit="$ctrl.submitLimit()">
                <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.limit}">
                    <label class="form-label" for="qps-limit-input">QPS 上限</label>
                    <input id="qps-limit-input" class="form-control" type="text" inputmode="numeric"
                        autocomplete="off" spellcheck="false" ng-model="$ctrl.editor.limitText"
                        ng-disabled="$ctrl.submitting" ng-change="$ctrl.clearLimitError()"
                        aria-describedby="qps-limit-hint qps-limit-error" required>
                    <p class="form-hint" id="qps-limit-hint">请输入 0 到 9,007,199,254,740,991 之间的整数；0 表示关闭限流。</p>
                    <p class="form-error" id="qps-limit-error" ng-if="$ctrl.fieldErrors.limit" role="alert">
                        {{$ctrl.fieldErrors.limit}}
                    </p>
                </div>
                <div class="inline-alert inline-alert--info">
                    <ui-icon name="info" size="18"></ui-icon>
                    <div>
                        <strong>预计同步状态</strong>
                        <p>{{$ctrl.expectedDraftSyncText()}}</p>
                    </div>
                </div>
            </resource-drawer>
        </section>
    `,
    controller: ['Api', 'Format', 'Toast', '$scope',
        function (Api, Format, Toast, $scope) {
            var ctrl = this;
            var maxSafeInteger = Number.MAX_SAFE_INTEGER || 9007199254740991;
            var requestSequence = 0;
            var mutationSequence = 0;
            var destroyed = false;

            ctrl.model = emptyModel();
            ctrl.metrics = [];
            ctrl.loaded = false;
            ctrl.initialLoading = true;
            ctrl.refreshing = false;
            ctrl.loadError = '';
            ctrl.refreshError = '';
            ctrl.refreshFeedback = '';
            ctrl.lastUpdated = null;
            ctrl.editorOpen = false;
            ctrl.editor = {limitText: '0'};
            ctrl.fieldErrors = {};
            ctrl.drawerError = '';
            ctrl.submitting = false;

            function emptyModel() {
                return {
                    revision: null,
                    limit: null,
                    enabled: null,
                    syncStatus: ''
                };
            }

            function hasValue(value) {
                return value !== undefined && value !== null;
            }

            function normalizeModel(data) {
                data = data || {};
                return {
                    revision: hasValue(data.revision) ? data.revision : null,
                    limit: hasValue(data.limit) ? data.limit : null,
                    enabled: typeof data.enabled === 'boolean' ? data.enabled : null,
                    syncStatus: hasValue(data.sync_status) ? String(data.sync_status) : ''
                };
            }

            function numberOrPlaceholder(value) {
                return hasValue(value) && value !== '' ? Format.number(value) : '—';
            }

            function updateMetrics() {
                ctrl.metrics = [
                    {
                        key: 'limit',
                        title: '当前 limit',
                        value: numberOrPlaceholder(ctrl.model.limit),
                        caption: 'QPS / Proxy；0 表示不限流',
                        icon: 'activity',
                        theme: ctrl.model.enabled === true ? 'primary' : 'neutral'
                    },
                    {
                        key: 'revision',
                        title: '配置 revision',
                        value: hasValue(ctrl.model.revision) && ctrl.model.revision !== '' ? '#' + numberOrPlaceholder(ctrl.model.revision) : '—',
                        caption: '每次保存自动递增',
                        icon: 'history',
                        theme: 'neutral'
                    },
                    {
                        key: 'enabled',
                        title: '限流状态',
                        value: ctrl.enabledLabel(),
                        caption: 'enabled',
                        icon: 'shield',
                        theme: ctrl.model.enabled === true ? 'primary' : 'neutral'
                    },
                    {
                        key: 'sync',
                        title: '同步状态',
                        value: ctrl.syncLabel(),
                        caption: ctrl.model.syncStatus || '—',
                        icon: 'refresh',
                        theme: 'neutral'
                    }
                ];
            }

            ctrl.enabledLabel = function () {
                if (ctrl.model.enabled === true) { return '已启用'; }
                if (ctrl.model.enabled === false) { return '未启用'; }
                return '—';
            };

            ctrl.enabledTone = function () {
                if (ctrl.model.enabled === true) { return 'success'; }
                return 'neutral';
            };

            ctrl.syncLabel = function () {
                var status = ctrl.model.syncStatus;
                if (status === 'ready') { return '已同步'; }
                if (status === 'not_configured') { return '未配置'; }
                if (status && status.indexOf('proxy_sync_failed:') === 0) { return '部分同步失败'; }
                return status ? '状态未知' : '—';
            };

            ctrl.syncTone = function () {
                var status = ctrl.model.syncStatus;
                if (status === 'ready') { return 'success'; }
                if (status && status.indexOf('proxy_sync_failed:') === 0) { return 'error'; }
                if (status === 'not_configured') { return 'neutral'; }
                return 'warning';
            };

            ctrl.expectedSyncText = function () {
                var status = ctrl.model.syncStatus;
                if (status === 'ready') {
                    return '当前 revision 已应用到所有可用 Proxy；下次保存后将生成新版本并重新同步。';
                }
                if (status && status.indexOf('proxy_sync_failed:') === 0) {
                    return '部分 Proxy 尚未应用当前 revision，可通过刷新重新探测，或重新保存以触发同步。';
                }
                if (status === 'not_configured') {
                    return '当前尚无已保存配置；首次保存后将生成 revision 并同步到所有 Proxy。';
                }
                return '同步结果以 Dashboard 返回的 sync_status 为准。';
            };

            function parseLimitText(raw) {
                var text = hasValue(raw) ? String(raw).trim() : '';
                if (!/^\d+$/.test(text)) { return null; }
                var value = Number(text);
                if (!isFinite(value) || Math.floor(value) !== value || value < 0 || value > maxSafeInteger) {
                    return null;
                }
                return value;
            }

            ctrl.expectedDraftSyncText = function () {
                var value = parseLimitText(ctrl.editor && ctrl.editor.limitText);
                if (value === null) {
                    return '请输入有效的非负安全整数后查看预计结果。';
                }
                if (value === 0) {
                    return '保存后将生成新 revision，并同步关闭所有 Proxy 的限流。';
                }
                return '保存后将生成新 revision，并把每个 Proxy 的上限同步为 ' + Format.number(value) + ' QPS。';
            };

            function refreshDoneText(options) {
                var time = Format.time(new Date());
                if (options.manual) { return '手动刷新完成 · ' + time; }
                if (options.afterSave) { return '保存后重新获取配置完成 · ' + time; }
                if (!options.background) { return '配置加载完成 · ' + time; }
                return '后台刷新完成 · ' + time;
            }

            function load(options) {
                options = options || {};
                var product = ctrl.product;
                if (!product) {
                    ctrl.initialLoading = false;
                    ctrl.refreshing = false;
                    ctrl.loadError = '请先选择集群';
                    return;
                }
                var sequence = ++requestSequence;
                var hadData = ctrl.loaded;
                ctrl.initialLoading = !hadData;
                ctrl.refreshing = hadData;
                ctrl.refreshError = '';
                if (!hadData) {
                    ctrl.loadError = '';
                    ctrl.refreshFeedback = '';
                } else {
                    ctrl.refreshFeedback = options.manual ? '正在手动刷新…' : '正在后台刷新…';
                }

                return Api.request('GET', product, '/api/topom/proxy/qps-limit/{xauth}').then(function (data) {
                    if (destroyed || sequence !== requestSequence || product !== ctrl.product) { return; }
                    ctrl.model = normalizeModel(data);
                    ctrl.loaded = true;
                    ctrl.lastUpdated = new Date();
                    ctrl.loadError = '';
                    ctrl.refreshError = '';
                    ctrl.refreshFeedback = refreshDoneText(options);
                    updateMetrics();
                    if (options.manual) {
                        Toast.success('Proxy 限流配置已刷新');
                    }
                }).catch(function (response) {
                    if (destroyed || sequence !== requestSequence || product !== ctrl.product) { return; }
                    var message = Api.errorMessage(response);
                    if (ctrl.loaded) {
                        ctrl.refreshError = message;
                        ctrl.refreshFeedback = (options.manual ? '手动' : '后台') + '刷新失败，已保留原配置';
                        if (options.manual) { Toast.error(message); }
                    } else {
                        ctrl.loadError = message;
                        ctrl.refreshFeedback = '';
                    }
                }).finally(function () {
                    if (destroyed || sequence !== requestSequence || product !== ctrl.product) { return; }
                    ctrl.initialLoading = false;
                    ctrl.refreshing = false;
                });
            }

            ctrl.refresh = function () {
                if (ctrl.refreshing || ctrl.submitting) { return; }
                return load({manual: true, background: ctrl.loaded});
            };

            ctrl.retry = function () {
                return load({manual: true, background: ctrl.loaded});
            };

            ctrl.openEditor = function () {
                if (ctrl.submitting) { return; }
                ctrl.editor = {
                    limitText: hasValue(ctrl.model.limit) ? String(ctrl.model.limit) : ''
                };
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                ctrl.editorOpen = true;
            };

            function scrubEditor() {
                if (ctrl.editor) { ctrl.editor.limitText = ''; }
            }

            ctrl.closeEditor = function () {
                if (ctrl.submitting) { return; }
                ctrl.editorOpen = false;
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                scrubEditor();
            };

            ctrl.clearLimitError = function () {
                if (ctrl.submitting) { return; }
                ctrl.fieldErrors.limit = '';
                ctrl.drawerError = '';
            };

            ctrl.submitLimit = function () {
                if (ctrl.submitting || destroyed || !ctrl.editor) { return; }
                var limit = parseLimitText(ctrl.editor.limitText);
                if (limit === null) {
                    ctrl.fieldErrors.limit = '请输入 0 到 ' + Format.number(maxSafeInteger) + ' 之间的非负安全整数';
                    ctrl.drawerError = '限流值未通过校验，请修改后重试';
                    return;
                }
                var product = ctrl.product;
                if (!product) { return; }
                var sequence = ++mutationSequence;
                ctrl.submitting = true;
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';

                Api.request('PUT', product, '/api/topom/proxy/qps-limit/{xauth}', {limit: limit}).then(function () {
                    if (destroyed || sequence !== mutationSequence || product !== ctrl.product) { return; }
                    ctrl.editorOpen = false;
                    scrubEditor();
                    ctrl.drawerError = '';
                    Toast.success('Proxy 限流已保存，正在刷新配置与同步状态');
                    load({background: true, afterSave: true});
                }).catch(function (response) {
                    if (destroyed || sequence !== mutationSequence || product !== ctrl.product) { return; }
                    var message = Api.errorMessage(response);
                    ctrl.drawerError = message;
                    ctrl.fieldErrors.limit = message;
                }).finally(function () {
                    if (destroyed || sequence !== mutationSequence || product !== ctrl.product) { return; }
                    ctrl.submitting = false;
                });
            };

            function resetForProduct() {
                requestSequence++;
                mutationSequence++;
                scrubEditor();
                ctrl.model = emptyModel();
                ctrl.loaded = false;
                ctrl.initialLoading = true;
                ctrl.refreshing = false;
                ctrl.loadError = '';
                ctrl.refreshError = '';
                ctrl.refreshFeedback = '';
                ctrl.lastUpdated = null;
                ctrl.editorOpen = false;
                ctrl.editor = {limitText: '0'};
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                ctrl.submitting = false;
                updateMetrics();
            }

            ctrl.$onInit = function () {
                updateMetrics();
                load();
            };

            ctrl.$onChanges = function (changes) {
                if (changes.product && !changes.product.isFirstChange() &&
                    changes.product.currentValue !== changes.product.previousValue) {
                    resetForProduct();
                    load();
                }
            };

            ctrl.$onDestroy = function () {
                destroyed = true;
                requestSequence++;
                mutationSequence++;
                scrubEditor();
                // This component creates no timers; pending responses are ignored after destroy.
            };

            $scope.$on('cluster:refreshRequested', function (event, payload) {
                if (!destroyed && payload && payload.product === ctrl.product) {
                    load({background: true});
                }
            });
        }
    ]
});
