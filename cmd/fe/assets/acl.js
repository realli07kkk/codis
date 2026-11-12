'use strict';

dashboard.component('securityPage', {
    bindings: {
        product: '<'
    },
    template: `
        <div class="security-page">
            <page-header title="安全与限流"
                description="集中管理 Redis 访问控制、用户规则与每个 Proxy 的 QPS 上限。">
                <status-tag status="$ctrl.acl.syncTone()" label="{{$ctrl.acl.syncLabel()}}"></status-tag>
            </page-header>

            <div class="tabs" role="tablist" aria-label="安全与限流功能">
                <button id="security-tab-acl" class="tab-button" type="button" role="tab"
                    ng-class="{active: $ctrl.activeTab === 'acl'}"
                    ng-click="$ctrl.setTab('acl')" aria-controls="security-panel-acl"
                    ng-attr-aria-selected="{{$ctrl.activeTab === 'acl'}}">
                    <ui-icon name="lock" size="16"></ui-icon>访问控制
                </button>
                <button id="security-tab-qps" class="tab-button" type="button" role="tab"
                    ng-class="{active: $ctrl.activeTab === 'qps'}"
                    ng-click="$ctrl.setTab('qps')" aria-controls="security-panel-qps"
                    ng-attr-aria-selected="{{$ctrl.activeTab === 'qps'}}">
                    <ui-icon name="activity" size="16"></ui-icon>Proxy 限流
                </button>
            </div>

            <section id="security-panel-acl" role="tabpanel" aria-labelledby="security-tab-acl"
                ng-show="$ctrl.activeTab === 'acl'">
                <loading-skeleton ng-if="$ctrl.acl.initialLoading" variant="page"></loading-skeleton>

                <empty-state ng-if="!$ctrl.acl.initialLoading && !$ctrl.acl.loaded && $ctrl.acl.loadError"
                    icon="alert" title="无法获取访问控制配置" description="{{$ctrl.acl.loadError}}"
                    action-label="重新加载" on-action="$ctrl.acl.retry()"></empty-state>

                <div ng-if="$ctrl.acl.loaded" class="acl-content">
                    <div class="inline-alert inline-alert--error" ng-if="$ctrl.acl.refreshError" role="alert">
                        <ui-icon name="alert" size="18"></ui-icon>
                        <div>
                            <strong>刷新失败，当前展示上次成功加载的数据</strong>
                            <p>{{$ctrl.acl.refreshError}}</p>
                            <button class="button button-secondary" type="button" ng-click="$ctrl.acl.retry()">重试</button>
                        </div>
                    </div>
                    <div class="inline-alert inline-alert--error" ng-if="$ctrl.acl.operationError" role="alert">
                        <ui-icon name="alert" size="18"></ui-icon>
                        <div><strong>操作未完成</strong><p>{{$ctrl.acl.operationError}}</p></div>
                    </div>

                    <div class="panel">
                        <div class="card-header">
                            <div>
                                <h2 class="card-title">访问控制</h2>
                                <p>所有修改都会整量提交用户列表；未修改用户的既有密码由 Dashboard 保留。</p>
                            </div>
                            <div class="card-actions">
                                <button class="button button-secondary" type="button"
                                    ng-click="$ctrl.acl.refresh()" ng-disabled="$ctrl.acl.refreshing || $ctrl.acl.submitting">
                                    <ui-icon name="refresh" size="16" ng-class="{'is-spinning': $ctrl.acl.refreshing}"></ui-icon>
                                    {{$ctrl.acl.refreshing ? '刷新中' : '刷新'}}
                                </button>
                                <button class="button button-primary" type="button"
                                    ng-click="$ctrl.acl.newUser()" ng-disabled="$ctrl.acl.submitting || $ctrl.acl.confirming">
                                    <ui-icon name="plus" size="16"></ui-icon>新增用户
                                </button>
                            </div>
                        </div>

                        <div class="metrics-grid">
                            <metric-card ng-repeat="metric in $ctrl.acl.metrics track by metric.key" metric="metric"></metric-card>
                        </div>

                        <div class="card card-header acl-global-settings">
                            <div>
                                <span class="form-label">全局访问控制</span>
                                <p class="form-hint">切换 enabled 会影响整个集群，并同步全部用户配置。</p>
                            </div>
                            <div class="card-actions">
                                <status-tag status="$ctrl.acl.enabledTone()" label="{{$ctrl.acl.enabledLabel()}}"></status-tag>
                                <button class="button" ng-class="$ctrl.acl.model.enabled ? 'button-danger' : 'button-primary'"
                                    type="button" ng-click="$ctrl.acl.toggleGlobal()"
                                    ng-disabled="$ctrl.acl.submitting || $ctrl.acl.confirming || $ctrl.acl.refreshing">
                                    {{$ctrl.acl.model.enabled ? '停用访问控制' : '启用访问控制'}}
                                </button>
                            </div>
                        </div>
                        <p class="form-hint" ng-if="$ctrl.acl.refreshFeedback" aria-live="polite">
                            {{$ctrl.acl.refreshFeedback}}
                        </p>
                    </div>

                    <div class="panel acl-users-panel">
                        <div class="card-header">
                            <div>
                                <h2 class="card-title">ACL 用户</h2>
                                <p>密码内容不可读取，列表仅展示已保存的密码数量。</p>
                            </div>
                            <span class="form-hint">共 {{$ctrl.acl.users.length}} 个用户</span>
                        </div>

                        <filter-bar filters="$ctrl.acl.filters" status-options="$ctrl.acl.statusOptions"
                            type-options="$ctrl.acl.typeOptions" result-count="$ctrl.acl.filteredRows.length"
                            refreshing="$ctrl.acl.refreshing || $ctrl.acl.submitting" search-placeholder="搜索用户名或 ACL 规则"
                            on-change="$ctrl.acl.applyFilters()" on-refresh="$ctrl.acl.refresh()"></filter-bar>

                        <codis-data-table ng-if="$ctrl.acl.filteredRows.length"
                            rows="$ctrl.acl.filteredRows" columns="$ctrl.acl.columns"
                            loading="$ctrl.acl.initialLoading" error="$ctrl.acl.loadError" selected="$ctrl.acl.selected"
                            page="$ctrl.acl.page" page-size="$ctrl.acl.pageSize"
                            sort-key="$ctrl.acl.sortKey" sort-direction="$ctrl.acl.sortDirection"
                            selectable="false" row-key="id" on-retry="$ctrl.acl.retry()"
                            on-open="$ctrl.acl.editUser(row)" on-row-action="$ctrl.acl.rowAction(row, action)"></codis-data-table>

                        <empty-state ng-if="!$ctrl.acl.users.length" icon="lock" title="还没有 ACL 用户"
                            description="新增用户并配置至少一条规则后，即可通过访问控制管理连接权限。"
                            action-label="新增用户" on-action="$ctrl.acl.newUser()"></empty-state>
                        <empty-state ng-if="$ctrl.acl.users.length && !$ctrl.acl.filteredRows.length"
                            icon="search" title="没有匹配的用户"
                            description="请调整用户名关键词或状态筛选条件。"
                            action-label="清除筛选" on-action="$ctrl.acl.clearFilters()"></empty-state>
                    </div>
                </div>

                <resource-drawer open="$ctrl.acl.editorOpen"
                    title="$ctrl.acl.editor.isNew ? '新增 ACL 用户' : '编辑 ACL 用户'"
                    subtitle="'用户列表将整量提交；留空新密码不会改变既有密码'"
                    width="720" submitting="$ctrl.acl.submitting"
                    submit-disabled="$ctrl.acl.submitting || !$ctrl.acl.editor" submit-label="保存用户"
                    show-submit="true" error="$ctrl.acl.drawerError"
                    on-close="$ctrl.acl.closeEditor()" on-submit="$ctrl.acl.saveUser()">
                    <div class="form-grid">
                        <div class="form-group" ng-class="{'has-error': $ctrl.acl.editorErrors.name}">
                            <label class="form-label" for="acl-user-name">用户名</label>
                            <input id="acl-user-name" class="form-control" type="text"
                                ng-model="$ctrl.acl.editor.name" ng-readonly="!$ctrl.acl.editor.isNew"
                                ng-disabled="$ctrl.acl.submitting" ng-change="$ctrl.acl.clearEditorError('name')"
                                autocomplete="off" spellcheck="false" required>
                            <p class="form-hint">可使用字母、数字、下划线、点、@ 和连字符；编辑时不可改名。</p>
                            <p class="form-error" ng-if="$ctrl.acl.editorErrors.name" role="alert">{{$ctrl.acl.editorErrors.name}}</p>
                        </div>

                        <div class="form-group">
                            <label class="form-label" for="acl-user-enabled">用户状态</label>
                            <label class="form-hint" for="acl-user-enabled">
                                <input id="acl-user-enabled" type="checkbox" ng-model="$ctrl.acl.editor.enabled"
                                    ng-disabled="$ctrl.acl.submitting">
                                启用此用户
                            </label>
                            <p class="form-hint">停用不会删除用户及其规则。</p>
                        </div>

                        <div class="form-group" ng-class="{'has-error': $ctrl.acl.editorErrors.password}">
                            <label class="form-label" for="acl-user-password">新密码</label>
                            <input id="acl-user-password" class="form-control" type="password"
                                ng-model="$ctrl.acl.editor.newPassword" ng-disabled="$ctrl.acl.submitting"
                                ng-change="$ctrl.acl.clearEditorError('password')" autocomplete="new-password"
                                aria-describedby="acl-password-hint acl-password-hint-edit">
                            <p class="form-hint" id="acl-password-hint" ng-if="$ctrl.acl.editor.isNew">
                                仅非空时提交；使用 nopass 规则时可留空。
                            </p>
                            <p class="form-hint" id="acl-password-hint-edit" ng-if="!$ctrl.acl.editor.isNew">
                                已保存密码数量：{{$ctrl.acl.passwordCountText()}}。原密码不可读取，留空表示保持不变。
                            </p>
                            <p class="form-error" ng-if="$ctrl.acl.editorErrors.password" role="alert">{{$ctrl.acl.editorErrors.password}}</p>
                        </div>

                        <div class="form-group form-group--full" ng-class="{'has-error': $ctrl.acl.editorErrors.rules}">
                            <label class="form-label" for="acl-user-rules">ACL 规则</label>
                            <textarea id="acl-user-rules" class="form-control" rows="7"
                                ng-model="$ctrl.acl.editor.rulesText" ng-disabled="$ctrl.acl.submitting"
                                ng-change="$ctrl.acl.clearEditorError('rules')" spellcheck="false"
                                placeholder="例如：~app:* +@read" required></textarea>
                            <p class="form-hint">按空白字符拆分规则，可换行填写，至少需要 1 条。</p>
                            <p class="form-error" ng-if="$ctrl.acl.editorErrors.rules" role="alert">{{$ctrl.acl.editorErrors.rules}}</p>
                        </div>
                    </div>

                    <div class="card">
                        <button class="button button-secondary" type="button"
                            ng-click="$ctrl.acl.moreOpen = !$ctrl.acl.moreOpen"
                            ng-attr-aria-expanded="{{$ctrl.acl.moreOpen}}" aria-controls="acl-more-settings">
                            <ui-icon name="{{$ctrl.acl.moreOpen ? 'chevron-down' : 'chevron-right'}}" size="15"></ui-icon>
                            更多配置 · DB 绑定
                        </button>
                        <div id="acl-more-settings" ng-if="$ctrl.acl.moreOpen">
                            <div class="form-group" ng-class="{'has-error': $ctrl.acl.editorErrors.db}">
                                <label class="form-label" for="acl-user-db">固定 DB（可选）</label>
                                <input id="acl-user-db" class="form-control" type="text" inputmode="numeric"
                                    ng-model="$ctrl.acl.editor.dbText" ng-disabled="$ctrl.acl.submitting"
                                    ng-change="$ctrl.acl.clearEditorError('db')" autocomplete="off"
                                    placeholder="留空表示不绑定">
                                <p class="form-hint">必须是非负整数；0 是有效的 DB 绑定，留空才表示不绑定。</p>
                                <p class="form-error" ng-if="$ctrl.acl.editorErrors.db" role="alert">{{$ctrl.acl.editorErrors.db}}</p>
                            </div>
                        </div>
                    </div>

                    <div class="inline-alert inline-alert--info">
                        <ui-icon name="info" size="18"></ui-icon>
                        <p>保存会携带当前完整用户列表；其他用户的密码哈希不会从浏览器读取或回传。</p>
                    </div>
                </resource-drawer>
            </section>

            <section id="security-panel-qps" role="tabpanel" aria-labelledby="security-tab-qps"
                ng-show="$ctrl.activeTab === 'qps'">
                <qps-limit-panel product="$ctrl.product"></qps-limit-panel>
            </section>
        </div>
    `,
    controller: ['Api', 'Confirm', 'Format', 'Preferences', 'Toast', '$scope',
        function (Api, Confirm, Format, Preferences, Toast, $scope) {
            var ctrl = this;
            var maxSafeInteger = Number.MAX_SAFE_INTEGER || 9007199254740991;
            var requestSequence = 0;
            var mutationSequence = 0;
            var destroyed = false;
            var acl = ctrl.acl = {};

            ctrl.activeTab = 'acl';

            acl.model = emptyModel();
            acl.users = [];
            acl.filteredRows = [];
            acl.metrics = [];
            acl.filters = {search: '', query: '', keyword: '', status: '', type: ''};
            acl.statusOptions = [
                {value: 'enabled', label: '已启用'},
                {value: 'disabled', label: '已停用'}
            ];
            acl.typeOptions = [
                {value: 'bound', label: '已绑定 DB'},
                {value: 'unbound', label: '未绑定 DB'}
            ];
            acl.columns = [
                {key: 'name', label: '用户名', sortable: true, type: 'link'},
                {key: 'enabledLabel', label: '状态', sortable: true},
                {key: 'passwordDisplay', label: '密码'},
                {key: 'rulesDisplay', label: 'ACL 规则'},
                {key: 'dbDisplay', label: 'DB 绑定', sortable: true},
                {key: 'actions', label: '操作', type: 'actions', actions: rowActions()}
            ];
            acl.selected = [];
            acl.page = 1;
            acl.pageSize = 10;
            acl.sortKey = 'name';
            acl.sortDirection = 'asc';
            acl.loaded = false;
            acl.initialLoading = true;
            acl.refreshing = false;
            acl.loadError = '';
            acl.refreshError = '';
            acl.operationError = '';
            acl.refreshFeedback = '';
            acl.lastUpdated = null;
            acl.editorOpen = false;
            acl.editor = newEditor();
            acl.editorErrors = {};
            acl.drawerError = '';
            acl.moreOpen = false;
            acl.submitting = false;
            acl.confirming = false;
            acl.mutationKind = '';

            function emptyModel() {
                return {enabled: null, revision: null, syncStatus: '', updatedAt: null};
            }

            function newEditor() {
                return {
                    isNew: true,
                    originalName: '',
                    name: '',
                    enabled: true,
                    newPassword: '',
                    rulesText: '',
                    dbText: '',
                    passwordCount: null
                };
            }

            function rowActions() {
                return [
                    {
                        name: 'edit',
                        label: '编辑',
                        icon: 'edit',
                        disabled: function () { return acl.submitting || acl.confirming; }
                    },
                    {
                        name: 'delete',
                        label: '删除',
                        icon: 'trash',
                        danger: true,
                        disabled: function () { return acl.submitting || acl.confirming; }
                    }
                ];
            }

            function hasValue(value) {
                return value !== undefined && value !== null;
            }

            function hasDB(user) {
                return user && hasValue(user.db) && user.db !== '';
            }

            function normalizeUser(user) {
                user = user || {};
                return {
                    name: hasValue(user.name) ? String(user.name) : '',
                    enabled: typeof user.enabled === 'boolean' ? user.enabled : null,
                    rules: angular.isArray(user.rules) ? user.rules.slice(0) : [],
                    db: hasDB(user) ? user.db : null,
                    passwordCount: hasValue(user.password_count) ? user.password_count : null,
                    lastError: hasValue(user.last_error) ? String(user.last_error) : ''
                };
            }

            function normalizeModel(data) {
                data = data || {};
                return {
                    enabled: typeof data.enabled === 'boolean' ? data.enabled : null,
                    revision: hasValue(data.revision) ? data.revision : null,
                    syncStatus: hasValue(data.sync_status) ? String(data.sync_status) : '',
                    updatedAt: hasValue(data.updated_at) ? data.updated_at : null
                };
            }

            function displayDate(value) {
                return hasValue(value) && value !== '' ? Format.date(value, true) : '—';
            }

            function updateMetrics() {
                acl.metrics = [
                    {
                        key: 'enabled',
                        title: '访问控制',
                        value: acl.enabledLabel(),
                        caption: 'enabled',
                        icon: 'lock',
                        theme: acl.model.enabled === true ? 'primary' : 'neutral'
                    },
                    {
                        key: 'revision',
                        title: '配置 revision',
                        value: hasValue(acl.model.revision) && acl.model.revision !== '' ? '#' + Format.number(acl.model.revision) : '—',
                        caption: '整量保存后自动递增',
                        icon: 'history',
                        theme: 'neutral'
                    },
                    {
                        key: 'sync',
                        title: '同步状态',
                        value: acl.syncLabel(),
                        caption: acl.model.syncStatus || '—',
                        icon: 'refresh',
                        theme: 'neutral'
                    },
                    {
                        key: 'updated',
                        title: '更新时间',
                        value: displayDate(acl.model.updatedAt),
                        caption: 'updated_at',
                        icon: 'clock',
                        theme: 'neutral'
                    }
                ];
            }

            acl.enabledLabel = function () {
                if (acl.model.enabled === true) { return '已启用'; }
                if (acl.model.enabled === false) { return '已停用'; }
                return '—';
            };

            acl.enabledTone = function () {
                return acl.model.enabled === true ? 'success' : 'neutral';
            };

            acl.syncLabel = function () {
                var status = acl.model.syncStatus;
                if (status === 'ready') { return '已同步'; }
                if (status === 'not_configured') { return '未配置'; }
                if (status && status.indexOf('proxy_sync_failed:') === 0) { return '部分同步失败'; }
                return status ? '状态未知' : '—';
            };

            acl.syncTone = function () {
                var status = acl.model.syncStatus;
                if (status === 'ready') { return 'success'; }
                if (status && status.indexOf('proxy_sync_failed:') === 0) { return 'error'; }
                if (status === 'not_configured') { return 'neutral'; }
                return 'warning';
            };

            function stateKey() {
                return 'acl.' + (ctrl.product || 'default') + '.list';
            }

            function loadState() {
                var saved = Preferences.get(stateKey(), {}) || {};
                var filters = saved.filters || {};
                acl.filters = {
                    search: hasValue(filters.search) ? String(filters.search) : '',
                    query: '', keyword: '',
                    status: ['enabled', 'disabled'].indexOf(filters.status) >= 0 ? filters.status : '',
                    type: ['bound', 'unbound'].indexOf(filters.type) >= 0 ? filters.type : ''
                };
                acl.page = Number(saved.page) > 0 ? Math.floor(Number(saved.page)) : 1;
                acl.sortKey = ['name', 'enabledLabel', 'dbDisplay'].indexOf(saved.sortKey) >= 0 ? saved.sortKey : 'name';
                acl.sortDirection = saved.sortDirection === 'desc' ? 'desc' : 'asc';
            }

            function persistState() {
                if (!ctrl.product) { return; }
                Preferences.set(stateKey(), {
                    filters: angular.copy(acl.filters),
                    page: acl.page,
                    sortKey: acl.sortKey,
                    sortDirection: acl.sortDirection
                });
            }

            function userSearchText() {
                var filters = acl.filters || {};
                return String(filters.search || filters.query || filters.keyword || '').trim().toLowerCase();
            }

            function userMatchesStatus(user, status) {
                if (!status || status === 'all') { return true; }
                if (status === 'enabled') { return user.enabled === true; }
                if (status === 'disabled') { return user.enabled === false; }
                return true;
            }

            function userMatchesType(user, type) {
                if (type === 'bound') { return hasDB(user); }
                if (type === 'unbound') { return !hasDB(user); }
                return true;
            }

            acl.applyFilters = function (resetPage) {
                var search = userSearchText();
                var status = acl.filters.status;
                var type = acl.filters.type;
                acl.filteredRows = [];
                acl.users.forEach(function (user, index) {
                    var haystack = (user.name + ' ' + user.rules.join(' ')).toLowerCase();
                    if (search && haystack.indexOf(search) < 0) { return; }
                    if (!userMatchesStatus(user, status) || !userMatchesType(user, type)) { return; }
                    acl.filteredRows.push({
                        id: user.name || 'acl-user-' + index,
                        name: user.name || '—',
                        enabledLabel: user.enabled === true ? '已启用' : (user.enabled === false ? '已停用' : '—'),
                        passwordDisplay: hasValue(user.passwordCount) && user.passwordCount !== '' ? String(user.passwordCount) + ' 个' : '—',
                        rulesDisplay: user.rules.length ? user.rules.join(' ') : '—',
                        dbDisplay: hasDB(user) ? String(user.db) : '未绑定',
                        actions: rowActions(),
                        source: user,
                        sourceIndex: index
                    });
                });
                if (resetPage !== false) {
                    acl.page = 1;
                }
                persistState();
            };

            acl.clearFilters = function () {
                acl.filters.search = '';
                acl.filters.query = '';
                acl.filters.keyword = '';
                acl.filters.status = '';
                acl.filters.type = '';
                acl.applyFilters();
            };

            function refreshDoneText(options) {
                var time = Format.time(new Date());
                if (options.manual) { return '手动刷新完成 · ' + time; }
                if (options.afterSave) { return '保存后重新获取配置完成 · ' + time; }
                if (!options.background) { return '配置加载完成 · ' + time; }
                return '后台刷新完成 · ' + time;
            }

            function loadACL(options) {
                options = options || {};
                var product = ctrl.product;
                if (!product) {
                    acl.initialLoading = false;
                    acl.refreshing = false;
                    acl.loadError = '请先选择集群';
                    return;
                }
                var sequence = ++requestSequence;
                var hadData = acl.loaded;
                acl.initialLoading = !hadData;
                acl.refreshing = hadData;
                acl.refreshError = '';
                if (!hadData) {
                    acl.loadError = '';
                    acl.refreshFeedback = '';
                } else {
                    acl.refreshFeedback = options.manual ? '正在手动刷新…' : '正在后台刷新…';
                }

                return Api.request('GET', product, '/api/topom/acl/{xauth}').then(function (data) {
                    if (destroyed || sequence !== requestSequence || product !== ctrl.product) { return; }
                    data = data || {};
                    acl.model = normalizeModel(data);
                    acl.users = (angular.isArray(data.users) ? data.users : []).map(normalizeUser);
                    acl.loaded = true;
                    acl.lastUpdated = new Date();
                    acl.loadError = '';
                    acl.refreshError = '';
                    acl.operationError = '';
                    acl.refreshFeedback = refreshDoneText(options);
                    updateMetrics();
                    acl.applyFilters(false);
                    if (options.manual) { Toast.success('访问控制配置已刷新'); }
                }).catch(function (response) {
                    if (destroyed || sequence !== requestSequence || product !== ctrl.product) { return; }
                    var message = Api.errorMessage(response);
                    if (acl.loaded) {
                        acl.refreshError = message;
                        acl.refreshFeedback = (options.manual ? '手动' : '后台') + '刷新失败，已保留原配置';
                        if (options.manual) { Toast.error(message); }
                    } else {
                        acl.loadError = message;
                        acl.refreshFeedback = '';
                    }
                }).finally(function () {
                    if (destroyed || sequence !== requestSequence || product !== ctrl.product) { return; }
                    acl.initialLoading = false;
                    acl.refreshing = false;
                });
            }

            acl.refresh = function () {
                if (acl.refreshing || acl.submitting) { return; }
                return loadACL({manual: true, background: acl.loaded});
            };

            acl.retry = function () {
                return loadACL({manual: true, background: acl.loaded});
            };

            function scrubEditor() {
                if (acl.editor) { acl.editor.newPassword = ''; }
            }

            acl.newUser = function () {
                if (acl.submitting || acl.confirming) { return; }
                scrubEditor();
                acl.editor = newEditor();
                acl.editorErrors = {};
                acl.drawerError = '';
                acl.operationError = '';
                acl.moreOpen = false;
                acl.editorOpen = true;
            };

            acl.editUser = function (row) {
                if (acl.submitting || acl.confirming) { return; }
                var user = row && row.source ? row.source : null;
                if (!user && row) {
                    for (var i = 0; i < acl.users.length; i++) {
                        if (acl.users[i].name === row.name) {
                            user = acl.users[i];
                            break;
                        }
                    }
                }
                if (!user) {
                    Toast.error('该用户已不在当前列表中，请刷新后重试');
                    return;
                }
                scrubEditor();
                acl.editor = {
                    isNew: false,
                    originalName: user.name,
                    name: user.name,
                    enabled: user.enabled,
                    newPassword: '',
                    rulesText: user.rules.join('\n'),
                    dbText: hasDB(user) ? String(user.db) : '',
                    passwordCount: user.passwordCount
                };
                acl.editorErrors = {};
                acl.drawerError = '';
                acl.operationError = '';
                acl.moreOpen = hasDB(user);
                acl.editorOpen = true;
            };

            acl.closeEditor = function () {
                if (acl.submitting) { return; }
                acl.editorOpen = false;
                acl.editorErrors = {};
                acl.drawerError = '';
                acl.moreOpen = false;
                scrubEditor();
            };

            acl.clearEditorError = function (field) {
                if (acl.submitting) { return; }
                acl.editorErrors[field] = '';
                acl.drawerError = '';
            };

            acl.passwordCountText = function () {
                return hasValue(acl.editor.passwordCount) && acl.editor.passwordCount !== '' ? String(acl.editor.passwordCount) : '—';
            };

            acl.rowAction = function (row, action) {
                var id = angular.isString(action) ? action : action && (action.id || action.action || action.name);
                if (id === 'edit') {
                    acl.editUser(row);
                } else if (id === 'delete' || id === 'remove') {
                    acl.deleteUser(row);
                }
            };

            function splitRules(text) {
                var parts = String(text || '').trim().split(/\s+/);
                return parts.filter(function (part) { return part !== ''; });
            }

            function parseOptionalDB(text) {
                var raw = String(hasValue(text) ? text : '').trim();
                if (raw === '') { return {present: false, value: null}; }
                if (!/^\d+$/.test(raw)) { return {present: true, value: null}; }
                var value = Number(raw);
                if (!isFinite(value) || Math.floor(value) !== value || value < 0 || value > maxSafeInteger) {
                    return {present: true, value: null};
                }
                return {present: true, value: value};
            }

            function cloneCurrentUsers() {
                return acl.users.map(function (user) {
                    var cloned = {
                        name: user.name,
                        enabled: user.enabled === true,
                        rules: user.rules.slice(0)
                    };
                    if (hasDB(user)) { cloned.db = user.db; }
                    return cloned;
                });
            }

            function payloadUsers(users) {
                return users.map(function (user) {
                    var payload = {
                        name: user.name,
                        enabled: user.enabled === true,
                        rules: (user.rules || []).slice(0)
                    };
                    if (hasDB(user)) { payload.db = user.db; }
                    if (typeof user.new_password === 'string' && user.new_password.length > 0) {
                        payload.new_password = user.new_password;
                    }
                    return payload;
                });
            }

            function clearPayloadPasswords(users) {
                (users || []).forEach(function (user) {
                    if (user && user.new_password !== undefined) { user.new_password = ''; }
                });
            }

            function submitACL(users, options) {
                if (acl.submitting || destroyed) { return; }
                var product = ctrl.product;
                if (!product) { return; }
                var sequence = ++mutationSequence;
                var usersForRequest = payloadUsers(users);
                acl.submitting = true;
                acl.mutationKind = options.kind;
                acl.operationError = '';
                if (options.kind === 'user') { acl.drawerError = ''; }

                Api.request('PUT', product, '/api/topom/acl/{xauth}', {
                    enabled: options.enabled === true,
                    users: usersForRequest
                }).then(function () {
                    if (destroyed || sequence !== mutationSequence || product !== ctrl.product) { return; }
                    if (options.kind === 'user') {
                        acl.editorOpen = false;
                        acl.moreOpen = false;
                        scrubEditor();
                    }
                    acl.drawerError = '';
                    Toast.success(options.successMessage);
                    loadACL({background: true, afterSave: true});
                }).catch(function (response) {
                    if (destroyed || sequence !== mutationSequence || product !== ctrl.product) { return; }
                    var message = Api.errorMessage(response);
                    if (options.kind === 'user') {
                        acl.drawerError = message;
                    } else {
                        acl.operationError = message;
                    }
                    Toast.error(message);
                }).finally(function () {
                    clearPayloadPasswords(usersForRequest);
                    if (destroyed || sequence !== mutationSequence || product !== ctrl.product) { return; }
                    acl.submitting = false;
                    acl.mutationKind = '';
                });
            }

            acl.saveUser = function () {
                if (acl.submitting || destroyed || !acl.editor) { return; }
                var editor = acl.editor;
                var name = String(editor.name || '').trim();
                var rules = splitRules(editor.rulesText);
                var db = parseOptionalDB(editor.dbText);
                acl.editorErrors = {};
                acl.drawerError = '';

                if (!name) {
                    acl.editorErrors.name = '请输入用户名';
                } else if (!/^[A-Za-z0-9_][A-Za-z0-9_.@-]*$/.test(name)) {
                    acl.editorErrors.name = '用户名格式无效，请使用字母、数字、下划线、点、@ 或连字符';
                } else if (!editor.isNew && name !== editor.originalName) {
                    acl.editorErrors.name = '编辑已有用户时不可修改用户名';
                }
                if (!rules.length) {
                    acl.editorErrors.rules = '至少需要 1 条 ACL 规则';
                }
                if (db.present && db.value === null) {
                    acl.editorErrors.db = 'DB 必须是非负安全整数，或留空表示不绑定';
                }

                var users = cloneCurrentUsers();
                var matchingIndexes = [];
                users.forEach(function (user, index) {
                    if (user.name === name) { matchingIndexes.push(index); }
                });
                if (editor.isNew && matchingIndexes.length) {
                    acl.editorErrors.name = '用户名已存在，请使用其他名称';
                }
                if (!editor.isNew && matchingIndexes.length > 1) {
                    acl.editorErrors.name = '当前列表存在用户名冲突，请刷新后检查配置';
                }
                if (acl.editorErrors.name || acl.editorErrors.rules || acl.editorErrors.db) {
                    acl.drawerError = '请修正表单中的错误后重试';
                    return;
                }

                var updated = {
                    name: name,
                    enabled: editor.enabled === true,
                    rules: rules
                };
                if (db.present) { updated.db = db.value; }
                if (typeof editor.newPassword === 'string' && editor.newPassword.length > 0) {
                    updated.new_password = editor.newPassword;
                }

                if (editor.isNew) {
                    users.push(updated);
                } else {
                    var originalIndex = -1;
                    users.forEach(function (user, index) {
                        if (user.name === editor.originalName) { originalIndex = index; }
                    });
                    if (originalIndex < 0) {
                        acl.drawerError = '该用户已在后台刷新中移除，请关闭抽屉并刷新列表';
                        return;
                    }
                    users[originalIndex] = updated;
                }

                submitACL(users, {
                    kind: 'user',
                    enabled: acl.model.enabled === true,
                    successMessage: editor.isNew ? 'ACL 用户已新增，正在刷新配置' : 'ACL 用户已保存，正在刷新配置'
                });
            };

            function confirmProduct() {
                return ctrl.product;
            }

            acl.toggleGlobal = function () {
                if (acl.submitting || acl.confirming || destroyed) { return; }
                var product = confirmProduct();
                var nextEnabled = acl.model.enabled !== true;
                acl.confirming = true;
                acl.operationError = '';
                Confirm.open({
                    title: nextEnabled ? '启用访问控制？' : '停用访问控制？',
                    message: nextEnabled ?
                        '将启用整个集群的 ACL，并整量同步当前全部用户。' :
                        '将停用整个集群的 ACL。用户与规则会保留，但访问限制将不再生效。',
                    confirmText: nextEnabled ? '确认启用' : '确认停用',
                    danger: !nextEnabled
                }).then(function () {
                    if (destroyed || product !== ctrl.product) { return; }
                    acl.confirming = false;
                    submitACL(cloneCurrentUsers(), {
                        kind: 'global',
                        enabled: nextEnabled,
                        successMessage: nextEnabled ? '访问控制已启用，正在刷新配置' : '访问控制已停用，正在刷新配置'
                    });
                }, function () {
                    if (!destroyed && product === ctrl.product) { acl.confirming = false; }
                });
            };

            acl.deleteUser = function (row) {
                if (acl.submitting || acl.confirming || destroyed) { return; }
                var user = row && row.source ? row.source : null;
                var name = user ? user.name : row && row.name;
                if (!name) { return; }
                var product = confirmProduct();
                acl.confirming = true;
                acl.operationError = '';
                Confirm.open({
                    title: '删除 ACL 用户？',
                    message: '将删除用户“' + name + '”并整量提交其余用户。此操作会同步到 Redis 与 Proxy。',
                    confirmText: '确认删除',
                    danger: true
                }).then(function () {
                    if (destroyed || product !== ctrl.product) { return; }
                    acl.confirming = false;
                    var users = cloneCurrentUsers().filter(function (item) { return item.name !== name; });
                    if (users.length === acl.users.length) {
                        Toast.info('该用户已不在当前列表中');
                        return;
                    }
                    submitACL(users, {
                        kind: 'delete',
                        enabled: acl.model.enabled === true,
                        successMessage: 'ACL 用户已删除，正在刷新配置'
                    });
                }, function () {
                    if (!destroyed && product === ctrl.product) { acl.confirming = false; }
                });
            };

            function resetForProduct() {
                requestSequence++;
                mutationSequence++;
                scrubEditor();
                acl.model = emptyModel();
                acl.users = [];
                acl.filteredRows = [];
                acl.filters = {search: '', query: '', keyword: '', status: '', type: ''};
                acl.selected = [];
                acl.page = 1;
                acl.loaded = false;
                acl.initialLoading = true;
                acl.refreshing = false;
                acl.loadError = '';
                acl.refreshError = '';
                acl.operationError = '';
                acl.refreshFeedback = '';
                acl.lastUpdated = null;
                acl.editorOpen = false;
                acl.editor = newEditor();
                acl.editorErrors = {};
                acl.drawerError = '';
                acl.moreOpen = false;
                acl.submitting = false;
                acl.confirming = false;
                acl.mutationKind = '';
                loadState();
                updateMetrics();
            }

            ctrl.setTab = function (tab) {
                if (tab === 'acl' || tab === 'qps') { ctrl.activeTab = tab; }
            };

            ctrl.$onInit = function () {
                loadState();
                updateMetrics();
                loadACL();
            };

            ctrl.$onChanges = function (changes) {
                if (changes.product && !changes.product.isFirstChange() &&
                    changes.product.currentValue !== changes.product.previousValue) {
                    resetForProduct();
                    loadACL();
                }
            };

            ctrl.$onDestroy = function () {
                destroyed = true;
                requestSequence++;
                mutationSequence++;
                scrubEditor();
            };

            $scope.$watchGroup([
                function () { return acl.page; },
                function () { return acl.sortKey; },
                function () { return acl.sortDirection; }
            ], persistState);

            $scope.$on('cluster:refreshRequested', function (event, payload) {
                if (!destroyed && payload && payload.product === ctrl.product) {
                    loadACL({background: true});
                }
            });
        }
    ]
});
