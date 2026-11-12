'use strict';

dashboard.component('recoveryPage', {
    bindings: {
        product: '<'
    },
    template: `
        <div class="recovery-page">
            <page-header title="PITR 数据闪回"
                description="按 Unix 时间点截断 Group Master 的 AOF，将数据闪回到指定时刻，并跟踪 shutdown、截断、恢复与副本重同步的完整过程。">
                <button type="button" class="button button-primary" ng-click="$ctrl.newTask()"
                    ng-disabled="$ctrl.submitting || $ctrl.actionSubmitting || $ctrl.confirming">
                    <ui-icon name="plus" size="16"></ui-icon>
                    <span>新建闪回任务</span>
                </button>
            </page-header>

            <loading-skeleton ng-if="$ctrl.initialLoading" variant="page"></loading-skeleton>

            <empty-state ng-if="!$ctrl.initialLoading && !$ctrl.loaded && $ctrl.loadError"
                icon="alert" title="{{$ctrl.forbidden ? '没有 PITR 访问权限（403）' : '无法获取 PITR 任务'}}"
                description="{{$ctrl.loadError}}" action-label="重新加载" on-action="$ctrl.retry()"></empty-state>

            <div ng-if="$ctrl.loaded">
                <section class="panel" aria-label="PITR 任务摘要">
                    <div class="card-header">
                        <div>
                            <h2 class="card-title">任务摘要</h2>
                            <p>统计当前集群的全部闪回任务；非终态任务每 2 秒自动更新。</p>
                        </div>
                        <span class="form-hint" ng-if="$ctrl.lastUpdated">更新于 {{$ctrl.dateText($ctrl.lastUpdated, true)}}</span>
                    </div>
                    <div class="metrics-grid">
                        <metric-card ng-repeat="metric in $ctrl.metrics track by metric.key" metric="metric"></metric-card>
                    </div>
                    <div class="inline-alert inline-alert--error" ng-if="$ctrl.refreshError" role="alert">
                        <ui-icon name="alert" size="18"></ui-icon>
                        <div>
                            <strong>{{$ctrl.forbidden ? '权限不足（403）' : '刷新失败'}}，当前展示上次成功加载的数据</strong>
                            <p>{{$ctrl.refreshError}}</p>
                            <button type="button" class="button button-secondary" ng-click="$ctrl.retry()"
                                ng-disabled="$ctrl.requestInFlight">重试</button>
                        </div>
                    </div>
                    <p class="form-hint" ng-if="$ctrl.refreshFeedback" aria-live="polite">{{$ctrl.refreshFeedback}}</p>
                </section>

                <section class="panel" aria-label="PITR 任务列表">
                    <div class="card-header">
                        <div>
                            <h2 class="card-title">闪回任务</h2>
                            <p>点击 Job ID 查看步骤、时间与 AOF Manifest 信息。</p>
                        </div>
                        <span class="form-hint">共 {{$ctrl.jobs.length}} 个任务</span>
                    </div>

                    <div class="filter-bar">
                        <label class="filter-search">
                            <span class="sr-only">搜索 PITR 任务</span>
                            <ui-icon name="search" size="17"></ui-icon>
                            <input type="search" ng-model="$ctrl.filters.search" ng-change="$ctrl.filtersChanged()"
                                placeholder="搜索 Job ID、Server 或 State" autocomplete="off">
                            <button type="button" class="filter-search__clear" ng-if="$ctrl.filters.search"
                                ng-click="$ctrl.clearSearch()" aria-label="清空搜索">
                                <ui-icon name="close" size="14"></ui-icon>
                            </button>
                        </label>
                        <label class="filter-select">
                            <span class="sr-only">状态筛选</span>
                            <ui-icon class="filter-select__leading" name="filter" size="15"></ui-icon>
                            <select ng-model="$ctrl.filters.status" ng-change="$ctrl.filtersChanged()"
                                ng-options="option.value as option.label for option in $ctrl.statusOptions" aria-label="状态筛选">
                                <option value="">全部状态</option>
                            </select>
                            <ui-icon class="filter-select__trailing" name="chevron-down" size="13"></ui-icon>
                        </label>
                        <span class="filter-result-count" aria-live="polite">共 <strong>{{$ctrl.filteredRows.length}}</strong> 条结果</span>
                        <button type="button" class="button button-secondary filter-refresh" ng-click="$ctrl.refresh()"
                            ng-disabled="$ctrl.requestInFlight || $ctrl.submitting || $ctrl.actionSubmitting || $ctrl.confirming">
                            <ui-icon name="refresh" size="16" ng-class="{'is-spinning': $ctrl.refreshing}"></ui-icon>
                            <span>{{$ctrl.refreshing ? '刷新中' : '刷新'}}</span>
                        </button>
                    </div>

                    <div class="inline-alert inline-alert--error" ng-if="$ctrl.operationError" role="alert">
                        <ui-icon name="alert" size="18"></ui-icon>
                        <div><strong>任务操作未完成</strong><p>{{$ctrl.operationError}}</p></div>
                    </div>

                    <codis-data-table ng-if="$ctrl.filteredRows.length"
                        rows="$ctrl.filteredRows" columns="$ctrl.columns" loading="false" error=""
                        selected="$ctrl.selected" page="$ctrl.page" page-size="$ctrl.pageSize"
                        sort-key="$ctrl.sortKey" sort-direction="$ctrl.sortDirection"
                        selectable="false" row-key="id" empty-title="暂无 PITR 任务"
                        empty-description="当前集群还没有数据闪回任务。"
                        on-retry="$ctrl.retry()" on-open="$ctrl.openDetail(row)"
                        on-row-action="$ctrl.rowAction(row, action)"></codis-data-table>

                    <empty-state ng-if="!$ctrl.jobs.length" icon="history" title="还没有数据闪回任务"
                        description="新建任务前，请确认目标为当前集群的 Group Master，并准备好外部进程恢复机制。"
                        action-label="新建闪回任务" on-action="$ctrl.newTask()"></empty-state>
                    <empty-state ng-if="$ctrl.jobs.length && !$ctrl.filteredRows.length"
                        icon="search" title="没有匹配的任务" description="请调整 Job ID、Server、State 关键词或状态筛选条件。"
                        action-label="清除筛选" on-action="$ctrl.clearFilters()"></empty-state>
                </section>
            </div>

            <resource-drawer open="$ctrl.detailOpen" title="'PITR 任务详情'"
                subtitle="$ctrl.activeDetailId || '任务标识缺失'" show-submit="false" width="720"
                error="$ctrl.detailError" on-close="$ctrl.closeDetail()">
                <div ng-if="$ctrl.detailJob">
                    <section class="panel">
                        <div class="card-header">
                            <div>
                                <h2 class="card-title">标识与目标</h2>
                                <p>以下字段来自 Dashboard 返回的任务快照。</p>
                            </div>
                            <status-tag status="$ctrl.detailJob.stateDisplay"></status-tag>
                        </div>
                        <dl class="key-value-grid">
                            <div class="key-value-item" ng-repeat="field in $ctrl.detailFields track by field.key">
                                <dt>{{field.label}}</dt><dd ng-attr-title="{{field.value}}">{{field.value}}</dd>
                            </div>
                        </dl>
                        <div ng-if="$ctrl.detailJob.error">
                            <span class="form-label">任务错误</span>
                            <pre class="code-block" style="white-space: pre-wrap;">{{$ctrl.detailJob.error}}</pre>
                        </div>
                    </section>

                    <section class="panel">
                        <div class="card-header">
                            <div>
                                <h2 class="card-title">AOF 与 Manifest</h2>
                                <p>仅展示后端已记录的路径、segment 和快照信息。</p>
                            </div>
                        </div>
                        <dl class="key-value-grid">
                            <div class="key-value-item" ng-repeat="field in $ctrl.manifestFields track by field.key">
                                <dt>{{field.label}}</dt><dd ng-attr-title="{{field.value}}">{{field.value}}</dd>
                            </div>
                        </dl>
                    </section>

                    <section class="panel">
                        <div class="card-header">
                            <div>
                                <h2 class="card-title">执行步骤</h2>
                                <p>步骤名称、状态、起止时间与错误均直接来自 steps。</p>
                            </div>
                            <ui-icon name="refresh" size="16" class="is-spinning" ng-if="$ctrl.detailRefreshing"></ui-icon>
                        </div>
                        <div class="data-table-scroll" ng-if="$ctrl.detailJob.steps.length">
                            <table class="result-table">
                                <caption class="sr-only">PITR 执行步骤</caption>
                                <thead><tr><th>步骤</th><th>状态</th><th>开始时间</th><th>结束时间</th><th>错误 / 警告</th></tr></thead>
                                <tbody>
                                    <tr ng-repeat="step in $ctrl.detailJob.steps track by $index">
                                        <td>{{step.name || '—'}}</td>
                                        <td><status-tag status="step.status || '—'"></status-tag></td>
                                        <td>{{$ctrl.dateText(step.started_at, true)}}</td>
                                        <td>{{$ctrl.dateText(step.finished_at, true)}}</td>
                                        <td style="white-space: pre-wrap; overflow-wrap: anywhere;">{{step.error || '—'}}</td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>
                        <p class="form-hint" ng-if="!$ctrl.detailJob.steps.length">后端尚未返回执行步骤。</p>
                    </section>
                </div>
            </resource-drawer>

            <resource-drawer open="$ctrl.editorOpen" title="'新建 PITR 数据闪回任务'"
                subtitle="'目标必须是当前集群的 Group Master；提交前需要危险操作确认'"
                width="720" submitting="$ctrl.submitting"
                submit-disabled="$ctrl.submitting || $ctrl.confirming" submit-label="创建闪回任务"
                show-submit="true" error="$ctrl.drawerError"
                on-close="$ctrl.closeEditor()" on-submit="$ctrl.submitCreate()">
                <div class="form-grid">
                    <div class="form-group form-group--full">
                        <label class="form-label" for="pitr-master-options">从当前集群选择 Group Master（可选）</label>
                        <select id="pitr-master-options" class="form-control" ng-model="$ctrl.selectedMaster"
                            ng-options="master.address as master.label for master in $ctrl.masterOptions"
                            ng-change="$ctrl.useSelectedMaster()" ng-disabled="$ctrl.submitting">
                            <option value="">选择 Master 地址，或在下方手动输入</option>
                        </select>
                        <p class="form-hint" ng-if="!$ctrl.masterOptions.length">当前尚未取得 Group Master 地址，仍可手动输入。</p>
                    </div>

                    <div class="form-group form-group--full" ng-class="{'has-error': $ctrl.fieldErrors.serverAddr}">
                        <label class="form-label" for="pitr-server-addr">Server 地址 <span class="required">必填</span></label>
                        <input id="pitr-server-addr" class="form-control" type="text"
                            ng-model="$ctrl.editor.serverAddr" ng-change="$ctrl.editServerAddress()"
                            ng-disabled="$ctrl.submitting" placeholder="host:port" autocomplete="off" spellcheck="false" required>
                        <p class="form-hint">server_addr；后端会校验它是否属于当前 product，且必须为 Group Master。</p>
                        <p class="form-error" ng-if="$ctrl.fieldErrors.serverAddr" role="alert">{{$ctrl.fieldErrors.serverAddr}}</p>
                    </div>

                    <div class="form-group form-group--full" ng-class="{'has-error': $ctrl.fieldErrors.truncateTs}">
                        <label class="form-label" for="pitr-truncate-ts">截断时间点（Unix 秒） <span class="required">必填</span></label>
                        <input id="pitr-truncate-ts" class="form-control" type="text" inputmode="numeric"
                            ng-model="$ctrl.editor.truncateTs" ng-change="$ctrl.clearEditorError('truncateTs')"
                            ng-disabled="$ctrl.submitting" placeholder="例如 1716000000" autocomplete="off" required>
                        <p class="form-hint">truncate_ts 必须是非负安全整数；当前后端要求大于 0，因此会拒绝 0（1970-01-01 00:00:00 UTC）。</p>
                        <p class="form-hint">对应本地时间：{{$ctrl.draftLocalTime()}}</p>
                        <p class="form-error" ng-if="$ctrl.fieldErrors.truncateTs" role="alert">{{$ctrl.fieldErrors.truncateTs}}</p>
                    </div>
                </div>

                <details class="advanced-config" open>
                    <summary>高级说明与操作风险</summary>
                    <div class="inline-alert inline-alert--warning">
                        <ui-icon name="alert" size="18"></ui-icon>
                        <div>
                            <strong>该任务会停止目标 Master 并截断 AOF</strong>
                            <p>执行过程会发送 SHUTDOWN NOSAVE，备份 Manifest 与最后 segment，并调用 redis-check-aof 截断 AOF。Dashboard 不直接启动 Redis；必须确保外部进程管理器能够恢复实例（可配合已配置的 pitr_restart_command），随后由 Dashboard 等待加载并重同步副本。</p>
                        </div>
                    </div>
                    <p class="form-hint">目标实例需要预先启用 appendonly 与 aof-timestamp-enabled，且截断点必须位于最后一个 AOF segment 的时间范围内。取消已进入 shutdown 或 truncate 阶段的任务不会自动恢复服务。</p>
                </details>
            </resource-drawer>
        </div>
    `,
    controller: ['Api', 'Activity', 'ClusterStore', 'Confirm', 'Format', 'Preferences', 'Toast', '$scope', '$timeout',
        function (Api, Activity, ClusterStore, Confirm, Format, Preferences, Toast, $scope, $timeout) {
            var ctrl = this;
            var maxSafeInteger = Number.MAX_SAFE_INTEGER || 9007199254740991;
            var terminalStates = {succeeded: true, failed: true, cancelled: true};
            var baseStatusOptions = [
                {value: 'pending', label: '等待处理 · pending'},
                {value: 'validating', label: '校验目标 · validating'},
                {value: 'checking_prereq', label: '检查前置条件 · checking_prereq'},
                {value: 'checking_feasibility', label: '检查可执行性 · checking_feasibility'},
                {value: 'shutting_down', label: '停止实例 · shutting_down'},
                {value: 'snapshotting', label: '创建快照 · snapshotting'},
                {value: 'truncating', label: '截断 AOF · truncating'},
                {value: 'restarting', label: '等待实例恢复 · restarting'},
                {value: 'waiting_load', label: '等待数据加载 · waiting_load'},
                {value: 'resyncing_replicas', label: '重同步副本 · resyncing_replicas'},
                {value: 'succeeded', label: '成功 · succeeded'},
                {value: 'failed', label: '失败 · failed'},
                {value: 'cancelled', label: '已取消 · cancelled'}
            ];
            var destroyed = false;
            var activated = false;
            var loadSequence = 0;
            var detailSequence = 0;
            var mutationSequence = 0;
            var clusterSequence = 0;
            var requestVersion = 0;
            var detailVersion = 0;
            var mutationJobId = null;
            var pollTimer = null;
            var activeConfirmation = null;
            var preferenceKey = '';
            var listeners = [];

            ctrl.jobs = [];
            ctrl.filteredRows = [];
            ctrl.metrics = [];
            ctrl.selected = [];
            ctrl.filters = {search: '', status: ''};
            ctrl.statusOptions = angular.copy(baseStatusOptions);
            ctrl.page = 1;
            ctrl.pageSize = 10;
            ctrl.sortKey = 'created_at';
            ctrl.sortDirection = 'desc';
            ctrl.loaded = false;
            ctrl.initialLoading = true;
            ctrl.refreshing = false;
            ctrl.requestInFlight = false;
            ctrl.loadError = '';
            ctrl.refreshError = '';
            ctrl.operationError = '';
            ctrl.refreshFeedback = '';
            ctrl.forbidden = false;
            ctrl.lastUpdated = null;
            ctrl.masterOptions = [];
            ctrl.selectedMaster = '';
            ctrl.editor = newEditor();
            ctrl.editorOpen = false;
            ctrl.fieldErrors = {};
            ctrl.drawerError = '';
            ctrl.submitting = false;
            ctrl.actionSubmitting = false;
            ctrl.confirming = false;
            ctrl.activeMutation = '';
            ctrl.detailOpen = false;
            ctrl.detailJob = null;
            ctrl.detailFields = [];
            ctrl.manifestFields = [];
            ctrl.detailError = '';
            ctrl.detailRefreshing = false;
            ctrl.activeDetailId = '';
            ctrl.columns = buildColumns();
            ctrl.dateText = dateText;

            function hasValue(value) {
                return value !== undefined && value !== null;
            }

            function hasText(value) {
                return hasValue(value) && String(value).trim() !== '';
            }

            function display(value) {
                return hasValue(value) && value !== '' ? String(value) : '—';
            }

            function isTerminal(state) {
                return terminalStates[String(hasValue(state) ? state : '')] === true;
            }

            function isActive(state) {
                return hasText(state) && !isTerminal(state);
            }

            function dateText(value, withSeconds) {
                if (!hasValue(value) || value === '') {
                    return '—';
                }
                var parsed = angular.isDate(value) ? value : new Date(value);
                if (isNaN(parsed.getTime())) {
                    return String(value);
                }
                return Format.date(parsed, withSeconds !== false);
            }

            function unixSecondsDate(value) {
                if (!hasValue(value) || value === '' || isNaN(Number(value))) {
                    return '—';
                }
                var parsed = new Date(Number(value) * 1000);
                return isNaN(parsed.getTime()) ? '—' : Format.date(parsed, true);
            }

            function stateLabel(state) {
                for (var i = 0; i < ctrl.statusOptions.length; i++) {
                    if (ctrl.statusOptions[i].value === state) {
                        return ctrl.statusOptions[i].label;
                    }
                }
                return display(state);
            }

            function currentStep(job) {
                var steps = job && angular.isArray(job.steps) ? job.steps : [];
                var running = null;
                for (var i = 0; i < steps.length; i++) {
                    if (steps[i] && steps[i].status === 'running') {
                        running = steps[i];
                    }
                }
                return running || (steps.length ? steps[steps.length - 1] : null);
            }

            function stepText(job) {
                var step = currentStep(job);
                if (!step) {
                    return '—';
                }
                var name = display(step.name);
                var status = display(step.status);
                if (name === '—' && status === '—') {
                    return '—';
                }
                return name + ' / ' + status;
            }

            function normalizeJob(raw) {
                raw = raw || {};
                var job = angular.extend({}, raw);
                job.id = hasValue(raw.id) ? String(raw.id) : '';
                job.idDisplay = display(job.id);
                job.state = hasValue(raw.state) ? String(raw.state) : '';
                job.stateDisplay = display(job.state);
                job.steps = (angular.isArray(raw.steps) ? raw.steps : []).map(function (step) {
                    return angular.extend({}, step || {});
                });
                job.stepDisplay = stepText(job);
                var step = currentStep(job);
                job.errorDisplay = display(raw.error || (step && step.error));
                job.truncateLocalDisplay = unixSecondsDate(raw.truncate_ts);
                job.searchText = [job.id, display(raw.server_addr), job.state, stateLabel(job.state)]
                    .join(' ').toLowerCase();
                return job;
            }

            function rowActions(row) {
                var busy = ctrl.submitting || ctrl.actionSubmitting || ctrl.confirming;
                var id = row && row.id;
                return [
                    {name: 'detail', label: '详情', icon: 'eye', title: '查看任务详情'},
                    {name: 'cancel', label: '取消', icon: 'stop', title: isActive(row && row.state) ? '取消非终态任务' : '仅非终态任务可取消', danger: true,
                        disabled: function () { return busy || !id || !isActive(row.state); }},
                    {name: 'remove', label: '删除', icon: 'trash', title: isTerminal(row && row.state) ? '删除终态任务及其快照' : '仅终态任务可删除', danger: true,
                        disabled: function () { return busy || !id || !isTerminal(row.state); }}
                ];
            }

            function buildColumns() {
                return [
                    {key: 'idDisplay', label: 'Job ID', type: 'link', sortable: true, width: 250},
                    {key: 'server_addr', label: 'Server', sortable: true, width: 165},
                    {key: 'stateDisplay', label: 'State', type: 'status', sortable: true, width: 145},
                    {key: 'stepDisplay', label: '当前 / 最近步骤', sortable: true, width: 165},
                    {key: 'truncate_ts', label: 'Truncate TS（Unix 秒）', type: 'number', sortable: true, width: 165},
                    {key: 'errorDisplay', label: 'Error', sortable: true, width: 220},
                    {key: 'created_at', label: '创建时间 · created_at', type: 'date', sortable: true, width: 155},
                    {key: 'actions', label: '操作', type: 'actions', width: 225, actions: rowActions}
                ];
            }

            function updateMetrics() {
                var running = 0;
                var succeeded = 0;
                var unsuccessful = 0;
                ctrl.jobs.forEach(function (job) {
                    if (isActive(job.state)) {
                        running++;
                    } else if (job.state === 'succeeded') {
                        succeeded++;
                    } else if (job.state === 'failed' || job.state === 'cancelled') {
                        unsuccessful++;
                    }
                });
                ctrl.metrics = [
                    {key: 'total', title: '任务总数', value: ctrl.jobs.length, caption: '当前 product 的全部 PITR 任务', icon: 'history'},
                    {key: 'running', title: '运行中', value: running, caption: '所有非终态任务', icon: 'activity', theme: running ? 'primary' : 'neutral'},
                    {key: 'succeeded', title: '成功', value: succeeded, caption: 'state = succeeded', icon: 'check'},
                    {key: 'unsuccessful', title: '失败或取消', value: unsuccessful, caption: 'failed + cancelled', icon: 'alert', status: unsuccessful ? 'WARNING' : ''}
                ];
            }

            function updateStatusOptions() {
                var options = angular.copy(baseStatusOptions);
                var known = {};
                options.forEach(function (option) { known[option.value] = true; });
                ctrl.jobs.forEach(function (job) {
                    if (hasText(job.state) && !known[job.state]) {
                        known[job.state] = true;
                        options.push({value: job.state, label: job.state});
                    }
                });
                ctrl.statusOptions = options;
            }

            function applyFilters(resetPage) {
                var search = String(ctrl.filters.search || '').trim().toLowerCase();
                ctrl.filteredRows = ctrl.jobs.filter(function (job) {
                    return (!search || job.searchText.indexOf(search) >= 0) &&
                        (!ctrl.filters.status || job.state === ctrl.filters.status);
                });
                if (resetPage) {
                    ctrl.page = 1;
                }
            }

            ctrl.filtersChanged = function () {
                applyFilters(true);
                savePreferences();
            };

            ctrl.clearSearch = function () {
                ctrl.filters.search = '';
                ctrl.filtersChanged();
            };

            ctrl.clearFilters = function () {
                ctrl.filters = {search: '', status: ''};
                applyFilters(true);
                savePreferences();
            };

            function validSavedStatus(status) {
                if (!status) {
                    return true;
                }
                for (var i = 0; i < baseStatusOptions.length; i++) {
                    if (baseStatusOptions[i].value === status) {
                        return true;
                    }
                }
                return false;
            }

            function loadPreferences() {
                preferenceKey = ctrl.product ? 'recovery.' + ctrl.product : '';
                var saved = Preferences.get(preferenceKey || 'recovery.default', {});
                saved = angular.isObject(saved) && !angular.isArray(saved) ? saved : {};
                var filters = angular.isObject(saved.filters) && !angular.isArray(saved.filters) ? saved.filters : {};
                var status = hasValue(filters.status) ? String(filters.status) : '';
                ctrl.filters = {
                    search: hasValue(filters.search) ? String(filters.search) : '',
                    status: validSavedStatus(status) ? status : ''
                };
                var page = Number(saved.page);
                ctrl.page = isFinite(page) && page > 0 ? Math.floor(page) : 1;
                var allowedSortKeys = ['idDisplay', 'server_addr', 'stateDisplay', 'stepDisplay', 'truncate_ts', 'errorDisplay', 'created_at'];
                ctrl.sortKey = allowedSortKeys.indexOf(saved.sortKey) >= 0 ? saved.sortKey : 'created_at';
                ctrl.sortDirection = hasValue(saved.sortDirection) ?
                    (saved.sortDirection === 'desc' ? 'desc' : 'asc') : 'desc';
                var pageSize = Number(saved.pageSize);
                ctrl.pageSize = [10, 25, 50].indexOf(pageSize) >= 0 ? pageSize : 10;
            }

            function savePreferences() {
                if (!preferenceKey || !ctrl.product) {
                    return;
                }
                Preferences.set(preferenceKey, {
                    filters: angular.copy(ctrl.filters),
                    page: ctrl.page,
                    pageSize: ctrl.pageSize,
                    sortKey: ctrl.sortKey,
                    sortDirection: ctrl.sortDirection
                });
            }

            function newEditor() {
                return {serverAddr: '', truncateTs: ''};
            }

            function parseTimestamp(value) {
                var text = hasValue(value) ? String(value).trim() : '';
                if (!/^\d+$/.test(text)) {
                    return null;
                }
                var parsed = Number(text);
                if (!isFinite(parsed) || Math.floor(parsed) !== parsed || parsed < 0 || parsed > maxSafeInteger) {
                    return null;
                }
                return parsed;
            }

            ctrl.draftLocalTime = function () {
                var value = parseTimestamp(ctrl.editor && ctrl.editor.truncateTs);
                return value === null ? '—' : unixSecondsDate(value);
            };

            function updateMasterOptions(state) {
                if (!state || state.product !== ctrl.product) {
                    return;
                }
                var options = [];
                var seen = {};
                (state.groups || []).forEach(function (group) {
                    var servers = group && angular.isArray(group.servers) ? group.servers : [];
                    var master = servers.length ? servers[0] : null;
                    var address = master && hasText(master.server) ? String(master.server).trim() : '';
                    if (!address || seen[address]) {
                        return;
                    }
                    seen[address] = true;
                    options.push({
                        address: address,
                        label: (group.name || ('Group-' + display(group.id))) + ' · ' + address
                    });
                });
                ctrl.masterOptions = options;
                if (ctrl.selectedMaster && !seen[ctrl.selectedMaster]) {
                    ctrl.selectedMaster = '';
                }
            }

            function loadCluster(product, force) {
                if (!product) {
                    return;
                }
                var sequence = ++clusterSequence;
                var request = force ? ClusterStore.refresh(product) : ClusterStore.load(product);
                request.then(function (state) {
                    if (!destroyed && sequence === clusterSequence && product === ctrl.product) {
                        updateMasterOptions(state);
                    }
                }).catch(angular.noop);
            }

            function stopPolling() {
                if (pollTimer !== null) {
                    $timeout.cancel(pollTimer);
                    pollTimer = null;
                }
            }

            function busy() {
                return ctrl.submitting || ctrl.actionSubmitting || ctrl.confirming;
            }

            function hasActiveJobs() {
                return ctrl.jobs.some(function (job) { return isActive(job.state); });
            }

            function scheduleNextPoll() {
                stopPolling();
                if (destroyed || !ctrl.loaded || ctrl.requestInFlight || busy() || ctrl.forbidden || !hasActiveJobs()) {
                    return;
                }
                pollTimer = $timeout(function () {
                    pollTimer = null;
                    if (!destroyed && !busy()) {
                        loadJobs({background: true, poll: true});
                    }
                }, 2000);
            }

            function currentLoad(product, sequence) {
                return !destroyed && product === ctrl.product && sequence === loadSequence;
            }

            function refreshDoneText(options) {
                var time = Format.time(new Date());
                if (options.afterMutation) {
                    return '操作后刷新完成 · ' + time;
                }
                if (options.manual) {
                    return '手动刷新完成 · ' + time;
                }
                if (options.shell) {
                    return 'Shell 刷新完成 · ' + time;
                }
                if (options.poll) {
                    return '自动刷新完成 · ' + time;
                }
                return '任务加载完成 · ' + time;
            }

            function updateOpenDetailFromList(version) {
                if (!ctrl.detailOpen || !ctrl.activeDetailId || version <= detailVersion) {
                    return;
                }
                var found = findJob(ctrl.activeDetailId);
                if (!found) {
                    return;
                }
                ctrl.detailJob = normalizeJob(found);
                detailVersion = version;
                ctrl.detailError = '';
                buildDetailFields();
            }

            function loadJobs(options) {
                options = options || {};
                var product = ctrl.product;
                if (!product) {
                    ctrl.initialLoading = false;
                    ctrl.refreshing = false;
                    ctrl.requestInFlight = false;
                    ctrl.loadError = '请先选择集群';
                    return;
                }
                stopPolling();
                var sequence = ++loadSequence;
                var version = ++requestVersion;
                var hadData = ctrl.loaded;
                ctrl.requestInFlight = true;
                ctrl.initialLoading = !hadData;
                ctrl.refreshing = hadData;
                ctrl.refreshError = '';
                ctrl.forbidden = false;
                if (!hadData) {
                    ctrl.loadError = '';
                    ctrl.refreshFeedback = '';
                } else if (options.manual) {
                    ctrl.refreshFeedback = '正在手动刷新…';
                } else if (options.shell) {
                    ctrl.refreshFeedback = '正在响应 Shell 刷新…';
                } else if (!options.poll) {
                    ctrl.refreshFeedback = '正在后台刷新…';
                }

                return Api.request('GET', product, '/api/topom/pitr/jobs/{xauth}').then(function (data) {
                    if (!currentLoad(product, sequence)) {
                        return;
                    }
                    ctrl.jobs = (angular.isArray(data) ? data : []).map(normalizeJob);
                    ctrl.loaded = true;
                    ctrl.lastUpdated = new Date();
                    ctrl.loadError = '';
                    ctrl.refreshError = '';
                    ctrl.forbidden = false;
                    ctrl.refreshFeedback = refreshDoneText(options);
                    updateStatusOptions();
                    updateMetrics();
                    applyFilters(false);
                    updateOpenDetailFromList(version);
                    if (options.manual) {
                        Toast.success('PITR 任务列表已刷新');
                    }
                }).catch(function (response) {
                    if (!currentLoad(product, sequence)) {
                        return;
                    }
                    var message = Api.errorMessage(response);
                    ctrl.forbidden = Api.isForbidden(response);
                    if (ctrl.loaded) {
                        ctrl.refreshError = message;
                        ctrl.refreshFeedback = (options.poll ? '自动' : (options.shell ? 'Shell' : '后台')) + '刷新失败，已保留原任务数据';
                        if (options.manual) {
                            ctrl.refreshFeedback = '手动刷新失败，已保留原任务数据';
                            Toast.error(message);
                        }
                    } else {
                        ctrl.loadError = message;
                        ctrl.refreshFeedback = '';
                    }
                }).finally(function () {
                    if (!currentLoad(product, sequence)) {
                        return;
                    }
                    ctrl.requestInFlight = false;
                    ctrl.initialLoading = false;
                    ctrl.refreshing = false;
                    scheduleNextPoll();
                });
            }

            ctrl.refresh = function () {
                if (ctrl.requestInFlight || busy() || destroyed) {
                    return;
                }
                var request = loadJobs({manual: true, background: ctrl.loaded});
                if (ctrl.detailOpen && ctrl.activeDetailId) {
                    loadDetail(ctrl.activeDetailId);
                }
                return request;
            };

            ctrl.retry = function () {
                if (ctrl.requestInFlight || destroyed) {
                    return;
                }
                return loadJobs({manual: true, background: ctrl.loaded});
            };

            function currentDetail(product, sequence, id) {
                return !destroyed && product === ctrl.product && sequence === detailSequence &&
                    ctrl.detailOpen && ctrl.activeDetailId === id;
            }

            function loadDetail(id) {
                var product = ctrl.product;
                if (!product || !id) {
                    return;
                }
                var sequence = ++detailSequence;
                var version = ++requestVersion;
                ctrl.detailRefreshing = true;
                ctrl.detailError = '';
                return Api.request('GET', product, '/api/topom/pitr/{xauth}/' + Api.segment(id)).then(function (data) {
                    if (!currentDetail(product, sequence, id) || version < detailVersion) {
                        return;
                    }
                    ctrl.detailJob = normalizeJob(data);
                    detailVersion = version;
                    ctrl.detailError = '';
                    buildDetailFields();
                }).catch(function (response) {
                    if (currentDetail(product, sequence, id) && version >= detailVersion) {
                        ctrl.detailError = Api.errorMessage(response);
                    }
                }).finally(function () {
                    if (currentDetail(product, sequence, id)) {
                        ctrl.detailRefreshing = false;
                    }
                });
            }

            function segmentRangeText(job) {
                var range = job && job.last_segment_ts_range;
                if (!angular.isArray(range) || range.length < 2) {
                    return '—';
                }
                return display(range[0]) + ' — ' + display(range[1]);
            }

            function buildDetailFields() {
                var job = ctrl.detailJob || {};
                ctrl.detailFields = [
                    {key: 'id', label: 'Job ID · id', value: display(job.id)},
                    {key: 'product', label: 'Product · product_name', value: display(job.product_name)},
                    {key: 'group', label: 'Group · group_id', value: display(job.group_id)},
                    {key: 'server', label: 'Server · server_addr', value: display(job.server_addr)},
                    {key: 'truncate', label: '截断时间点 · truncate_ts', value: display(job.truncate_ts)},
                    {key: 'truncate-local', label: '截断点本地时间', value: unixSecondsDate(job.truncate_ts)},
                    {key: 'state', label: '状态 · state', value: display(job.state)},
                    {key: 'step', label: '当前 / 最近步骤', value: stepText(job)},
                    {key: 'created', label: '创建时间 · created_at', value: dateText(job.created_at, true)},
                    {key: 'updated', label: '更新时间 · updated_at', value: dateText(job.updated_at, true)},
                    {key: 'error', label: '任务错误 · error', value: display(job.error)}
                ];
                ctrl.manifestFields = [
                    {key: 'aof-dir', label: 'AOF 目录 · aof_dir', value: display(job.aof_dir)},
                    {key: 'manifest', label: 'Manifest · aof_manifest', value: display(job.aof_manifest)},
                    {key: 'segment', label: '最后 Segment · last_segment_file', value: display(job.last_segment_file)},
                    {key: 'range', label: 'Segment TS 范围 · last_segment_ts_range', value: segmentRangeText(job)},
                    {key: 'snapshot', label: '快照目录 · snapshot_dir', value: display(job.snapshot_dir)},
                    {key: 'restart', label: '已触发恢复命令 · restart_kicked', value: display(job.restart_kicked)}
                ];
            }

            ctrl.openDetail = function (row) {
                if (!row) {
                    return;
                }
                detailSequence++;
                ctrl.activeDetailId = row.id || '';
                detailVersion = ++requestVersion;
                ctrl.detailJob = normalizeJob(row);
                ctrl.detailError = '';
                ctrl.detailRefreshing = false;
                ctrl.detailOpen = true;
                buildDetailFields();
                if (ctrl.activeDetailId) {
                    loadDetail(ctrl.activeDetailId);
                }
            };

            ctrl.closeDetail = function () {
                detailSequence++;
                detailVersion = ++requestVersion;
                ctrl.detailOpen = false;
                ctrl.activeDetailId = '';
                ctrl.detailJob = null;
                ctrl.detailFields = [];
                ctrl.manifestFields = [];
                ctrl.detailError = '';
                ctrl.detailRefreshing = false;
            };

            ctrl.newTask = function () {
                if (busy() || destroyed) {
                    return;
                }
                ctrl.editor = newEditor();
                ctrl.selectedMaster = '';
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                ctrl.editorOpen = true;
            };

            ctrl.closeEditor = function () {
                if (ctrl.submitting) {
                    return;
                }
                ctrl.editorOpen = false;
                ctrl.selectedMaster = '';
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
            };

            ctrl.useSelectedMaster = function () {
                if (!ctrl.selectedMaster) {
                    return;
                }
                ctrl.editor.serverAddr = ctrl.selectedMaster;
                ctrl.clearEditorError('serverAddr');
            };

            ctrl.editServerAddress = function () {
                if (ctrl.selectedMaster && ctrl.editor.serverAddr !== ctrl.selectedMaster) {
                    ctrl.selectedMaster = '';
                }
                ctrl.clearEditorError('serverAddr');
            };

            ctrl.clearEditorError = function (field) {
                if (ctrl.submitting) {
                    return;
                }
                ctrl.fieldErrors[field] = '';
                ctrl.drawerError = '';
            };

            function cancelActiveConfirmation() {
                if (activeConfirmation && Confirm.state.current === activeConfirmation) {
                    Confirm.cancel('destroyed');
                }
                activeConfirmation = null;
                ctrl.confirming = false;
            }

            function confirmOperation(options, confirmed) {
                if (ctrl.confirming || destroyed) {
                    return;
                }
                var product = ctrl.product;
                if (!product) {
                    return;
                }
                ctrl.confirming = true;
                stopPolling();
                var promise = Confirm.open({
                    title: options.title,
                    message: options.message,
                    details: options.details,
                    confirmText: options.confirmText,
                    danger: options.danger !== false
                });
                activeConfirmation = Confirm.state.current;
                return promise.then(function () {
                    if (destroyed || product !== ctrl.product) {
                        return;
                    }
                    ctrl.confirming = false;
                    activeConfirmation = null;
                    return confirmed(product);
                }, function () {
                    if (!destroyed && product === ctrl.product) {
                        ctrl.confirming = false;
                        activeConfirmation = null;
                        scheduleNextPoll();
                    }
                });
            }

            function currentMutation(product, sequence, jobId) {
                return !destroyed && product === ctrl.product && sequence === mutationSequence && mutationJobId === jobId;
            }

            function validateEditor() {
                var serverAddr = String(ctrl.editor.serverAddr || '').trim();
                var timestamp = parseTimestamp(ctrl.editor.truncateTs);
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                if (!serverAddr) {
                    ctrl.fieldErrors.serverAddr = '请输入 server_addr，或从当前集群选择 Group Master';
                }
                if (timestamp === null) {
                    ctrl.fieldErrors.truncateTs = '请输入 0 到 ' + Format.number(maxSafeInteger) + ' 之间的非负安全整数（Unix 秒）';
                } else if (timestamp === 0) {
                    ctrl.fieldErrors.truncateTs = '当前后端明确拒绝 truncate_ts = 0，请输入大于 0 的 Unix 秒时间戳';
                }
                if (ctrl.fieldErrors.serverAddr || ctrl.fieldErrors.truncateTs) {
                    ctrl.drawerError = '请修正表单中的错误后重试';
                    return null;
                }
                return {serverAddr: serverAddr, truncateTs: timestamp};
            }

            ctrl.submitCreate = function () {
                if (busy() || destroyed || !ctrl.editor) {
                    return;
                }
                var value = validateEditor();
                if (!value) {
                    return;
                }
                var localTime = unixSecondsDate(value.truncateTs);
                return confirmOperation({
                    title: '确认创建 PITR 数据闪回任务？',
                    message: '此危险操作会向目标 Master 发送 SHUTDOWN NOSAVE 并截断 AOF。必须已由外部进程管理器接管实例恢复；取消或失败也可能需要人工检查服务与快照。',
                    details: [
                        'server_addr: ' + value.serverAddr,
                        'truncate_ts: ' + value.truncateTs,
                        '本地时间: ' + localTime,
                        '将执行 SHUTDOWN、AOF 截断，并等待外部恢复与副本重同步。'
                    ],
                    confirmText: '确认 SHUTDOWN 并创建',
                    danger: true
                }, function (product) {
                    var sequence = ++mutationSequence;
                    mutationJobId = '';
                    ctrl.submitting = true;
                    ctrl.activeMutation = 'create';
                    ctrl.drawerError = '';
                    stopPolling();
                    return Api.request('PUT', product, '/api/topom/pitr/create/{xauth}', {
                        server_addr: value.serverAddr,
                        truncate_ts: Number(value.truncateTs)
                    }).then(function (data) {
                        if (!currentMutation(product, sequence, '')) {
                            return;
                        }
                        ctrl.editorOpen = false;
                        ctrl.editor = newEditor();
                        ctrl.selectedMaster = '';
                        ctrl.fieldErrors = {};
                        ctrl.drawerError = '';
                        var id = data && data.job_id;
                        Activity.add(product, '创建 PITR 数据闪回任务', value.serverAddr + ' · ' + value.truncateTs, 'success');
                        Toast.success(id ? 'PITR 任务 ' + id + ' 已创建，正在刷新' : 'PITR 数据闪回任务已创建，正在刷新');
                        loadJobs({background: true, afterMutation: true});
                    }).catch(function (response) {
                        if (!currentMutation(product, sequence, '')) {
                            return;
                        }
                        var message = Api.errorMessage(response);
                        ctrl.drawerError = message;
                        Toast.error(message);
                    }).finally(function () {
                        if (!currentMutation(product, sequence, '')) {
                            return;
                        }
                        ctrl.submitting = false;
                        ctrl.activeMutation = '';
                        scheduleNextPoll();
                    });
                });
            };

            function findJob(id) {
                var wanted = String(id);
                for (var i = 0; i < ctrl.jobs.length; i++) {
                    if (String(ctrl.jobs[i].id) === wanted) {
                        return ctrl.jobs[i];
                    }
                }
                return null;
            }

            ctrl.rowAction = function (row, action) {
                var name = angular.isString(action) ? action : action && (action.name || action.action || action.id);
                if (name === 'detail') {
                    ctrl.openDetail(row);
                } else if (name === 'cancel') {
                    ctrl.cancelJob(row);
                } else if (name === 'remove') {
                    ctrl.removeJob(row);
                }
            };

            function mutateJob(kind, row) {
                if (busy() || destroyed || !row || !row.id) {
                    return;
                }
                var id = String(row.id);
                if (kind === 'cancel' && !isActive(row.state)) {
                    Toast.info('仅非终态 PITR 任务可以取消');
                    return;
                }
                if (kind === 'remove' && !isTerminal(row.state)) {
                    Toast.info('仅 succeeded、failed 或 cancelled 任务可以删除');
                    return;
                }
                var cancelling = kind === 'cancel';
                return confirmOperation({
                    title: cancelling ? '确认取消 PITR 任务？' : '确认删除 PITR 任务？',
                    message: cancelling ?
                        '取消不会自动恢复已经 SHUTDOWN 的实例。若任务已进入截断阶段，请根据步骤错误和快照目录检查 AOF 与服务状态。' :
                        '删除会移除该终态任务记录，并由后端清理其快照目录、释放该 Server 的 PITR 锁。此操作不可撤销。',
                    details: [
                        'job_id: ' + id,
                        'server_addr: ' + display(row.server_addr),
                        'state: ' + display(row.state)
                    ],
                    confirmText: cancelling ? '确认取消任务' : '确认删除任务',
                    danger: true
                }, function (product) {
                    var latest = findJob(id);
                    if (!latest) {
                        Toast.info('该任务已不在当前列表中，正在刷新');
                        loadJobs({background: true});
                        return;
                    }
                    if (cancelling && !isActive(latest.state)) {
                        Toast.info('任务已进入终态，无需取消');
                        loadJobs({background: true});
                        return;
                    }
                    if (!cancelling && !isTerminal(latest.state)) {
                        Toast.info('任务尚未进入终态，不能删除');
                        loadJobs({background: true});
                        return;
                    }
                    var sequence = ++mutationSequence;
                    mutationJobId = id;
                    ctrl.actionSubmitting = true;
                    ctrl.activeMutation = kind;
                    ctrl.operationError = '';
                    stopPolling();
                    return Api.request('PUT', product, '/api/topom/pitr/' + kind + '/{xauth}/' + Api.segment(id)).then(function () {
                        if (!currentMutation(product, sequence, id)) {
                            return;
                        }
                        Activity.add(product, cancelling ? '取消 PITR 数据闪回任务' : '删除 PITR 数据闪回任务',
                            id + ' · ' + display(latest.server_addr), 'success');
                        Toast.success(cancelling ? 'PITR 任务已请求取消，正在刷新' : 'PITR 任务已删除，正在刷新');
                        if (!cancelling && ctrl.detailOpen && ctrl.activeDetailId === id) {
                            ctrl.closeDetail();
                        }
                        loadJobs({background: true, afterMutation: true});
                    }).catch(function (response) {
                        if (!currentMutation(product, sequence, id)) {
                            return;
                        }
                        var message = Api.errorMessage(response);
                        ctrl.operationError = message;
                        Toast.error(message);
                    }).finally(function () {
                        if (!currentMutation(product, sequence, id)) {
                            return;
                        }
                        ctrl.actionSubmitting = false;
                        ctrl.activeMutation = '';
                        scheduleNextPoll();
                    });
                });
            }

            ctrl.cancelJob = function (row) {
                return mutateJob('cancel', row);
            };

            ctrl.removeJob = function (row) {
                return mutateJob('remove', row);
            };

            function resetForProduct() {
                loadSequence++;
                detailSequence++;
                mutationSequence++;
                clusterSequence++;
                requestVersion++;
                detailVersion = requestVersion;
                mutationJobId = null;
                stopPolling();
                cancelActiveConfirmation();
                ctrl.jobs = [];
                ctrl.filteredRows = [];
                ctrl.metrics = [];
                ctrl.loaded = false;
                ctrl.initialLoading = true;
                ctrl.refreshing = false;
                ctrl.requestInFlight = false;
                ctrl.loadError = '';
                ctrl.refreshError = '';
                ctrl.operationError = '';
                ctrl.refreshFeedback = '';
                ctrl.forbidden = false;
                ctrl.lastUpdated = null;
                ctrl.masterOptions = [];
                ctrl.selectedMaster = '';
                ctrl.editor = newEditor();
                ctrl.editorOpen = false;
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                ctrl.submitting = false;
                ctrl.actionSubmitting = false;
                ctrl.confirming = false;
                ctrl.activeMutation = '';
                ctrl.detailOpen = false;
                ctrl.detailJob = null;
                ctrl.detailFields = [];
                ctrl.manifestFields = [];
                ctrl.detailError = '';
                ctrl.detailRefreshing = false;
                ctrl.activeDetailId = '';
                ctrl.statusOptions = angular.copy(baseStatusOptions);
                ctrl.filters = {search: '', status: ''};
                ctrl.page = 1;
                ctrl.pageSize = 10;
                ctrl.sortKey = 'created_at';
                ctrl.sortDirection = 'desc';
            }

            function activate() {
                resetForProduct();
                loadPreferences();
                updateMetrics();
                ctrl.cluster = ClusterStore.state(ctrl.product || 'default');
                updateMasterOptions(ctrl.cluster);
                if (ctrl.product) {
                    loadCluster(ctrl.product, false);
                    loadJobs();
                } else {
                    ctrl.initialLoading = false;
                    ctrl.loadError = '请先选择集群';
                }
            }

            ctrl.$onInit = function () {
                activated = true;
                activate();
                listeners.push($scope.$watch(function () { return ctrl.page; }, savePreferences));
                listeners.push($scope.$watch(function () { return ctrl.pageSize; }, savePreferences));
                listeners.push($scope.$watch(function () { return ctrl.sortKey; }, savePreferences));
                listeners.push($scope.$watch(function () { return ctrl.sortDirection; }, savePreferences));
            };

            ctrl.$onChanges = function (changes) {
                if (activated && changes.product && !changes.product.isFirstChange() &&
                    changes.product.currentValue !== changes.product.previousValue) {
                    activate();
                }
            };

            ctrl.$onDestroy = function () {
                destroyed = true;
                loadSequence++;
                detailSequence++;
                mutationSequence++;
                clusterSequence++;
                requestVersion++;
                mutationJobId = null;
                stopPolling();
                cancelActiveConfirmation();
                listeners.forEach(function (unsubscribe) { unsubscribe(); });
                listeners = [];
            };

            listeners.push($scope.$on('cluster:updated', function (event, payload) {
                if (!destroyed && payload && payload.product === ctrl.product) {
                    ctrl.cluster = payload.state;
                    updateMasterOptions(payload.state);
                }
            }));

            listeners.push($scope.$on('cluster:refreshRequested', function (event, payload) {
                if (!destroyed && payload && payload.product === ctrl.product && !busy()) {
                    loadCluster(ctrl.product, true);
                    if (!ctrl.requestInFlight) {
                        loadJobs({background: ctrl.loaded, shell: true});
                    }
                    if (ctrl.detailOpen && ctrl.activeDetailId) {
                        loadDetail(ctrl.activeDetailId);
                    }
                }
            }));
        }
    ]
});
