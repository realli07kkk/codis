'use strict';

dashboard.directive('rdbFileInput', function () {
    return {
        restrict: 'A',
        scope: {rdbFileInput: '&'},
        link: function (scope, element) {
            function onChange() {
                scope.rdbFileInput({input: element[0]});
            }
            element.on('change', onChange);
            scope.$on('$destroy', function () { element.off('change', onChange); });
        }
    };
});

dashboard.component('analysisPage', {
    bindings: {
        product: '<'
    },
    template: `
        <div class="analysis-page">
            <page-header title="RDB 数据分析"
                description="分析真实 RDB 快照中的 Key 数量、内存占用、前缀分布与大 Key / 热 Key。">
                <button class="button button-secondary" type="button" ng-if="$ctrl.job"
                    ng-click="$ctrl.refresh()" ng-disabled="$ctrl.refreshing || $ctrl.busy()">
                    <ui-icon name="refresh" size="16" ng-class="{'is-spinning': $ctrl.refreshing}"></ui-icon>
                    {{$ctrl.refreshing ? '刷新中' : '刷新任务'}}
                </button>
                <button class="button button-primary" type="button"
                    ng-click="$ctrl.openDrawer()" ng-disabled="$ctrl.busy()">
                    <ui-icon name="plus" size="16"></ui-icon>新建分析
                </button>
            </page-header>

            <div class="inline-alert inline-alert--error" ng-if="$ctrl.operationError" role="alert">
                <ui-icon name="alert" size="18"></ui-icon>
                <div><strong>操作未完成</strong><p>{{$ctrl.operationError}}</p></div>
            </div>
            <div class="inline-alert inline-alert--error" ng-if="$ctrl.job && $ctrl.refreshError" role="alert">
                <ui-icon name="alert" size="18"></ui-icon>
                <div>
                    <strong>{{$ctrl.hasSnapshot ? '刷新失败，已保留当前任务内容' : '任务加载失败'}}</strong>
                    <p>{{$ctrl.refreshError}}</p>
                    <button class="button button-secondary" type="button"
                        ng-click="$ctrl.refresh()" ng-disabled="$ctrl.refreshing || $ctrl.busy()">重试</button>
                </div>
            </div>

            <loading-skeleton ng-if="$ctrl.initialLoading" variant="page"></loading-skeleton>
            <empty-state ng-if="!$ctrl.initialLoading && !$ctrl.job" icon="analysis"
                title="还没有 RDB 分析任务"
                description="从浏览器上传 RDB 文件、选择 workspace 中的相对路径，或从当前集群 Redis Server 获取快照。"
                action-label="新建分析" on-action="$ctrl.openDrawer()"></empty-state>

            <div ng-if="!$ctrl.initialLoading && $ctrl.job">
                <section class="panel" aria-label="当前分析任务">
                    <div class="card-header">
                        <div style="min-width:0">
                            <h2 class="card-title">当前任务</h2>
                            <p class="table-text" ng-attr-title="{{$ctrl.job.id}}">Job ID · {{$ctrl.job.id}}</p>
                        </div>
                        <div class="card-actions">
                            <status-tag status="$ctrl.job.status"></status-tag>
                            <button class="button button-secondary" type="button"
                                ng-if="!$ctrl.isTerminal($ctrl.job.status)"
                                ng-click="$ctrl.cancelJob()" ng-disabled="$ctrl.busy()">
                                <ui-icon name="stop" size="15"></ui-icon>取消任务
                            </button>
                            <button class="button button-danger" type="button"
                                ng-click="$ctrl.removeJob()" ng-disabled="$ctrl.busy()">
                                <ui-icon name="trash" size="15"></ui-icon>
                                {{$ctrl.isTerminal($ctrl.job.status) ? '清除任务' : '取消并清除'}}
                            </button>
                        </div>
                    </div>

                    <div class="metrics-grid">
                        <metric-card ng-repeat="metric in $ctrl.summaryMetrics track by metric.key"
                            metric="metric"></metric-card>
                    </div>
                    <div aria-label="任务进度">
                        <div style="display:flex;justify-content:space-between;gap:12px;margin-bottom:7px">
                            <span class="form-hint" style="margin:0">读取进度</span>
                            <strong class="table-number">{{$ctrl.progressPercent()}}%</strong>
                        </div>
                        <div class="progress-track" role="progressbar" aria-valuemin="0" aria-valuemax="100"
                            ng-attr-aria-valuenow="{{$ctrl.progressPercent()}}"
                            ng-attr-aria-label="RDB 分析进度 {{$ctrl.progressPercent()}}%">
                            <span class="progress-bar" ng-class="$ctrl.progressBarClass()"
                                ng-style="{width: $ctrl.progressPercent() + '%'}"></span>
                        </div>
                    </div>
                    <div class="key-value-grid" style="margin-top:12px">
                        <div class="key-value-item">
                            <dt>来源</dt>
                            <dd class="table-text" ng-attr-title="{{$ctrl.job.source || ''}}">{{$ctrl.text($ctrl.job.source)}}</dd>
                        </div>
                        <div class="key-value-item"><dt>DB 数量</dt><dd>{{$ctrl.number($ctrl.job.db_count)}}</dd></div>
                        <div class="key-value-item"><dt>对象数量</dt><dd>{{$ctrl.number($ctrl.job.objects_read)}}</dd></div>
                        <div class="key-value-item"><dt>分析总大小</dt><dd>{{$ctrl.bytes($ctrl.job.total_size)}}</dd></div>
                    </div>
                    <p class="form-hint" ng-if="$ctrl.refreshFeedback">{{$ctrl.refreshFeedback}}</p>
                </section>

                <div class="inline-alert inline-alert--error" ng-if="$ctrl.job.error" role="alert">
                    <ui-icon name="alert" size="18"></ui-icon>
                    <div><strong>分析任务返回错误</strong><p>{{$ctrl.job.error}}</p></div>
                </div>

                <section class="panel" aria-label="RDB 分析结果" style="margin-top:20px">
                    <div class="card-header">
                        <div>
                            <h2 class="card-title">分析结果</h2>
                            <p>结果仅来自 Dashboard 返回的任务快照；运行中的结果会随后台轮询更新。</p>
                        </div>
                        <span class="form-hint" ng-if="$ctrl.lastUpdated">更新于 {{$ctrl.date($ctrl.lastUpdated)}}</span>
                    </div>

                    <div class="tabs" role="tablist" aria-label="分析结果分类" style="overflow-x:auto">
                        <button ng-repeat="tab in $ctrl.resultTabs track by tab.id"
                            id="analysis-result-tab-{{tab.id}}" class="tab-button" type="button" role="tab"
                            ng-class="{active: $ctrl.activeResultTab === tab.id}"
                            ng-click="$ctrl.setResultTab(tab.id)" aria-controls="analysis-result-panel-{{tab.id}}"
                            ng-attr-aria-selected="{{$ctrl.activeResultTab === tab.id}}">
                            <ui-icon name="{{tab.icon}}" size="15"></ui-icon>
                            {{tab.label}}<span ng-if="tab.id !== 'summary'">（{{$ctrl.resultCount(tab.id)}}）</span>
                        </button>
                    </div>

                    <div ng-switch="$ctrl.activeResultTab" style="margin-top:16px">
                        <section id="analysis-result-panel-summary" role="tabpanel"
                            aria-labelledby="analysis-result-tab-summary" ng-switch-when="summary">
                            <div class="data-table-scroll" ng-if="$ctrl.summaryFields.length">
                                <table class="result-table" style="table-layout:fixed;min-width:680px">
                                    <caption class="sr-only">RDB 分析任务 Summary</caption>
                                    <thead><tr><th style="width:32%">字段</th><th style="width:68%">值</th></tr></thead>
                                    <tbody><tr ng-repeat="field in $ctrl.summaryFields track by field.key">
                                        <td><span class="table-text" ng-attr-title="{{field.label}}">{{field.label}}</span></td>
                                        <td ng-style="{'text-align': field.numeric ? 'right' : 'left'}">
                                            <span ng-class="field.numeric ? 'table-number' : 'table-text'"
                                                ng-attr-title="{{field.value}}">{{field.value}}</span>
                                        </td>
                                    </tr></tbody>
                                </table>
                            </div>
                            <empty-state ng-if="!$ctrl.summaryFields.length" icon="analysis"
                                title="暂无任务摘要" description="Dashboard 尚未返回可展示的任务字段。"></empty-state>
                        </section>

                        <section id="analysis-result-panel-type" role="tabpanel"
                            aria-labelledby="analysis-result-tab-type" ng-switch-when="type">
                            <div class="data-table-scroll" ng-if="$ctrl.job.type_summary.length">
                                <table class="result-table" style="table-layout:fixed;min-width:680px">
                                    <caption class="sr-only">按 Redis 类型汇总</caption>
                                    <thead><tr>
                                        <th style="width:28%">类型 name</th>
                                        <th style="width:24%;text-align:right">Key 数量 key_count</th>
                                        <th style="width:24%;text-align:right">大小 size（bytes）</th>
                                        <th style="width:24%;text-align:right">可读大小 size_readable</th>
                                    </tr></thead>
                                    <tbody><tr ng-repeat="row in $ctrl.job.type_summary track by $index">
                                        <td><span class="table-text" ng-attr-title="{{$ctrl.text(row.name)}}">{{$ctrl.text(row.name)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.key_count)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.size)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.readable(row)}}</span></td>
                                    </tr></tbody>
                                </table>
                            </div>
                            <empty-state ng-if="!$ctrl.job.type_summary.length" icon="database"
                                title="暂无类型汇总" description="type_summary 为空或尚未生成。"></empty-state>
                        </section>

                        <section id="analysis-result-panel-db" role="tabpanel"
                            aria-labelledby="analysis-result-tab-db" ng-switch-when="db">
                            <div class="data-table-scroll" ng-if="$ctrl.job.db_summary.length">
                                <table class="result-table" style="table-layout:fixed;min-width:760px">
                                    <caption class="sr-only">按 Redis DB 汇总</caption>
                                    <thead><tr>
                                        <th style="width:22%">名称 name</th>
                                        <th style="width:12%;text-align:right">DB</th>
                                        <th style="width:22%;text-align:right">Key 数量</th>
                                        <th style="width:22%;text-align:right">大小（bytes）</th>
                                        <th style="width:22%;text-align:right">可读大小</th>
                                    </tr></thead>
                                    <tbody><tr ng-repeat="row in $ctrl.job.db_summary track by $index">
                                        <td><span class="table-text" ng-attr-title="{{$ctrl.text(row.name)}}">{{$ctrl.text(row.name)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.db)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.key_count)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.size)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.readable(row)}}</span></td>
                                    </tr></tbody>
                                </table>
                            </div>
                            <empty-state ng-if="!$ctrl.job.db_summary.length" icon="database"
                                title="暂无 DB 汇总" description="db_summary 为空或尚未生成。"></empty-state>
                        </section>

                        <section id="analysis-result-panel-prefix" role="tabpanel"
                            aria-labelledby="analysis-result-tab-prefix" ng-switch-when="prefix">
                            <div class="data-table-scroll" ng-if="$ctrl.job.prefix_summary.length">
                                <table class="result-table" style="table-layout:fixed;min-width:820px">
                                    <caption class="sr-only">按 Key 前缀汇总</caption>
                                    <thead><tr>
                                        <th style="width:34%">前缀 name</th>
                                        <th style="width:10%;text-align:right">DB</th>
                                        <th style="width:18%;text-align:right">Key 数量</th>
                                        <th style="width:19%;text-align:right">大小（bytes）</th>
                                        <th style="width:19%;text-align:right">可读大小</th>
                                    </tr></thead>
                                    <tbody><tr ng-repeat="row in $ctrl.job.prefix_summary track by $index">
                                        <td><span class="table-text" ng-attr-title="{{$ctrl.text(row.name)}}">{{$ctrl.text(row.name)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.db)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.key_count)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.size)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.readable(row)}}</span></td>
                                    </tr></tbody>
                                </table>
                            </div>
                            <empty-state ng-if="!$ctrl.job.prefix_summary.length" icon="database"
                                title="暂无前缀汇总" description="prefix_summary 为空；可检查前缀分隔符与 max_depth 配置。"></empty-state>
                        </section>

                        <section id="analysis-result-panel-big" role="tabpanel"
                            aria-labelledby="analysis-result-tab-big" ng-switch-when="big">
                            <div class="data-table-scroll" ng-if="$ctrl.job.top_big_keys.length">
                                <table class="result-table" style="table-layout:fixed;min-width:1180px">
                                    <caption class="sr-only">Top Big Keys</caption>
                                    <thead><tr>
                                        <th style="width:60px;text-align:right">DB</th>
                                        <th style="width:280px">Key</th><th style="width:80px">类型</th>
                                        <th style="width:110px;text-align:right">元素数</th><th style="width:110px">Encoding</th>
                                        <th style="width:100px;text-align:right">Frequency</th>
                                        <th style="width:130px;text-align:right">大小（bytes）</th>
                                        <th style="width:120px;text-align:right">可读大小</th>
                                    </tr></thead>
                                    <tbody><tr ng-repeat="row in $ctrl.job.top_big_keys track by $index">
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.db)}}</span></td>
                                        <td><span class="table-text" ng-attr-title="{{$ctrl.text(row.key)}}">{{$ctrl.text(row.key)}}</span></td>
                                        <td><span class="table-text" ng-attr-title="{{$ctrl.text(row.type)}}">{{$ctrl.text(row.type)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.element_count)}}</span></td>
                                        <td><span class="table-text" ng-attr-title="{{$ctrl.text(row.encoding)}}">{{$ctrl.text(row.encoding)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.frequency)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.size)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.readable(row)}}</span></td>
                                    </tr></tbody>
                                </table>
                            </div>
                            <empty-state ng-if="!$ctrl.job.top_big_keys.length" icon="database"
                                title="暂无 Big Keys" description="top_big_keys 为空或尚未生成。"></empty-state>
                        </section>

                        <section id="analysis-result-panel-hot" role="tabpanel"
                            aria-labelledby="analysis-result-tab-hot" ng-switch-when="hot">
                            <div class="data-table-scroll" ng-if="$ctrl.job.top_hot_keys.length">
                                <table class="result-table" style="table-layout:fixed;min-width:1180px">
                                    <caption class="sr-only">Top Hot Keys</caption>
                                    <thead><tr>
                                        <th style="width:60px;text-align:right">DB</th>
                                        <th style="width:280px">Key</th><th style="width:80px">类型</th>
                                        <th style="width:110px;text-align:right">元素数</th><th style="width:110px">Encoding</th>
                                        <th style="width:100px;text-align:right">Frequency</th>
                                        <th style="width:130px;text-align:right">大小（bytes）</th>
                                        <th style="width:120px;text-align:right">可读大小</th>
                                    </tr></thead>
                                    <tbody><tr ng-repeat="row in $ctrl.job.top_hot_keys track by $index">
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.db)}}</span></td>
                                        <td><span class="table-text" ng-attr-title="{{$ctrl.text(row.key)}}">{{$ctrl.text(row.key)}}</span></td>
                                        <td><span class="table-text" ng-attr-title="{{$ctrl.text(row.type)}}">{{$ctrl.text(row.type)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.element_count)}}</span></td>
                                        <td><span class="table-text" ng-attr-title="{{$ctrl.text(row.encoding)}}">{{$ctrl.text(row.encoding)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.frequency)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.size)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.readable(row)}}</span></td>
                                    </tr></tbody>
                                </table>
                            </div>
                            <empty-state ng-if="!$ctrl.job.top_hot_keys.length" icon="database"
                                title="暂无 Hot Keys" description="top_hot_keys 为空；RDB 未提供 LFU frequency 时不会出现热 Key。"></empty-state>
                        </section>

                        <section id="analysis-result-panel-flame" role="tabpanel"
                            aria-labelledby="analysis-result-tab-flame" ng-switch-when="flame">
                            <div class="data-table-scroll" ng-if="$ctrl.flameRows.length">
                                <table class="result-table" style="table-layout:fixed;min-width:760px">
                                    <caption class="sr-only">Flamegraph 节点，最多 100 行</caption>
                                    <thead><tr>
                                        <th style="width:56%">节点路径 name</th>
                                        <th style="width:22%;text-align:right">大小 value（bytes）</th>
                                        <th style="width:22%;text-align:right">可读大小</th>
                                    </tr></thead>
                                    <tbody><tr ng-repeat="row in $ctrl.flameRows track by $index">
                                        <td ng-style="{'padding-left': (12 + row.depth * 18) + 'px'}">
                                            <span class="table-text" ng-attr-title="{{row.name}}">{{row.name}}</span>
                                        </td>
                                        <td style="text-align:right"><span class="table-number">{{$ctrl.number(row.size)}}</span></td>
                                        <td style="text-align:right"><span class="table-number">{{row.size_readable}}</span></td>
                                    </tr></tbody>
                                </table>
                                <p class="form-hint">按 size 降序、name 升序深度展开，最多展示 100 行。</p>
                            </div>
                            <empty-state ng-if="!$ctrl.flameRows.length" icon="analysis"
                                title="暂无 Flamegraph" description="flamegraph 为空或没有可展开的子节点。"></empty-state>
                        </section>
                    </div>
                </section>
            </div>

            <resource-drawer open="$ctrl.drawerOpen" title="'新建 RDB 分析'"
                subtitle="'三种来源使用相同的分析配置；提交失败时会保留当前全部输入'"
                width="760" submitting="$ctrl.submitting" submit-disabled="$ctrl.submitting"
                submit-label="开始分析" show-submit="true" error="$ctrl.drawerError"
                on-close="$ctrl.closeDrawer()" on-submit="$ctrl.submit()">
                <div class="tabs" role="tablist" aria-label="RDB 来源">
                    <button id="rdb-source-tab-upload" class="tab-button" type="button" role="tab"
                        ng-class="{active: $ctrl.form.source === 'upload'}" ng-click="$ctrl.setSource('upload')"
                        ng-disabled="$ctrl.submitting" aria-controls="rdb-source-panel-upload"
                        ng-attr-aria-selected="{{$ctrl.form.source === 'upload'}}">
                        <ui-icon name="upload" size="16"></ui-icon>浏览器上传
                    </button>
                    <button id="rdb-source-tab-workspace" class="tab-button" type="button" role="tab"
                        ng-class="{active: $ctrl.form.source === 'workspace'}" ng-click="$ctrl.setSource('workspace')"
                        ng-disabled="$ctrl.submitting" aria-controls="rdb-source-panel-workspace"
                        ng-attr-aria-selected="{{$ctrl.form.source === 'workspace'}}">
                        <ui-icon name="file" size="16"></ui-icon>Workspace
                    </button>
                    <button id="rdb-source-tab-remote" class="tab-button" type="button" role="tab"
                        ng-class="{active: $ctrl.form.source === 'remote'}" ng-click="$ctrl.setSource('remote')"
                        ng-disabled="$ctrl.submitting" aria-controls="rdb-source-panel-remote"
                        ng-attr-aria-selected="{{$ctrl.form.source === 'remote'}}">
                        <ui-icon name="server" size="16"></ui-icon>Remote Server
                    </button>
                </div>

                <div style="margin-top:18px">
                    <section id="rdb-source-panel-upload" role="tabpanel" aria-labelledby="rdb-source-tab-upload"
                        ng-show="$ctrl.form.source === 'upload'" class="form-group"
                        ng-class="{'has-error': $ctrl.fieldErrors.file}">
                        <label class="form-label" for="rdb-upload-file">RDB 文件 <span class="required">*</span></label>
                        <input id="rdb-upload-file" class="form-control" type="file"
                            rdb-file-input="$ctrl.fileChanged(input)"
                            ng-disabled="$ctrl.submitting" required>
                        <p class="form-hint">文件仅用于本次 multipart 上传，不会写入浏览器 Preferences。</p>
                        <p class="form-hint" ng-if="$ctrl.form.file">已选择：{{$ctrl.fileText()}}</p>
                        <p class="form-error" ng-if="$ctrl.fieldErrors.file" role="alert">{{$ctrl.fieldErrors.file}}</p>
                    </section>

                    <section id="rdb-source-panel-workspace" role="tabpanel" aria-labelledby="rdb-source-tab-workspace"
                        ng-show="$ctrl.form.source === 'workspace'" class="form-group"
                        ng-class="{'has-error': $ctrl.fieldErrors.path}">
                        <label class="form-label" for="rdb-workspace-path">Workspace 相对路径 <span class="required">*</span></label>
                        <input id="rdb-workspace-path" class="form-control" type="text"
                            ng-model="$ctrl.form.path" ng-disabled="$ctrl.submitting"
                            ng-change="$ctrl.clearFieldError('path')" placeholder="例如：backups/dump.rdb"
                            autocomplete="off" spellcheck="false" required>
                        <p class="form-hint">路径相对于 Dashboard 的 rdb_analysis_workspace；不可使用绝对路径或 .. 越界。</p>
                        <p class="form-error" ng-if="$ctrl.fieldErrors.path" role="alert">{{$ctrl.fieldErrors.path}}</p>
                    </section>

                    <section id="rdb-source-panel-remote" role="tabpanel" aria-labelledby="rdb-source-tab-remote"
                        ng-show="$ctrl.form.source === 'remote'">
                        <div class="form-group">
                            <label class="form-label" for="rdb-remote-options">从 Group Server 选择（可选）</label>
                            <select id="rdb-remote-options" class="form-control"
                                ng-model="$ctrl.selectedRemoteServer" ng-change="$ctrl.chooseRemoteServer()"
                                ng-options="server.address as server.label for server in $ctrl.remoteServers"
                                ng-disabled="$ctrl.submitting">
                                <option value="">选择当前集群中的 Redis Server</option>
                            </select>
                            <p class="form-hint">选择列表来自 ClusterStore groups；也可以在下方的 server_addr 中手动输入。</p>
                        </div>
                        <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.server_addr}">
                            <label class="form-label" for="rdb-remote-server">server_addr <span class="required">*</span></label>
                            <input id="rdb-remote-server" class="form-control" type="text"
                                ng-model="$ctrl.form.server_addr" ng-disabled="$ctrl.submitting"
                                ng-change="$ctrl.clearFieldError('server_addr')" placeholder="redis.example.com:6379"
                                autocomplete="off" spellcheck="false" required>
                            <p class="form-hint">仅输入 host:port（IPv6 使用 [address]:port），不要包含 URL、路径或认证信息。</p>
                            <p class="form-error" ng-if="$ctrl.fieldErrors.server_addr" role="alert">{{$ctrl.fieldErrors.server_addr}}</p>
                        </div>
                    </section>
                </div>

                <div class="advanced-config">
                    <button class="button button-secondary" type="button"
                        ng-click="$ctrl.moreOpen = !$ctrl.moreOpen" ng-disabled="$ctrl.submitting"
                        ng-attr-aria-expanded="{{$ctrl.moreOpen}}" aria-controls="rdb-more-settings">
                        <ui-icon name="{{$ctrl.moreOpen ? 'chevron-down' : 'chevron-right'}}" size="15"></ui-icon>
                        更多配置
                    </button>
                    <div id="rdb-more-settings" ng-if="$ctrl.moreOpen">
                        <div class="form-grid">
                            <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.top_n}">
                                <label class="form-label" for="rdb-option-top-n">top_n</label>
                                <input id="rdb-option-top-n" class="form-control" type="text" inputmode="numeric"
                                    ng-model="$ctrl.form.options.top_n" ng-disabled="$ctrl.submitting"
                                    ng-change="$ctrl.clearFieldError('top_n')" autocomplete="off" required>
                                <p class="form-hint">1 到 100 的整数，默认 20。</p>
                                <p class="form-error" ng-if="$ctrl.fieldErrors.top_n" role="alert">{{$ctrl.fieldErrors.top_n}}</p>
                            </div>
                            <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.max_depth}">
                                <label class="form-label" for="rdb-option-max-depth">max_depth</label>
                                <input id="rdb-option-max-depth" class="form-control" type="text" inputmode="numeric"
                                    ng-model="$ctrl.form.options.max_depth" ng-disabled="$ctrl.submitting"
                                    ng-change="$ctrl.clearFieldError('max_depth')" autocomplete="off" required>
                                <p class="form-hint">0 到 16 的整数；0 由后端采用默认深度，默认值为 3。</p>
                                <p class="form-error" ng-if="$ctrl.fieldErrors.max_depth" role="alert">{{$ctrl.fieldErrors.max_depth}}</p>
                            </div>
                            <div class="form-group form-group--full">
                                <label class="form-label" for="rdb-option-separators">prefix_separators</label>
                                <input id="rdb-option-separators" class="form-control" type="text"
                                    ng-model="$ctrl.form.options.prefix_separators" ng-disabled="$ctrl.submitting"
                                    placeholder="例如：:,/,-" autocomplete="off" spellcheck="false">
                                <p class="form-hint">使用英文逗号分隔，提交为字符串数组；留空由后端使用默认分隔符 :。</p>
                            </div>
                            <div class="form-group form-group--full">
                                <label class="form-label" for="rdb-option-regex">regex（可选）</label>
                                <textarea id="rdb-option-regex" class="form-control" rows="3"
                                    ng-model="$ctrl.form.options.regex" ng-disabled="$ctrl.submitting"
                                    placeholder="仅分析匹配此 Go 正则表达式的 Key" spellcheck="false"></textarea>
                            </div>
                            <div class="form-group form-group--full">
                                <label class="form-hint" for="rdb-option-expired">
                                    <input id="rdb-option-expired" type="checkbox"
                                        ng-model="$ctrl.form.options.include_expired" ng-disabled="$ctrl.submitting">
                                    include_expired · 将已过期 Key 纳入分析
                                </label>
                            </div>
                        </div>
                    </div>
                </div>
            </resource-drawer>
        </div>
    `,
    controller: ['Api', 'Activity', 'ClusterStore', 'Confirm', 'Format', 'Preferences', 'Toast', '$scope', '$timeout',
        function (Api, Activity, ClusterStore, Confirm, Format, Preferences, Toast, $scope, $timeout) {
            var ctrl = this;
            var destroyed = false;
            var pollTimer = null;
            var requestSequence = 0;
            var mutationSequence = 0;
            var activeJobId = '';
            var flameRowsLimit = 100;

            ctrl.resultTabs = [
                {id: 'summary', label: 'Summary', icon: 'grid'},
                {id: 'type', label: 'Types', icon: 'database'},
                {id: 'db', label: 'DBs', icon: 'database'},
                {id: 'prefix', label: 'Prefixes', icon: 'columns'},
                {id: 'big', label: 'Big Keys', icon: 'analysis'},
                {id: 'hot', label: 'Hot Keys', icon: 'activity'},
                {id: 'flame', label: 'Flamegraph', icon: 'analysis'}
            ];
            ctrl.activeResultTab = 'summary';
            ctrl.resultTab = 'summary';
            ctrl.job = null;
            ctrl.hasSnapshot = false;
            ctrl.initialLoading = false;
            ctrl.refreshing = false;
            ctrl.polling = false;
            ctrl.refreshError = '';
            ctrl.refreshFeedback = '';
            ctrl.operationError = '';
            ctrl.lastUpdated = null;
            ctrl.summaryMetrics = [];
            ctrl.summaryFields = [];
            ctrl.flameRows = [];
            ctrl.remoteServers = [];
            ctrl.selectedRemoteServer = '';
            ctrl.drawerOpen = false;
            ctrl.moreOpen = false;
            ctrl.submitting = false;
            ctrl.confirming = false;
            ctrl.actionPending = false;
            ctrl.drawerError = '';
            ctrl.fieldErrors = {};
            ctrl.form = newForm();
            ctrl.cluster = ClusterStore.state(ctrl.product || 'default');

            function newForm() {
                return {
                    source: 'upload',
                    file: null,
                    fileName: '',
                    path: '',
                    server_addr: '',
                    options: {
                        top_n: '20',
                        prefix_separators: ':',
                        max_depth: '3',
                        regex: '',
                        include_expired: false
                    }
                };
            }

            function present(value) {
                return value !== undefined && value !== null && value !== '';
            }

            function preferenceKey(product) {
                return 'rdb-analysis.' + product + '.job-id';
            }

            function readSavedJobId(product) {
                if (!product) { return ''; }
                var saved = Preferences.get(preferenceKey(product), '');
                if (typeof saved !== 'string' && typeof saved !== 'number') { return ''; }
                return String(saved).trim();
            }

            function saveJobId(product, id) {
                if (product) {
                    Preferences.set(preferenceKey(product), id || '');
                }
            }

            function isTerminal(status) {
                return status === 'done' || status === 'error' || status === 'canceled';
            }
            ctrl.isTerminal = isTerminal;

            function normalizeJob(data, requestedId) {
                data = angular.isObject(data) ? data : {};
                return angular.extend({}, data, {
                    id: present(data.id) ? String(data.id) : requestedId,
                    status: present(data.status) ? String(data.status) : 'queued',
                    options: angular.isObject(data.options) ? data.options : {},
                    type_summary: angular.isArray(data.type_summary) ? data.type_summary : [],
                    db_summary: angular.isArray(data.db_summary) ? data.db_summary : [],
                    prefix_summary: angular.isArray(data.prefix_summary) ? data.prefix_summary : [],
                    top_big_keys: angular.isArray(data.top_big_keys) ? data.top_big_keys : [],
                    top_hot_keys: angular.isArray(data.top_hot_keys) ? data.top_hot_keys : [],
                    flamegraph: data.flamegraph || null
                });
            }

            ctrl.number = function (value) {
                return present(value) && !isNaN(Number(value)) ? Format.number(value) : '—';
            };
            ctrl.bytes = function (value) {
                return present(value) && !isNaN(Number(value)) ? Format.bytes(value) : '—';
            };
            ctrl.text = function (value) {
                return present(value) ? String(value) : '—';
            };
            ctrl.date = function (value) {
                return present(value) ? Format.date(value, true) : '—';
            };
            ctrl.readable = function (row) {
                return row && present(row.size_readable) ? String(row.size_readable) : ctrl.bytes(row && row.size);
            };

            ctrl.progressPercent = function () {
                var job = ctrl.job;
                if (!job) { return 0; }
                var fileSize = Number(job.file_size);
                var bytesRead = Number(job.bytes_read);
                var progress = 0;
                if (present(job.file_size) && present(job.bytes_read) &&
                    isFinite(fileSize) && isFinite(bytesRead) && fileSize > 0) {
                    progress = Math.floor(bytesRead * 100 / fileSize);
                } else if (job.status === 'done') {
                    progress = 100;
                }
                return Math.max(0, Math.min(100, isFinite(progress) ? progress : 0));
            };

            ctrl.progressBarClass = function () {
                if (!ctrl.job) { return ''; }
                if (ctrl.job.status === 'done') { return 'is-success'; }
                if (ctrl.job.status === 'error') { return 'is-error'; }
                if (ctrl.job.status === 'canceled') { return 'is-warning'; }
                return '';
            };

            function buildFlameRows(root) {
                var rows = [];
                function walk(node, depth, path) {
                    if (!node || rows.length >= flameRowsLimit) { return; }
                    var nodeName = present(node.name) ? String(node.name) : '';
                    var fullName = path ? path + '/' + nodeName : nodeName;
                    if (depth > 0) {
                        rows.push({
                            name: fullName,
                            depth: depth - 1,
                            size: present(node.value) ? node.value : 0,
                            size_readable: ctrl.bytes(present(node.value) ? node.value : 0)
                        });
                    }
                    var children = (angular.isArray(node.children) ? node.children : []).slice(0);
                    children.sort(function (left, right) {
                        var leftSize = Number(left && left.value) || 0;
                        var rightSize = Number(right && right.value) || 0;
                        if (leftSize !== rightSize) { return rightSize - leftSize; }
                        return String(left && left.name || '').localeCompare(String(right && right.name || ''));
                    });
                    for (var index = 0; index < children.length && rows.length < flameRowsLimit; index++) {
                        walk(children[index], depth + 1, fullName);
                    }
                }
                walk(root, 0, '');
                return rows;
            }

            function updateView() {
                var job = ctrl.job;
                ctrl.flameRows = job ? buildFlameRows(job.flamegraph) : [];
                ctrl.summaryFields = [];
                ctrl.summaryMetrics = [];
                if (!job) { return; }
                var options = job.options || {};
                ctrl.summaryMetrics = [
                    {key: 'status', title: '任务状态', value: job.status || 'unknown', caption: 'status', icon: 'activity', status: job.status || 'unknown'},
                    {key: 'progress', title: '读取进度', value: ctrl.progressPercent(), unit: '%', caption: 'bytes_read / file_size，限制为 0–100', icon: 'analysis', theme: 'primary'},
                    {key: 'read', title: '已读取', value: ctrl.bytes(job.bytes_read), caption: 'bytes_read', icon: 'file'},
                    {key: 'file', title: '文件大小', value: ctrl.bytes(job.file_size), caption: 'file_size', icon: 'database'}
                ];
                ctrl.summaryFields = [
                    {key: 'id', label: 'Job ID', value: ctrl.text(job.id)},
                    {key: 'status', label: 'status', value: ctrl.text(job.status)},
                    {key: 'progress', label: 'progress', value: ctrl.progressPercent() + '%', numeric: true},
                    {key: 'source', label: 'source', value: ctrl.text(job.source)},
                    {key: 'created', label: 'created_at', value: ctrl.date(job.created_at)},
                    {key: 'updated', label: 'updated_at', value: ctrl.date(job.updated_at)},
                    {key: 'read', label: 'bytes_read', value: ctrl.number(job.bytes_read) + ' / ' + ctrl.bytes(job.bytes_read), numeric: true},
                    {key: 'file', label: 'file_size', value: ctrl.number(job.file_size) + ' / ' + ctrl.bytes(job.file_size), numeric: true},
                    {key: 'objects', label: 'objects_read', value: ctrl.number(job.objects_read), numeric: true},
                    {key: 'dbs', label: 'db_count', value: ctrl.number(job.db_count), numeric: true},
                    {key: 'total', label: 'total_size', value: ctrl.number(job.total_size) + ' / ' + ctrl.bytes(job.total_size), numeric: true},
                    {key: 'top-n', label: 'options.top_n', value: ctrl.number(options.top_n), numeric: true},
                    {key: 'separators', label: 'options.prefix_separators', value: angular.isArray(options.prefix_separators) ?
                        (options.prefix_separators.length ? options.prefix_separators.join(', ') : '—') : ctrl.text(options.prefix_separators)},
                    {key: 'depth', label: 'options.max_depth', value: ctrl.number(options.max_depth), numeric: true},
                    {key: 'regex', label: 'options.regex', value: present(options.regex) ? String(options.regex) : '—'},
                    {key: 'expired', label: 'options.include_expired', value: options.include_expired === true ? 'true' :
                        (options.include_expired === false ? 'false' : '—')},
                    {key: 'error', label: 'error', value: ctrl.text(job.error)}
                ];
            }

            ctrl.resultCount = function (tab) {
                if (tab === 'flame') { return ctrl.flameRows.length; }
                if (tab === 'summary') { return ctrl.summaryFields.length; }
                var field = {
                    type: 'type_summary', db: 'db_summary', prefix: 'prefix_summary',
                    big: 'top_big_keys', hot: 'top_hot_keys'
                }[tab];
                return field && ctrl.job && angular.isArray(ctrl.job[field]) ? ctrl.job[field].length : 0;
            };

            ctrl.setResultTab = function (tab) {
                var valid = ctrl.resultTabs.some(function (item) { return item.id === tab; });
                if (valid) {
                    ctrl.activeResultTab = tab;
                    ctrl.resultTab = tab;
                }
            };

            ctrl.busy = function () {
                return ctrl.submitting || ctrl.confirming || ctrl.actionPending;
            };

            function stopPolling() {
                if (pollTimer !== null) {
                    $timeout.cancel(pollTimer);
                    pollTimer = null;
                }
            }

            function loadIsCurrent(product, id, sequence) {
                return !destroyed && ctrl.product === product && activeJobId === id && requestSequence === sequence;
            }

            function schedulePoll(product, id, sequence) {
                if (!loadIsCurrent(product, id, sequence) || !ctrl.job || isTerminal(ctrl.job.status)) { return; }
                stopPolling();
                pollTimer = $timeout(function () {
                    pollTimer = null;
                    if (loadIsCurrent(product, id, sequence)) {
                        loadJob({background: true});
                    }
                }, 1500);
            }

            function loadJob(options) {
                options = options || {};
                var product = ctrl.product;
                var id = activeJobId;
                if (destroyed || !product || !id) { return; }
                if (options.manual && ctrl.busy()) { return; }
                stopPolling();
                var sequence = ++requestSequence;
                ctrl.initialLoading = !ctrl.hasSnapshot;
                ctrl.refreshing = !!options.manual;
                ctrl.polling = !!options.background && ctrl.hasSnapshot;
                if (options.manual) {
                    ctrl.refreshError = '';
                    ctrl.refreshFeedback = '正在手动刷新当前任务…';
                }

                var path = '/api/topom/rdb-analysis/{xauth}/' + Api.segment(id);
                return Api.request('GET', product, path).then(function (data) {
                    if (!loadIsCurrent(product, id, sequence)) { return; }
                    ctrl.job = normalizeJob(data, id);
                    ctrl.hasSnapshot = true;
                    ctrl.lastUpdated = new Date();
                    ctrl.refreshError = '';
                    ctrl.refreshFeedback = (options.manual ? '手动刷新完成' : '任务已更新') + ' · ' + Format.time(ctrl.lastUpdated);
                    updateView();
                    if (options.manual) { Toast.success('RDB 分析任务已刷新'); }
                    schedulePoll(product, id, sequence);
                }).catch(function (response) {
                    if (!loadIsCurrent(product, id, sequence)) { return; }
                    var message = Api.errorMessage(response);
                    ctrl.refreshError = message;
                    ctrl.refreshFeedback = options.manual ? '手动刷新失败，已保留当前内容' : '后台刷新失败，已保留当前内容';
                    if (options.manual) { Toast.error('刷新 RDB 分析任务失败：' + message); }
                    schedulePoll(product, id, sequence);
                }).finally(function () {
                    if (!loadIsCurrent(product, id, sequence)) { return; }
                    ctrl.initialLoading = false;
                    ctrl.refreshing = false;
                    ctrl.polling = false;
                });
            }

            function activateJob(id) {
                id = String(id || '').trim();
                if (!id || !ctrl.product) { return; }
                stopPolling();
                requestSequence++;
                activeJobId = id;
                ctrl.job = normalizeJob({id: id, status: 'queued'}, id);
                ctrl.hasSnapshot = false;
                ctrl.initialLoading = true;
                ctrl.refreshing = false;
                ctrl.polling = false;
                ctrl.refreshError = '';
                ctrl.refreshFeedback = '';
                ctrl.operationError = '';
                ctrl.lastUpdated = null;
                ctrl.activeResultTab = 'summary';
                ctrl.resultTab = 'summary';
                saveJobId(ctrl.product, id);
                updateView();
                return loadJob({background: true});
            }

            ctrl.refresh = function () {
                if (!ctrl.job || ctrl.refreshing || ctrl.busy()) { return; }
                return loadJob({manual: true});
            };

            function syncCluster(state) {
                if (destroyed || !state || state.product !== ctrl.product) { return; }
                ctrl.cluster = state;
                var servers = [];
                (state.groups || []).forEach(function (group) {
                    (group.servers || []).forEach(function (server, index) {
                        if (!server.server) { return; }
                        servers.push({
                            address: server.server,
                            label: 'Group-' + group.id + ' · ' + server.server + (index === 0 ? ' · Master' : ' · Replica')
                        });
                    });
                });
                ctrl.remoteServers = servers;
            }

            function loadCluster() {
                var product = ctrl.product;
                if (!product) { return; }
                ClusterStore.load(product).then(function (state) {
                    if (!destroyed && ctrl.product === product) { syncCluster(state); }
                }).catch(angular.noop);
            }

            function parseInteger(value, minimum, maximum) {
                var text = String(present(value) ? value : '').trim();
                if (!/^\d+$/.test(text)) { return null; }
                var parsed = Number(text);
                if (!isFinite(parsed) || Math.floor(parsed) !== parsed || parsed < minimum || parsed > maximum) {
                    return null;
                }
                return parsed;
            }

            function buildOptions(options, errors) {
                var topN = parseInteger(options.top_n, 1, 100);
                var maxDepth = parseInteger(options.max_depth, 0, 16);
                if (topN === null) { errors.top_n = 'top_n 必须是 1 到 100 的整数'; }
                if (maxDepth === null) { errors.max_depth = 'max_depth 必须是 0 到 16 的整数'; }
                var separators = String(options.prefix_separators || '').split(',').map(function (part) {
                    return part.trim();
                }).filter(function (part) {
                    return part !== '';
                });
                return {
                    top_n: topN,
                    prefix_separators: separators,
                    max_depth: maxDepth,
                    regex: String(options.regex || ''),
                    include_expired: options.include_expired === true
                };
            }

            function workspacePathError(value) {
                var path = String(value || '').trim();
                if (!path) { return '请选择或输入 workspace 中的 RDB 相对路径'; }
                if (path.charAt(0) === '/' || path.charAt(0) === '\\' || /^[A-Za-z]:[\\/]/.test(path)) {
                    return 'workspace 路径必须是相对路径，不能是绝对路径';
                }
                if (path.indexOf('\0') >= 0 || path.split(/[\\/]+/).some(function (part) { return part === '..'; })) {
                    return 'workspace 路径不能包含空字符或 .. 越界路径段';
                }
                if (path === '.' || /[\\/]\.$/.test(path)) { return '请输入 RDB 文件路径，而不是目录'; }
                return '';
            }

            function serverAddressError(value) {
                var address = String(value || '').trim();
                if (!address) { return '请输入 server_addr，或从 Group Server 中选择'; }
                if (/\s/.test(address) || /:\/\/|[/?#]/.test(address)) {
                    return 'server_addr 只能是 host:port，不能包含空白、URL、路径或查询参数';
                }
                var match;
                if (address.charAt(0) === '[') {
                    match = address.match(/^\[([0-9A-Fa-f:.]+)\]:(\d+)$/);
                    if (!match || match[1].indexOf(':') < 0) { return 'IPv6 server_addr 必须使用 [address]:port'; }
                } else {
                    match = address.match(/^([^:]+):(\d+)$/);
                    if (!match || !match[1]) { return 'server_addr 必须是有效的 host:port'; }
                }
                var port = Number(match[2]);
                if (!isFinite(port) || Math.floor(port) !== port || port < 1 || port > 65535) {
                    return 'server_addr 端口必须是 1 到 65535 的整数';
                }
                return '';
            }

            ctrl.openDrawer = function () {
                if (ctrl.busy() || destroyed || !ctrl.product) { return; }
                ctrl.drawerError = '';
                ctrl.fieldErrors = {};
                ctrl.operationError = '';
                ctrl.drawerOpen = true;
                loadCluster();
            };

            ctrl.closeDrawer = function () {
                if (ctrl.submitting) { return; }
                ctrl.drawerOpen = false;
                ctrl.drawerError = '';
                ctrl.fieldErrors = {};
            };

            ctrl.setSource = function (source) {
                if (ctrl.submitting || ['upload', 'workspace', 'remote'].indexOf(source) < 0) { return; }
                ctrl.form.source = source;
                ctrl.drawerError = '';
                delete ctrl.fieldErrors.file;
                delete ctrl.fieldErrors.path;
                delete ctrl.fieldErrors.server_addr;
            };

            ctrl.clearFieldError = function (field) {
                if (ctrl.submitting) { return; }
                delete ctrl.fieldErrors[field];
                ctrl.drawerError = '';
            };

            ctrl.fileChanged = function (input) {
                var file = input && input.files && input.files.length ? input.files[0] : null;
                $scope.$evalAsync(function () {
                    if (destroyed || ctrl.submitting) { return; }
                    ctrl.form.file = file;
                    ctrl.form.fileName = file ? file.name : '';
                    delete ctrl.fieldErrors.file;
                    ctrl.drawerError = '';
                });
            };

            ctrl.fileText = function () {
                var file = ctrl.form && ctrl.form.file;
                if (!file) { return ''; }
                return String(file.name || ctrl.form.fileName || 'RDB 文件') + ' · ' + ctrl.bytes(file.size);
            };

            ctrl.chooseRemoteServer = function () {
                if (ctrl.selectedRemoteServer) {
                    ctrl.form.server_addr = ctrl.selectedRemoteServer;
                }
                delete ctrl.fieldErrors.server_addr;
                ctrl.drawerError = '';
            };

            ctrl.submit = function () {
                if (ctrl.submitting || destroyed || !ctrl.product) { return; }
                var product = ctrl.product;
                var form = ctrl.form;
                var errors = {};
                var options = buildOptions(form.options, errors);
                var path;
                var body;
                var config;

                if (form.source === 'upload') {
                    if (!form.file) { errors.file = '请选择要上传的 RDB 文件'; }
                    path = '/api/topom/rdb-analysis/upload/{xauth}';
                    body = new FormData();
                    if (form.file) { body.append('file', form.file); }
                    body.append('options', angular.toJson(options));
                    config = {
                        transformRequest: angular.identity,
                        headers: {'Content-Type': undefined}
                    };
                } else if (form.source === 'workspace') {
                    errors.path = workspacePathError(form.path);
                    path = '/api/topom/rdb-analysis/start/{xauth}';
                    body = {path: String(form.path || '').trim(), options: options};
                } else if (form.source === 'remote') {
                    errors.server_addr = serverAddressError(form.server_addr);
                    path = '/api/topom/rdb-analysis/remote-fetch/{xauth}';
                    body = {server_addr: String(form.server_addr || '').trim(), options: options};
                } else {
                    return;
                }

                Object.keys(errors).forEach(function (key) {
                    if (!errors[key]) { delete errors[key]; }
                });
                ctrl.fieldErrors = errors;
                if (Object.keys(errors).length) {
                    ctrl.drawerError = '请修正表单中的错误后重试；当前输入已保留';
                    return;
                }

                var sequence = ++mutationSequence;
                ctrl.submitting = true;
                ctrl.drawerError = '';
                ctrl.operationError = '';
                var method = form.source === 'upload' ? 'POST' : 'PUT';
                return Api.request(method, product, path, body, config).then(function (data) {
                    if (destroyed || sequence !== mutationSequence || product !== ctrl.product) { return; }
                    var id = data && present(data.id) ? String(data.id).trim() : '';
                    if (!id) {
                        ctrl.drawerError = 'Dashboard 创建响应缺少任务 ID，请刷新后重试；当前输入已保留';
                        Toast.error(ctrl.drawerError);
                        return;
                    }
                    ctrl.drawerOpen = false;
                    ctrl.drawerError = '';
                    ctrl.fieldErrors = {};
                    ctrl.form = newForm();
                    ctrl.selectedRemoteServer = '';
                    ctrl.moreOpen = false;
                    Activity.add(product, '新建 RDB 分析', id, 'success');
                    Toast.success('RDB 分析任务已创建，正在获取进度');
                    activateJob(id);
                }).catch(function (response) {
                    if (destroyed || sequence !== mutationSequence || product !== ctrl.product) { return; }
                    var message = Api.errorMessage(response);
                    ctrl.drawerError = message;
                    Activity.add(product, '新建 RDB 分析', '任务创建', 'error');
                    Toast.error('创建 RDB 分析任务失败：' + message);
                }).finally(function () {
                    if (destroyed || sequence !== mutationSequence || product !== ctrl.product) { return; }
                    ctrl.submitting = false;
                });
            };

            function mutationIsCurrent(product, id, sequence) {
                return !destroyed && ctrl.product === product && activeJobId === id && mutationSequence === sequence;
            }

            // Releasing the pending flag must not depend on activeJobId: a successful
            // remove clears it, and the user may switch jobs while the request runs.
            function mutationOwnsSequence(product, sequence) {
                return !destroyed && ctrl.product === product && mutationSequence === sequence;
            }

            function runJobAction(kind, product, id) {
                if (ctrl.actionPending || ctrl.submitting || destroyed) { return; }
                var sequence = ++mutationSequence;
                ctrl.actionPending = true;
                ctrl.operationError = '';
                var path = '/api/topom/rdb-analysis/' + (kind === 'cancel' ? 'cancel' : 'remove') +
                    '/{xauth}/' + Api.segment(id);
                return Api.request('PUT', product, path).then(function () {
                    if (!mutationIsCurrent(product, id, sequence)) { return; }
                    Activity.add(product, kind === 'cancel' ? '取消 RDB 分析' : '清除 RDB 分析', id, 'success');
                    if (kind === 'cancel') {
                        Toast.success('取消请求已提交，正在刷新任务状态');
                        loadJob({background: true});
                    } else {
                        stopPolling();
                        requestSequence++;
                        activeJobId = '';
                        ctrl.job = null;
                        ctrl.hasSnapshot = false;
                        ctrl.initialLoading = false;
                        ctrl.refreshing = false;
                        ctrl.polling = false;
                        ctrl.refreshError = '';
                        ctrl.refreshFeedback = '';
                        ctrl.operationError = '';
                        ctrl.lastUpdated = null;
                        ctrl.activeResultTab = 'summary';
                        ctrl.resultTab = 'summary';
                        saveJobId(product, '');
                        updateView();
                        Toast.success('RDB 分析任务已清除');
                    }
                }).catch(function (response) {
                    if (!mutationIsCurrent(product, id, sequence)) { return; }
                    var prefix = kind === 'cancel' ? '取消 RDB 分析任务失败：' : '清除 RDB 分析任务失败：';
                    var message = prefix + Api.errorMessage(response);
                    ctrl.operationError = message;
                    Activity.add(product, kind === 'cancel' ? '取消 RDB 分析' : '清除 RDB 分析', id, 'error');
                    Toast.error(message);
                }).finally(function () {
                    if (!mutationOwnsSequence(product, sequence)) { return; }
                    ctrl.actionPending = false;
                });
            }

            function confirmJobAction(kind) {
                if (ctrl.busy() || destroyed || !ctrl.job || !activeJobId) { return; }
                var product = ctrl.product;
                var id = activeJobId;
                var removing = kind === 'remove';
                ctrl.confirming = true;
                ctrl.operationError = '';
                return Confirm.open({
                    title: removing ? '清除 RDB 分析任务？' : '取消 RDB 分析任务？',
                    message: removing ?
                        (isTerminal(ctrl.job.status) ? '将删除任务及其已上传的临时文件，当前分析结果也会从页面移除。' :
                            '任务仍在运行，清除会同时请求取消任务并删除记录及临时文件。') :
                        '将请求停止当前 RDB 分析，已生成的任务记录和结果会保留。',
                    details: 'Job ID: ' + id,
                    confirmText: removing ? '确认清除' : '确认取消',
                    danger: true
                }).then(function () {
                    if (destroyed || ctrl.product !== product || activeJobId !== id) { return; }
                    ctrl.confirming = false;
                    return runJobAction(kind, product, id);
                }, function () {
                    if (!destroyed && ctrl.product === product && activeJobId === id) {
                        ctrl.confirming = false;
                    }
                });
            }

            ctrl.cancelJob = function () {
                if (!ctrl.job || isTerminal(ctrl.job.status)) { return; }
                return confirmJobAction('cancel');
            };
            ctrl.removeJob = function () {
                return confirmJobAction('remove');
            };

            function resetForProduct() {
                stopPolling();
                requestSequence++;
                mutationSequence++;
                activeJobId = '';
                ctrl.job = null;
                ctrl.hasSnapshot = false;
                ctrl.initialLoading = false;
                ctrl.refreshing = false;
                ctrl.polling = false;
                ctrl.refreshError = '';
                ctrl.refreshFeedback = '';
                ctrl.operationError = '';
                ctrl.lastUpdated = null;
                ctrl.activeResultTab = 'summary';
                ctrl.resultTab = 'summary';
                ctrl.drawerOpen = false;
                ctrl.moreOpen = false;
                ctrl.submitting = false;
                ctrl.confirming = false;
                ctrl.actionPending = false;
                ctrl.drawerError = '';
                ctrl.fieldErrors = {};
                ctrl.form = newForm();
                ctrl.selectedRemoteServer = '';
                ctrl.remoteServers = [];
                ctrl.cluster = ClusterStore.state(ctrl.product || 'default');
                updateView();
                loadCluster();
                var savedId = readSavedJobId(ctrl.product);
                if (savedId) { activateJob(savedId); }
            }

            ctrl.$onInit = function () {
                resetForProduct();
            };

            ctrl.$onChanges = function (changes) {
                if (changes.product && !changes.product.isFirstChange() &&
                    changes.product.currentValue !== changes.product.previousValue) {
                    resetForProduct();
                }
            };

            var offRefreshRequested = $scope.$on('cluster:refreshRequested', function (event, payload) {
                if (!destroyed && payload && payload.product === ctrl.product && activeJobId &&
                    !ctrl.refreshing && !ctrl.busy()) {
                    loadJob({manual: true});
                }
            });
            var offClusterUpdated = $scope.$on('cluster:updated', function (event, payload) {
                if (!destroyed && payload && payload.product === ctrl.product) {
                    syncCluster(payload.state);
                }
            });

            ctrl.$onDestroy = function () {
                destroyed = true;
                stopPolling();
                requestSequence++;
                mutationSequence++;
                activeJobId = '';
                offRefreshRequested();
                offClusterUpdated();
                ctrl.form = newForm();
                ctrl.remoteServers = [];
            };
        }
    ]
});
