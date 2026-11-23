'use strict';

dashboard.component('migrationPage', {
    bindings: {
        product: '<'
    },
    template: `
        <div class="migration-page">
            <page-header title="数据迁移"
                description="基于 RedisShake 内核，支持单机 Redis / 集群 Redis / Codis 之间迁入迁出，并跟踪全量、增量与 SCAN 任务的阶段、进度和运行日志。">
                <button type="button" class="button button-primary" ng-click="$ctrl.newTask()"
                    ng-disabled="$ctrl.submitting || $ctrl.actionSubmitting || $ctrl.confirming || $ctrl.featureDisabled">
                    <ui-icon name="plus" size="16"></ui-icon>
                    <span>新增数据迁移</span>
                </button>
            </page-header>

            <loading-skeleton ng-if="$ctrl.initialLoading" variant="page"></loading-skeleton>

            <div class="inline-alert inline-alert--warning" role="alert"
                ng-if="!$ctrl.initialLoading && $ctrl.featureDisabled">
                <ui-icon name="alert" size="18"></ui-icon>
                <div>
                    <strong>数据迁移功能未启用</strong>
                    <p>Dashboard 返回：{{$ctrl.disabledMessage || $ctrl.loadError}}</p>
                    <p>请在 Dashboard 配置中设置 <code>data_migration_enabled = true</code>，重启或重载配置后再试。</p>
                    <button type="button" class="button button-secondary" ng-click="$ctrl.retry()"
                        ng-disabled="$ctrl.requestInFlight">重新检查</button>
                </div>
            </div>

            <empty-state ng-if="!$ctrl.initialLoading && !$ctrl.featureDisabled && !$ctrl.loaded && $ctrl.loadError"
                icon="alert" title="{{$ctrl.forbidden ? '没有数据迁移访问权限（403）' : '无法获取数据迁移任务'}}"
                description="{{$ctrl.loadError}}" action-label="重新加载" on-action="$ctrl.retry()"></empty-state>

            <div ng-if="$ctrl.loaded && !$ctrl.featureDisabled">
                <section class="panel" aria-label="数据迁移任务摘要">
                    <div class="card-header">
                        <div>
                            <h2 class="card-title">任务摘要</h2>
                            <p>统计当前集群的全部数据迁移任务；存在非终态任务时每 2 秒自动刷新。</p>
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

                <section class="panel" aria-label="数据迁移任务列表" style="margin-top:20px">
                    <div class="card-header">
                        <div>
                            <h2 class="card-title">迁移任务</h2>
                            <p>点击任务名称查看端点、实时进度与 Worker 运行日志。</p>
                        </div>
                        <span class="form-hint">共 {{$ctrl.jobs.length}} 个任务</span>
                    </div>

                    <div class="filter-bar">
                        <label class="filter-search">
                            <span class="sr-only">搜索数据迁移任务</span>
                            <ui-icon name="search" size="17"></ui-icon>
                            <input type="search" ng-model="$ctrl.filters.search" ng-change="$ctrl.filtersChanged()"
                                placeholder="搜索名称、地址、状态或 Job ID" autocomplete="off">
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
                        selectable="false" row-key="id" empty-title="暂无数据迁移任务"
                        empty-description="当前集群还没有数据迁移任务。"
                        on-retry="$ctrl.retry()" on-open="$ctrl.openDetail(row)"
                        on-row-action="$ctrl.rowAction(row, action)"></codis-data-table>

                    <empty-state ng-if="!$ctrl.jobs.length" icon="link" title="还没有数据迁移任务"
                        description="可以从单机 Redis、集群 Redis 或 Codis 迁入迁出数据；创建后 Dashboard 会启动 RedisShake Worker。"
                        action-label="新增数据迁移" on-action="$ctrl.newTask()"></empty-state>
                    <empty-state ng-if="$ctrl.jobs.length && !$ctrl.filteredRows.length"
                        icon="search" title="没有匹配的任务" description="请调整名称、地址、状态关键词或状态筛选条件。"
                        action-label="清除筛选" on-action="$ctrl.clearFilters()"></empty-state>
                </section>
            </div>

            <resource-drawer open="$ctrl.detailOpen" title="'数据迁移任务详情'"
                subtitle="$ctrl.detailSubtitle()" show-submit="false" width="760"
                error="$ctrl.detailError" on-close="$ctrl.closeDetail()">
                <div ng-if="$ctrl.detailJob">
                    <section class="panel">
                        <div class="card-header">
                            <div>
                                <h2 class="card-title">任务与端点</h2>
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
                            <span class="form-label">任务错误 · error</span>
                            <pre class="code-block" style="white-space: pre-wrap;">{{$ctrl.detailJob.error}}</pre>
                        </div>
                    </section>

                    <section class="panel">
                        <div class="card-header">
                            <div>
                                <h2 class="card-title">实时进度</h2>
                                <p>读取 / 写入统计、RDB 进度与 AOF 延迟；运行中任务随列表轮询更新。</p>
                            </div>
                            <ui-icon name="refresh" size="16" class="is-spinning" ng-if="$ctrl.detailRefreshing"></ui-icon>
                        </div>
                        <dl class="key-value-grid">
                            <div class="key-value-item" ng-repeat="field in $ctrl.progressFields track by field.key">
                                <dt>{{field.label}}</dt><dd ng-attr-title="{{field.value}}">{{field.value}}</dd>
                            </div>
                            <div class="key-value-item">
                                <dt>一致性 · consistent</dt>
                                <dd>
                                    <status-tag status="$ctrl.detailJob.stats.consistent ? 'succeeded' : 'pending'"
                                        label="{{$ctrl.detailJob.stats.consistent ? '已追平 (consistent)' : '尚未追平'}}"></status-tag>
                                </dd>
                            </div>
                        </dl>
                        <div class="form-grid" style="margin-top:14px">
                            <div class="form-group">
                                <span class="form-label">reader · 原始状态</span>
                                <pre class="code-block" style="max-height:220px;">{{$ctrl.prettyJson($ctrl.detailJob.reader)}}</pre>
                            </div>
                            <div class="form-group">
                                <span class="form-label">writer · 原始状态</span>
                                <pre class="code-block" style="max-height:220px;">{{$ctrl.prettyJson($ctrl.detailJob.writer)}}</pre>
                            </div>
                        </div>
                    </section>

                    <div class="inline-alert inline-alert--error" ng-if="$ctrl.hasConsoleTail()" role="alert">
                        <ui-icon name="alert" size="18"></ui-icon>
                        <div>
                            <strong>Worker 控制台输出（stderr / panic）</strong>
                            <pre class="code-block" style="margin-top:8px;max-height:200px;white-space:pre-wrap;">{{$ctrl.consoleTail()}}</pre>
                        </div>
                    </div>

                    <section class="panel">
                        <div class="card-header">
                            <div>
                                <h2 class="card-title">运行日志</h2>
                                <p ng-if="$ctrl.logFile">文件：{{$ctrl.logFile}}</p>
                                <p ng-if="!$ctrl.logFile">任务 log 文件（shake.log），按 offset 增量读取。</p>
                            </div>
                            <div class="card-actions">
                                <span class="form-hint" ng-if="$ctrl.logUpdatedAt">{{$ctrl.dateText($ctrl.logUpdatedAt, true)}}</span>
                                <button type="button" class="button button-secondary" ng-click="$ctrl.refreshLog()"
                                    ng-disabled="$ctrl.logLoading">
                                    <ui-icon name="refresh" size="15" ng-class="{'is-spinning': $ctrl.logLoading}"></ui-icon>
                                    <span>{{$ctrl.logLoading ? '读取中' : '刷新日志'}}</span>
                                </button>
                            </div>
                        </div>
                        <div class="inline-alert inline-alert--error" ng-if="$ctrl.logError" role="alert">
                            <ui-icon name="alert" size="17"></ui-icon>
                            <span>{{$ctrl.logError}}</span>
                        </div>
                        <pre class="code-block" ng-if="$ctrl.logText"
                            style="max-height:320px;white-space:pre-wrap;">{{$ctrl.logText}}</pre>
                        <p class="form-hint" ng-if="$ctrl.logTrimmed">日志较长，仅保留最近 256KB。</p>
                        <p class="form-hint" ng-if="!$ctrl.logText && !$ctrl.logError">日志文件暂时没有内容。</p>
                    </section>
                </div>
            </resource-drawer>

            <resource-drawer open="$ctrl.editorOpen" title="'新增数据迁移任务'"
                subtitle="'RedisShake 内核 · 提交前请确认源端与目标端拓扑'"
                width="760" submitting="$ctrl.submitting"
                submit-disabled="$ctrl.submitting || $ctrl.confirming || $ctrl.submitBlocked()"
                submit-label="创建迁移任务" show-submit="true" error="$ctrl.drawerError"
                on-close="$ctrl.closeEditor()" on-submit="$ctrl.submitCreate()">
                <div class="form-grid">
                    <div class="form-group form-group--full" ng-class="{'has-error': $ctrl.fieldErrors.name}">
                        <label class="form-label" for="migration-name">任务名称 <span class="required">必填</span></label>
                        <input id="migration-name" class="form-control" type="text" maxlength="64"
                            ng-model="$ctrl.editor.name" ng-change="$ctrl.clearEditorError('name')"
                            ng-disabled="$ctrl.submitting" placeholder="例如 订单 Redis → Codis 集群"
                            autocomplete="off" spellcheck="false" required>
                        <p class="form-hint">name，最多 64 个字符；创建后可在任务列表与操作记录中识别。</p>
                        <p class="form-error" ng-if="$ctrl.fieldErrors.name" role="alert">{{$ctrl.fieldErrors.name}}</p>
                    </div>
                    <div class="form-group form-group--full" ng-class="{'has-error': $ctrl.fieldErrors.mode}">
                        <label class="form-label" for="migration-mode">迁移模式</label>
                        <select id="migration-mode" class="form-control" ng-model="$ctrl.editor.mode"
                            ng-options="option.value as option.label for option in $ctrl.modeOptions"
                            ng-change="$ctrl.modeChanged()" ng-disabled="$ctrl.submitting"></select>
                        <p class="form-hint">{{$ctrl.modeHint()}}</p>
                        <p class="form-error" ng-if="$ctrl.fieldErrors.mode" role="alert">{{$ctrl.fieldErrors.mode}}</p>
                    </div>
                </div>

                <section class="panel" style="margin-top:14px">
                    <div class="card-header">
                        <div>
                            <h2 class="card-title">源端</h2>
                            <p>数据来源；单机 / 集群 Redis 需要地址，Codis 使用当前集群拓扑或手动分片。</p>
                        </div>
                    </div>
                    <div class="form-grid">
                        <div class="form-group form-group--full">
                            <label class="form-label" for="migration-source-kind">源端类型</label>
                            <select id="migration-source-kind" class="form-control" ng-model="$ctrl.editor.source.kind"
                                ng-options="option.value as option.label for option in $ctrl.kindOptions"
                                ng-change="$ctrl.sideChanged('source')" ng-disabled="$ctrl.submitting"></select>
                        </div>
                        <div class="form-group form-group--full" ng-if="$ctrl.editor.source.kind !== 'codis'"
                            ng-class="{'has-error': $ctrl.fieldErrors.sourceAddress}">
                            <label class="form-label" for="migration-source-address">源端地址 <span class="required">必填</span></label>
                            <input id="migration-source-address" class="form-control" type="text"
                                ng-model="$ctrl.editor.source.address" ng-change="$ctrl.clearEditorError('sourceAddress')"
                                ng-disabled="$ctrl.submitting" placeholder="host:port" autocomplete="off" spellcheck="false">
                            <p class="form-hint">source.address，例如 10.0.20.1:6379。</p>
                            <p class="form-error" ng-if="$ctrl.fieldErrors.sourceAddress" role="alert">{{$ctrl.fieldErrors.sourceAddress}}</p>
                        </div>
                        <div class="form-group" ng-if="$ctrl.editor.source.kind !== 'codis'">
                            <label class="form-label" for="migration-source-username">源端用户名</label>
                            <input id="migration-source-username" class="form-control" type="text"
                                ng-model="$ctrl.editor.source.username" ng-disabled="$ctrl.submitting"
                                placeholder="可留空" autocomplete="off" spellcheck="false">
                        </div>
                        <div class="form-group" ng-if="$ctrl.editor.source.kind !== 'codis'">
                            <label class="form-label" for="migration-source-password">源端密码</label>
                            <input id="migration-source-password" class="form-control" type="password"
                                ng-model="$ctrl.editor.source.password" ng-disabled="$ctrl.submitting"
                                placeholder="可留空" autocomplete="new-password">
                        </div>
                        <div class="form-group form-group--full" ng-if="$ctrl.editor.source.kind !== 'codis'">
                            <label class="form-hint" for="migration-source-tls">
                                <input id="migration-source-tls" type="checkbox" ng-model="$ctrl.editor.source.tls"
                                    ng-disabled="$ctrl.submitting">
                                tls · 使用 TLS 连接源端
                            </label>
                        </div>
                        <div class="form-group form-group--full" ng-if="$ctrl.editor.source.kind !== 'codis'">
                            <label class="form-hint" for="migration-source-prefer-replica">
                                <input id="migration-source-prefer-replica" type="checkbox"
                                    ng-model="$ctrl.editor.source.preferReplica"
                                    ng-disabled="$ctrl.submitting || $ctrl.editor.mode !== 'sync'">
                                prefer_replica · 从副本读取（仅 sync 模式生效）
                            </label>
                        </div>
                        <div class="form-group form-group--full" ng-if="$ctrl.editor.source.kind === 'codis'">
                            <span class="form-label">源端 Codis 拓扑来源</span>
                            <label class="form-hint" for="migration-source-scope-current">
                                <input id="migration-source-scope-current" type="radio"
                                    ng-model="$ctrl.editor.source.codisScope" value="current"
                                    ng-change="$ctrl.sideChanged('source')" ng-disabled="$ctrl.submitting">
                                codis_scope = current
                            </label>
                            <label class="form-hint" for="migration-source-scope-manual">
                                <input id="migration-source-scope-manual" type="radio"
                                    ng-model="$ctrl.editor.source.codisScope" value="manual"
                                    ng-change="$ctrl.sideChanged('source')" ng-disabled="$ctrl.submitting">
                                codis_scope = manual
                            </label>
                            <p class="form-hint" ng-if="$ctrl.editor.source.codisScope === 'current'">当前集群：自动读取 Group Master 与 slot 拓扑，使用 Dashboard 的后端认证。</p>
                            <p class="form-hint" ng-if="$ctrl.editor.source.codisScope === 'manual'">每行一个源端 Group Master：host:port。</p>
                        </div>
                        <div class="form-group form-group--full"
                            ng-if="$ctrl.editor.source.kind === 'codis' && $ctrl.editor.source.codisScope === 'manual'"
                            ng-class="{'has-error': $ctrl.fieldErrors.sourceShards}">
                            <label class="form-label" for="migration-source-shards">源端分片列表 <span class="required">必填</span></label>
                            <textarea id="migration-source-shards" class="form-control" rows="3"
                                ng-model="$ctrl.editor.source.shardsText" ng-change="$ctrl.clearEditorError('sourceShards')"
                                ng-disabled="$ctrl.submitting" spellcheck="false"
                                placeholder="10.0.21.1:6379&#10;10.0.21.2:6379"></textarea>
                            <p class="form-hint">每行一个 host:port；顺序对应 shard 编号。</p>
                            <p class="form-error" ng-if="$ctrl.fieldErrors.sourceShards" role="alert">{{$ctrl.fieldErrors.sourceShards}}</p>
                        </div>
                    </div>
                </section>

                <section class="panel" style="margin-top:14px">
                    <div class="card-header">
                        <div>
                            <h2 class="card-title">目标端</h2>
                            <p>写入目标；Codis 手动模式需要为每个分片提供 slot 范围。</p>
                        </div>
                    </div>
                    <div class="form-grid">
                        <div class="form-group form-group--full">
                            <label class="form-label" for="migration-target-kind">目标端类型</label>
                            <select id="migration-target-kind" class="form-control" ng-model="$ctrl.editor.target.kind"
                                ng-options="option.value as option.label for option in $ctrl.kindOptions"
                                ng-change="$ctrl.sideChanged('target')" ng-disabled="$ctrl.submitting"></select>
                        </div>
                        <div class="form-group form-group--full" ng-if="$ctrl.editor.target.kind !== 'codis'"
                            ng-class="{'has-error': $ctrl.fieldErrors.targetAddress}">
                            <label class="form-label" for="migration-target-address">目标端地址 <span class="required">必填</span></label>
                            <input id="migration-target-address" class="form-control" type="text"
                                ng-model="$ctrl.editor.target.address" ng-change="$ctrl.clearEditorError('targetAddress')"
                                ng-disabled="$ctrl.submitting" placeholder="host:port" autocomplete="off" spellcheck="false">
                            <p class="form-hint">target.address，例如 10.0.30.9:6380。</p>
                            <p class="form-error" ng-if="$ctrl.fieldErrors.targetAddress" role="alert">{{$ctrl.fieldErrors.targetAddress}}</p>
                        </div>
                        <div class="form-group" ng-if="$ctrl.editor.target.kind !== 'codis'">
                            <label class="form-label" for="migration-target-username">目标端用户名</label>
                            <input id="migration-target-username" class="form-control" type="text"
                                ng-model="$ctrl.editor.target.username" ng-disabled="$ctrl.submitting"
                                placeholder="可留空" autocomplete="off" spellcheck="false">
                        </div>
                        <div class="form-group" ng-if="$ctrl.editor.target.kind !== 'codis'">
                            <label class="form-label" for="migration-target-password">目标端密码</label>
                            <input id="migration-target-password" class="form-control" type="password"
                                ng-model="$ctrl.editor.target.password" ng-disabled="$ctrl.submitting"
                                placeholder="可留空" autocomplete="new-password">
                        </div>
                        <div class="form-group form-group--full" ng-if="$ctrl.editor.target.kind !== 'codis'">
                            <label class="form-hint" for="migration-target-tls">
                                <input id="migration-target-tls" type="checkbox" ng-model="$ctrl.editor.target.tls"
                                    ng-disabled="$ctrl.submitting">
                                tls · 使用 TLS 连接目标端
                            </label>
                        </div>
                        <div class="form-group form-group--full" ng-if="$ctrl.editor.target.kind === 'codis'">
                            <span class="form-label">目标端 Codis 拓扑来源</span>
                            <label class="form-hint" for="migration-target-scope-current">
                                <input id="migration-target-scope-current" type="radio"
                                    ng-model="$ctrl.editor.target.codisScope" value="current"
                                    ng-change="$ctrl.sideChanged('target')" ng-disabled="$ctrl.submitting">
                                codis_scope = current
                            </label>
                            <label class="form-hint" for="migration-target-scope-manual">
                                <input id="migration-target-scope-manual" type="radio"
                                    ng-model="$ctrl.editor.target.codisScope" value="manual"
                                    ng-change="$ctrl.sideChanged('target')" ng-disabled="$ctrl.submitting">
                                codis_scope = manual
                            </label>
                            <p class="form-hint" ng-if="$ctrl.editor.target.codisScope === 'current'">当前集群：自动读取 Group Master 与 slot 拓扑，使用 Dashboard 的后端认证。</p>
                            <p class="form-hint" ng-if="$ctrl.editor.target.codisScope === 'manual'">每行一个 Group Master 及 slot 范围：host:port slot_start-slot_end；所有范围必须覆盖 0-1023。</p>
                        </div>
                        <div class="form-group form-group--full"
                            ng-if="$ctrl.editor.target.kind === 'codis' && $ctrl.editor.target.codisScope === 'manual'"
                            ng-class="{'has-error': $ctrl.fieldErrors.targetShards}">
                            <label class="form-label" for="migration-target-shards">目标端分片列表 <span class="required">必填</span></label>
                            <textarea id="migration-target-shards" class="form-control" rows="3"
                                ng-model="$ctrl.editor.target.shardsText" ng-change="$ctrl.clearEditorError('targetShards')"
                                ng-disabled="$ctrl.submitting" spellcheck="false"
                                placeholder="10.0.21.1:6379 0-340&#10;10.0.21.2:6379 341-682"></textarea>
                            <p class="form-hint">后端要求目标端 slot 范围覆盖 0-1023；格式错误会在创建时被拒绝。</p>
                            <p class="form-error" ng-if="$ctrl.fieldErrors.targetShards" role="alert">{{$ctrl.fieldErrors.targetShards}}</p>
                        </div>
                    </div>
                </section>

                <div class="inline-alert" ng-if="$ctrl.topologyVisible()"
                    ng-class="{'inline-alert--warning': $ctrl.topologyProblem(), 'inline-alert--success': !$ctrl.topologyProblem()}">
                    <ui-icon name="{{$ctrl.topologyProblem() ? 'alert' : 'info'}}" size="18"></ui-icon>
                    <div>
                        <strong>{{$ctrl.topologyTitle()}}</strong>
                        <p>{{$ctrl.topologySummary()}}</p>
                    </div>
                </div>

                <div class="inline-alert inline-alert--warning" ng-if="$ctrl.sameEndpointAddress()" role="alert">
                    <ui-icon name="alert" size="18"></ui-icon>
                    <div>
                        <strong>源端与目标端地址相同</strong>
                        <p>两端都指向 {{$ctrl.editor.source.address}}；迁移会向源实例写入并覆盖数据，请修改后再提交。</p>
                    </div>
                </div>

                <details class="advanced-config">
                    <summary>Key / DB / 命令过滤</summary>
                    <div class="form-grid">
                        <div class="form-group">
                            <label class="form-label" for="migration-allow-key-prefix">allow_key_prefix</label>
                            <input id="migration-allow-key-prefix" class="form-control" type="text"
                                ng-model="$ctrl.editor.filter.allowKeyPrefix" ng-disabled="$ctrl.submitting"
                                placeholder="order:,user:" autocomplete="off" spellcheck="false">
                            <p class="form-hint">多个前缀用英文逗号分隔；留空表示不过滤。</p>
                        </div>
                        <div class="form-group">
                            <label class="form-label" for="migration-block-key-prefix">block_key_prefix</label>
                            <input id="migration-block-key-prefix" class="form-control" type="text"
                                ng-model="$ctrl.editor.filter.blockKeyPrefix" ng-disabled="$ctrl.submitting"
                                placeholder="tmp:,debug:" autocomplete="off" spellcheck="false">
                        </div>
                        <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.allowDb}">
                            <label class="form-label" for="migration-allow-db">allow_db</label>
                            <input id="migration-allow-db" class="form-control" type="text"
                                ng-model="$ctrl.editor.filter.allowDb" ng-change="$ctrl.clearEditorError('allowDb')"
                                ng-disabled="$ctrl.submitting" placeholder="0,1" autocomplete="off" spellcheck="false">
                            <p class="form-hint">DB 编号，用英文逗号分隔。</p>
                            <p class="form-error" ng-if="$ctrl.fieldErrors.allowDb" role="alert">{{$ctrl.fieldErrors.allowDb}}</p>
                        </div>
                        <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.blockDb}">
                            <label class="form-label" for="migration-block-db">block_db</label>
                            <input id="migration-block-db" class="form-control" type="text"
                                ng-model="$ctrl.editor.filter.blockDb" ng-change="$ctrl.clearEditorError('blockDb')"
                                ng-disabled="$ctrl.submitting" placeholder="15" autocomplete="off" spellcheck="false">
                            <p class="form-error" ng-if="$ctrl.fieldErrors.blockDb" role="alert">{{$ctrl.fieldErrors.blockDb}}</p>
                        </div>
                        <div class="form-group">
                            <label class="form-label" for="migration-allow-command">allow_command</label>
                            <input id="migration-allow-command" class="form-control" type="text"
                                ng-model="$ctrl.editor.filter.allowCommand" ng-disabled="$ctrl.submitting"
                                placeholder="GET,SET" autocomplete="off" spellcheck="false">
                        </div>
                        <div class="form-group">
                            <label class="form-label" for="migration-block-command">block_command</label>
                            <input id="migration-block-command" class="form-control" type="text"
                                ng-model="$ctrl.editor.filter.blockCommand" ng-disabled="$ctrl.submitting"
                                placeholder="FLUSHALL,FLUSHDB" autocomplete="off" spellcheck="false">
                            <p class="form-hint">默认拦截 FLUSHALL 与 FLUSHDB。</p>
                        </div>
                    </div>
                </details>

                <details class="advanced-config">
                    <summary>高级选项</summary>
                    <div class="form-grid">
                        <div class="form-group">
                            <label class="form-label" for="migration-restore-behavior">restore_behavior</label>
                            <select id="migration-restore-behavior" class="form-control"
                                ng-model="$ctrl.editor.restoreBehavior"
                                ng-options="option.value as option.label for option in $ctrl.restoreOptions"
                                ng-disabled="$ctrl.submitting"></select>
                        </div>
                        <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.qps}">
                            <label class="form-label" for="migration-target-qps">target_redis_max_qps</label>
                            <input id="migration-target-qps" class="form-control" type="number" min="0" step="1"
                                ng-model="$ctrl.editor.targetRedisMaxQps" ng-change="$ctrl.clearEditorError('qps')"
                                ng-disabled="$ctrl.submitting">
                            <p class="form-hint">写入目标端的最大 QPS；留空或 0 表示使用默认值 300000。</p>
                            <p class="form-error" ng-if="$ctrl.fieldErrors.qps" role="alert">{{$ctrl.fieldErrors.qps}}</p>
                        </div>
                        <div class="form-group" ng-class="{'has-error': $ctrl.fieldErrors.pipeline}">
                            <label class="form-label" for="migration-pipeline">pipeline_count_limit</label>
                            <input id="migration-pipeline" class="form-control" type="number" min="0" step="1"
                                ng-model="$ctrl.editor.pipelineCountLimit"
                                ng-change="$ctrl.clearEditorError('pipeline')" ng-disabled="$ctrl.submitting">
                            <p class="form-hint">写入 pipeline 深度；留空或 0 表示使用默认值 1024。</p>
                            <p class="form-error" ng-if="$ctrl.fieldErrors.pipeline" role="alert">{{$ctrl.fieldErrors.pipeline}}</p>
                        </div>
                        <div class="form-group form-group--full">
                            <label class="form-hint" for="migration-empty-db">
                                <input id="migration-empty-db" type="checkbox"
                                    ng-model="$ctrl.editor.emptyDbBeforeSync" ng-disabled="$ctrl.submitting">
                                empty_db_before_sync · 同步前清空目标端数据库
                            </label>
                        </div>
                    </div>
                    <div class="inline-alert inline-alert--warning" ng-if="$ctrl.editor.emptyDbBeforeSync">
                        <ui-icon name="alert" size="18"></ui-icon>
                        <div>
                            <strong>会先对目标端执行 FLUSHALL</strong>
                            <p>任务启动阶段会清空目标实例（或全部 Codis 分片）的数据，请确认目标端没有仍需保留的数据。</p>
                        </div>
                    </div>
                </details>
            </resource-drawer>
        </div>
    `,
    controller: ['Api', 'Activity', 'Confirm', 'Format', 'Preferences', 'Toast', '$scope', '$timeout',
        function (Api, Activity, Confirm, Format, Preferences, Toast, $scope, $timeout) {
            var ctrl = this;
            var terminalStates = {succeeded: true, failed: true, stopped: true};
            var baseStatusOptions = [
                {value: 'pending', label: '等待处理 · pending'},
                {value: 'starting', label: '启动中 · starting'},
                {value: 'running', label: '运行中 · running'},
                {value: 'succeeded', label: '已完成 · succeeded'},
                {value: 'failed', label: '失败 · failed'},
                {value: 'stopped', label: '已停止 · stopped'}
            ];
            var phaseLabels = {
                'hand shaking': '握手',
                'waiting bgsave': '等待 BGSAVE',
                'receiving rdb': '接收 RDB',
                'syncing rdb': '写入 RDB',
                'syncing aof': '增量同步',
                'scanning': '扫描中',
                'draining': '收尾',
                '': '—'
            };
            var modeLabels = {sync: '全量+增量', scan: 'SCAN 全量', rdb: '仅 RDB'};
            var kindLabels = {
                standalone: '单机 Redis',
                cluster: '集群 Redis',
                codis: 'Codis 集群',
                static: 'Codis 集群（静态分片拓扑）'
            };
            var restoreLabels = {
                rewrite: '覆盖同名 key（rewrite）',
                skip: '跳过已存在的 key（skip）',
                panic: '遇到同名 key 失败（panic）'
            };
            var maxLogBytes = 262144;
            var destroyed = false;
            var activated = false;
            var loadSequence = 0;
            var detailSequence = 0;
            var mutationSequence = 0;
            var topologySequence = 0;
            var logSequence = 0;
            var requestVersion = 0;
            var detailVersion = 0;
            var mutationJobId = null;
            var pendingSelectId = '';
            var pollTimer = null;
            var logPollTimer = null;
            var logNextOffset = 0;
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
            ctrl.featureDisabled = false;
            ctrl.disabledMessage = '';
            ctrl.lastUpdated = null;
            ctrl.modeOptions = [
                {value: 'sync', label: '全量+增量 · sync'},
                {value: 'scan', label: 'SCAN 全量 · scan'},
                {value: 'rdb', label: '仅 RDB · rdb'}
            ];
            ctrl.kindOptions = [
                {value: 'standalone', label: '单机 Redis'},
                {value: 'cluster', label: '集群 Redis'},
                {value: 'codis', label: 'Codis 集群'}
            ];
            ctrl.restoreOptions = [
                {value: 'rewrite', label: 'rewrite · 覆盖同名 key（默认）'},
                {value: 'skip', label: 'skip · 跳过已存在的 key'},
                {value: 'panic', label: 'panic · 遇到同名 key 立即失败'}
            ];
            ctrl.editor = newEditor();
            ctrl.editorOpen = false;
            ctrl.fieldErrors = {};
            ctrl.drawerError = '';
            ctrl.submitting = false;
            ctrl.actionSubmitting = false;
            ctrl.confirming = false;
            ctrl.activeMutation = '';
            ctrl.topology = null;
            ctrl.topologyError = '';
            ctrl.topologyLoading = false;
            ctrl.detailOpen = false;
            ctrl.detailJob = null;
            ctrl.detailFields = [];
            ctrl.progressFields = [];
            ctrl.detailError = '';
            ctrl.detailRefreshing = false;
            ctrl.activeDetailId = '';
            ctrl.logText = '';
            ctrl.logFile = '';
            ctrl.logError = '';
            ctrl.logLoading = false;
            ctrl.logTrimmed = false;
            ctrl.logUpdatedAt = null;
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

            function stateLabel(state) {
                for (var i = 0; i < ctrl.statusOptions.length; i++) {
                    if (ctrl.statusOptions[i].value === state) {
                        return ctrl.statusOptions[i].label;
                    }
                }
                return display(state);
            }

            function modeLabel(mode) {
                return modeLabels[String(hasValue(mode) ? mode : '')] || display(mode);
            }

            function phaseLabel(phase) {
                var key = hasValue(phase) ? String(phase) : '';
                return hasValue(phaseLabels[key]) ? phaseLabels[key] : display(phase);
            }

            function kindLabel(kind) {
                return kindLabels[String(hasValue(kind) ? kind : '')] || display(kind);
            }

            function restoreLabel(behavior) {
                return restoreLabels[String(hasValue(behavior) ? behavior : '')] || display(behavior);
            }

            function endpointText(endpoint) {
                if (!endpoint) {
                    return '—';
                }
                var label = kindLabel(endpoint.kind);
                if (hasText(endpoint.address)) {
                    return label + ' · ' + String(endpoint.address);
                }
                var shards = angular.isArray(endpoint.shards) ? endpoint.shards : [];
                if (shards.length) {
                    return label + ' · ' + shards.length + ' 个分片';
                }
                return label;
            }

            function endpointSearchText(endpoint) {
                if (!endpoint) {
                    return '';
                }
                var parts = [endpoint.kind, endpoint.address, endpoint.username];
                (angular.isArray(endpoint.shards) ? endpoint.shards : []).forEach(function (shard) {
                    parts.push(shard && shard.address);
                    (shard && angular.isArray(shard.slots) ? shard.slots : []).forEach(function (range) {
                        parts.push(display(range.start) + '-' + display(range.end));
                    });
                });
                return parts.join(' ');
            }

            function shardText(endpoint) {
                var shards = endpoint && angular.isArray(endpoint.shards) ? endpoint.shards : [];
                if (!shards.length) {
                    return '—';
                }
                return shards.map(function (shard) {
                    var ranges = (angular.isArray(shard.slots) ? shard.slots : []).map(function (range) {
                        return display(range.start) + '-' + display(range.end);
                    }).join(',');
                    return display(shard.address) + (ranges ? ' [' + ranges + ']' : '');
                }).join('；');
            }

            function normalizeJob(raw) {
                raw = raw || {};
                var job = angular.extend({}, raw);
                job.id = hasValue(raw.id) ? String(raw.id) : '';
                job.idDisplay = display(job.id);
                job.name = hasValue(raw.name) ? String(raw.name) : '';
                job.nameDisplay = display(job.name);
                job.mode = hasValue(raw.mode) ? String(raw.mode) : '';
                job.modeDisplay = job.mode ? modeLabel(job.mode) : '—';
                job.state = hasValue(raw.state) ? String(raw.state) : '';
                job.stateDisplay = display(job.state);
                job.phase = hasValue(raw.phase) ? String(raw.phase) : '';
                job.phaseDisplay = phaseLabel(job.phase);
                job.stats = angular.isObject(raw.stats) ? angular.extend({}, raw.stats) : {};
                job.source = raw.source || null;
                job.target = raw.target || null;
                job.sourceDisplay = endpointText(raw.source);
                job.targetDisplay = endpointText(raw.target);
                job.writeCount = hasValue(job.stats.write_count) ? Number(job.stats.write_count) : null;
                job.writeOpsDisplay = hasValue(job.stats.write_ops) ? Format.number(job.stats.write_ops, 1) : '—';
                job.lagDisplay = job.mode === 'sync' && hasValue(job.stats.aof_lag_bytes) ?
                    Format.bytes(job.stats.aof_lag_bytes) : '—';
                job.consoleTail = hasText(raw.console_tail) ? String(raw.console_tail) : '';
                job.searchText = [
                    job.id, job.name, job.state, stateLabel(job.state), job.mode, modeLabel(job.mode),
                    job.phase, phaseLabel(job.phase), endpointSearchText(raw.source), endpointSearchText(raw.target)
                ].join(' ').toLowerCase();
                return job;
            }

            function rowActions(row) {
                var busyNow = ctrl.submitting || ctrl.actionSubmitting || ctrl.confirming;
                var id = row && row.id;
                return [
                    {name: 'detail', label: '详情', icon: 'eye', title: '查看任务详情'},
                    {name: 'stop', label: '停止', icon: 'stop', title: isActive(row && row.state) ? '停止非终态任务' : '仅非终态任务可停止', danger: true,
                        disabled: function () { return busyNow || !id || !isActive(row.state); }},
                    {name: 'remove', label: '删除', icon: 'trash', title: isTerminal(row && row.state) ? '删除终态任务记录' : '仅终态任务可删除', danger: true,
                        disabled: function () { return busyNow || !id || !isTerminal(row.state); }}
                ];
            }

            function buildColumns() {
                return [
                    {key: 'nameDisplay', label: '名称', type: 'link', sortable: true, width: 240},
                    {key: 'sourceDisplay', label: '源端', sortable: false, width: 200},
                    {key: 'targetDisplay', label: '目标端', sortable: false, width: 200},
                    {key: 'modeDisplay', label: '模式', sortable: true, width: 120},
                    {key: 'state', label: '状态', type: 'status', sortable: true, width: 120},
                    {key: 'phaseDisplay', label: '阶段', sortable: false, width: 130},
                    {key: 'writeCount', label: '写入条目', type: 'number', sortable: true, width: 130},
                    {key: 'writeOpsDisplay', label: '写入 QPS', numeric: true, sortable: false, width: 110},
                    {key: 'lagDisplay', label: '延迟', numeric: true, sortable: false, width: 110},
                    {key: 'created_at', label: '创建时间', type: 'date', sortable: true, width: 155},
                    {key: 'actions', label: '操作', type: 'actions', width: 200, actions: rowActions}
                ];
            }

            function updateMetrics() {
                var running = 0;
                var completed = 0;
                var failed = 0;
                ctrl.jobs.forEach(function (job) {
                    if (job.state === 'pending' || job.state === 'starting' || job.state === 'running') {
                        running++;
                    } else if (job.state === 'succeeded' || job.state === 'stopped') {
                        completed++;
                    } else if (job.state === 'failed') {
                        failed++;
                    }
                });
                ctrl.metrics = [
                    {key: 'total', title: '任务总数', value: ctrl.jobs.length, caption: '当前集群的全部数据迁移任务', icon: 'link'},
                    {key: 'running', title: '运行中', value: running, caption: 'pending + starting + running', icon: 'activity', theme: running ? 'primary' : 'neutral'},
                    {key: 'completed', title: '已完成', value: completed, caption: 'succeeded + stopped', icon: 'check'},
                    {key: 'failed', title: '失败', value: failed, caption: 'state = failed', icon: 'alert', status: failed ? 'WARNING' : ''}
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
                preferenceKey = ctrl.product ? 'migration.' + ctrl.product : '';
                var saved = Preferences.get(preferenceKey || 'migration.default', {});
                saved = angular.isObject(saved) && !angular.isArray(saved) ? saved : {};
                var filters = angular.isObject(saved.filters) && !angular.isArray(saved.filters) ? saved.filters : {};
                var status = hasValue(filters.status) ? String(filters.status) : '';
                ctrl.filters = {
                    search: hasValue(filters.search) ? String(filters.search) : '',
                    status: validSavedStatus(status) ? status : ''
                };
                var page = Number(saved.page);
                ctrl.page = isFinite(page) && page > 0 ? Math.floor(page) : 1;
                var allowedSortKeys = ['nameDisplay', 'modeDisplay', 'state', 'writeCount', 'created_at'];
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
                return {
                    name: '',
                    mode: 'sync',
                    source: {
                        kind: 'standalone', address: '', username: '', password: '',
                        tls: false, preferReplica: false, codisScope: 'current', shardsText: ''
                    },
                    target: {
                        kind: 'codis', address: '', username: '', password: '',
                        tls: false, preferReplica: false, codisScope: 'current', shardsText: ''
                    },
                    filter: {
                        allowKeyPrefix: '',
                        blockKeyPrefix: '',
                        allowDb: '',
                        blockDb: '',
                        allowCommand: '',
                        blockCommand: 'FLUSHALL,FLUSHDB'
                    },
                    restoreBehavior: 'rewrite',
                    targetRedisMaxQps: 300000,
                    pipelineCountLimit: 1024,
                    emptyDbBeforeSync: false
                };
            }

            ctrl.modeHint = function () {
                var mode = ctrl.editor && ctrl.editor.mode;
                if (mode === 'scan') {
                    return '通过 SCAN 命令逻辑扫描源端 key 并逐条写入目标端，适合无法 BGSAVE 或希望按前缀过滤的场景。';
                }
                if (mode === 'rdb') {
                    return '只导入 RDB 快照，不追 AOF 增量；适合离线一次性迁移。';
                }
                return '全量 RDB + 持续 AOF 增量同步，直到用户停止；停止/重启后不能续传，需新建任务（会重新全量同步）；可用 consistent / aof_lag_bytes 判断是否追平。';
            };

            ctrl.modeChanged = function () {
                ctrl.clearEditorError('mode');
                if (ctrl.editor.mode !== 'sync') {
                    ctrl.editor.source.preferReplica = false;
                }
            };

            function stopPolling() {
                if (pollTimer !== null) {
                    $timeout.cancel(pollTimer);
                    pollTimer = null;
                }
            }

            function stopLogPolling() {
                if (logPollTimer !== null) {
                    $timeout.cancel(logPollTimer);
                    logPollTimer = null;
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
                if (destroyed || !ctrl.loaded || ctrl.requestInFlight || busy() || ctrl.forbidden ||
                    ctrl.featureDisabled || !hasActiveJobs()) {
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

            function isDisabledResponse(response) {
                return !!response && response.status === 800 && /disabled/i.test(Api.errorMessage(response));
            }

            function findJob(id) {
                var wanted = String(id);
                for (var i = 0; i < ctrl.jobs.length; i++) {
                    if (String(ctrl.jobs[i].id) === wanted) {
                        return ctrl.jobs[i];
                    }
                }
                return null;
            }

            function updateOpenDetailFromList(version) {
                if (!ctrl.detailOpen || !ctrl.activeDetailId || version <= detailVersion) {
                    return;
                }
                var found = findJob(ctrl.activeDetailId);
                if (!found) {
                    return;
                }
                var previousTail = ctrl.detailJob && ctrl.detailJob.console_tail;
                ctrl.detailJob = normalizeJob(found);
                if (!hasText(ctrl.detailJob.console_tail) && hasText(previousTail)) {
                    ctrl.detailJob.console_tail = previousTail;
                }
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

                return Api.request('GET', product, '/api/topom/data-migration/jobs/{xauth}').then(function (data) {
                    if (!currentLoad(product, sequence)) {
                        return;
                    }
                    ctrl.jobs = (angular.isArray(data) ? data : []).map(normalizeJob);
                    ctrl.loaded = true;
                    ctrl.lastUpdated = new Date();
                    ctrl.loadError = '';
                    ctrl.refreshError = '';
                    ctrl.forbidden = false;
                    ctrl.featureDisabled = false;
                    ctrl.disabledMessage = '';
                    ctrl.refreshFeedback = refreshDoneText(options);
                    updateStatusOptions();
                    updateMetrics();
                    applyFilters(false);
                    if (pendingSelectId) {
                        var created = findJob(pendingSelectId);
                        pendingSelectId = '';
                        if (created) {
                            // 新任务可能被当前筛选条件隐藏：清空筛选并回到第一页，再打开详情。
                            ctrl.filters = {search: '', status: ''};
                            applyFilters(true);
                            savePreferences();
                            ctrl.openDetail(created);
                        }
                    }
                    updateOpenDetailFromList(version);
                    if (options.manual) {
                        Toast.success('数据迁移任务列表已刷新');
                    }
                }).catch(function (response) {
                    if (!currentLoad(product, sequence)) {
                        return;
                    }
                    var message = Api.errorMessage(response);
                    ctrl.forbidden = Api.isForbidden(response);
                    if (isDisabledResponse(response)) {
                        ctrl.featureDisabled = true;
                        ctrl.disabledMessage = message;
                    }
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
                return Api.request('GET', product, '/api/topom/data-migration/get/{xauth}/' + Api.segment(id)).then(function (data) {
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

            function rdbProgressText(stats) {
                var text = Format.bytes(stats.rdb_received_bytes) + ' / ' + Format.bytes(stats.rdb_total_bytes);
                var received = Number(stats.rdb_received_bytes);
                var total = Number(stats.rdb_total_bytes);
                if (isFinite(total) && total > 0 && isFinite(received)) {
                    text += ' · ' + Format.percent(Math.min(100, received * 100 / total));
                }
                return text;
            }

            function buildDetailFields() {
                var job = ctrl.detailJob || {};
                var stats = job.stats || {};
                var source = job.source || {};
                var target = job.target || {};
                ctrl.detailFields = [
                    {key: 'id', label: '任务 ID · id', value: display(job.id)},
                    {key: 'name', label: '名称 · name', value: display(job.name)},
                    {key: 'mode', label: '模式 · mode', value: job.mode ? modeLabel(job.mode) + ' · ' + job.mode : '—'},
                    {key: 'source-kind', label: '源端类型 · source.kind', value: job.source ? kindLabel(source.kind) : '—'},
                    {key: 'source-address', label: '源端地址 · source.address', value: display(source.address)},
                    {key: 'source-username', label: '源端用户名 · source.username', value: display(source.username)},
                    {key: 'source-password', label: '源端密码 · source.has_password',
                        value: source.has_password ? '已配置（has_password=true）' : '未配置'},
                    {key: 'source-tls', label: '源端 TLS · source.tls', value: source.tls ? 'true' : 'false'},
                    {key: 'source-shards', label: '源端分片 · source.shards', value: shardText(source)},
                    {key: 'target-kind', label: '目标端类型 · target.kind', value: job.target ? kindLabel(target.kind) : '—'},
                    {key: 'target-address', label: '目标端地址 · target.address', value: display(target.address)},
                    {key: 'target-username', label: '目标端用户名 · target.username', value: display(target.username)},
                    {key: 'target-password', label: '目标端密码 · target.has_password',
                        value: target.has_password ? '已配置（has_password=true）' : '未配置'},
                    {key: 'target-tls', label: '目标端 TLS · target.tls', value: target.tls ? 'true' : 'false'},
                    {key: 'target-shards', label: '目标端分片 · target.shards', value: shardText(target)},
                    {key: 'restore', label: '恢复行为 · restore_behavior', value: restoreLabel(job.restore_behavior)},
                    {key: 'empty', label: '同步前清空 · empty_db_before_sync', value: job.empty_db_before_sync ? 'true' : 'false'},
                    {key: 'qps', label: '目标端 QPS 上限 · target_redis_max_qps', value: Format.number(job.target_redis_max_qps)},
                    {key: 'pipeline', label: 'Pipeline 深度 · pipeline_count_limit', value: Format.number(job.pipeline_count_limit)},
                    {key: 'pid', label: 'PID · pid', value: display(job.pid)},
                    {key: 'state', label: '状态 · state', value: display(job.state)},
                    {key: 'phase', label: '阶段 · phase', value: phaseLabel(job.phase)},
                    {key: 'created', label: '创建时间 · created_at', value: dateText(job.created_at, true)},
                    {key: 'started', label: '开始时间 · started_at', value: dateText(job.started_at, true)},
                    {key: 'finished', label: '结束时间 · finished_at', value: dateText(job.finished_at, true)},
                    {key: 'exit', label: '退出码 · exit_code', value: display(job.exit_code)},
                    {key: 'dir', label: '工作目录 · dir', value: display(job.dir)},
                    {key: 'log', label: '日志文件 · log_file', value: display(job.log_file)},
                    {key: 'console', label: '控制台输出 · console_file', value: display(job.console_file)},
                    {key: 'error', label: '错误 · error', value: display(job.error)}
                ];
                ctrl.progressFields = [
                    {key: 'read-count', label: '读取条目 · read_count', value: Format.number(stats.read_count)},
                    {key: 'write-count', label: '写入条目 · write_count', value: Format.number(stats.write_count)},
                    {key: 'read-ops', label: '读取 QPS · read_ops', value: Format.number(stats.read_ops, 1)},
                    {key: 'write-ops', label: '写入 QPS · write_ops', value: Format.number(stats.write_ops, 1)},
                    {key: 'rdb', label: 'RDB 进度 · rdb_received/rdb_total', value: rdbProgressText(stats)},
                    {key: 'aof-lag', label: 'AOF 延迟 · aof_lag_bytes', value: Format.bytes(stats.aof_lag_bytes)},
                    {key: 'shards', label: '已追平分片 · consistent_shards/shards',
                        value: Format.number(stats.consistent_shards) + ' / ' + Format.number(stats.shards)}
                ];
            }

            ctrl.prettyJson = function (value) {
                if (!hasValue(value)) {
                    return '—';
                }
                var json = angular.toJson(value, 2);
                return hasText(json) ? json : '—';
            };

            ctrl.hasConsoleTail = function () {
                return !!(ctrl.detailJob && hasText(ctrl.detailJob.console_tail));
            };

            ctrl.consoleTail = function () {
                return ctrl.detailJob && hasText(ctrl.detailJob.console_tail) ? ctrl.detailJob.console_tail : '';
            };

            ctrl.detailSubtitle = function () {
                if (!ctrl.detailJob) {
                    return '';
                }
                return display(ctrl.detailJob.name) + ' · ' + display(ctrl.detailJob.id);
            };

            function currentLog(product, sequence, id) {
                return !destroyed && product === ctrl.product && sequence === logSequence &&
                    ctrl.detailOpen && ctrl.activeDetailId === id;
            }

            function resetLog() {
                logSequence++;
                stopLogPolling();
                logNextOffset = 0;
                ctrl.logText = '';
                ctrl.logFile = '';
                ctrl.logError = '';
                ctrl.logLoading = false;
                ctrl.logTrimmed = false;
                ctrl.logUpdatedAt = null;
            }

            function scheduleNextLogPoll() {
                stopLogPolling();
                if (destroyed || !ctrl.detailOpen || !ctrl.activeDetailId) {
                    return;
                }
                logPollTimer = $timeout(function () {
                    logPollTimer = null;
                    if (!destroyed && ctrl.detailOpen && ctrl.activeDetailId && !ctrl.logLoading) {
                        loadLog({poll: true});
                    }
                }, 2000);
            }

            function loadLog(options) {
                options = options || {};
                var product = ctrl.product;
                var id = ctrl.activeDetailId;
                if (!product || !id || destroyed || !ctrl.detailOpen || ctrl.logLoading) {
                    return;
                }
                if (options.reset) {
                    ctrl.logText = '';
                    ctrl.logFile = '';
                    ctrl.logTrimmed = false;
                    logNextOffset = 0;
                }
                var sequence = ++logSequence;
                var fileSwitched = false;
                ctrl.logLoading = true;
                ctrl.logError = '';
                stopLogPolling();
                return Api.request('GET', product, '/api/topom/data-migration/log/{xauth}/' + Api.segment(id) +
                    '?offset=' + logNextOffset + '&max_bytes=65536').then(function (data) {
                    if (!currentLog(product, sequence, id)) {
                        return;
                    }
                    data = data || {};
                    var file = hasText(data.file) ? String(data.file) : '';
                    if (file && ctrl.logFile && file !== ctrl.logFile) {
                        // 服务端优先读 shake.log，尚未创建时回退 console.log。文件切换后上一页的
                        // next_offset 属于另一个文件，这一页必须丢弃：清空已显示文本，offset
                        // 归零后立即从新文件头部重读（清除而不是保留，与 offset=0 整页覆盖一致）。
                        fileSwitched = true;
                        ctrl.logText = '';
                        ctrl.logTrimmed = false;
                        ctrl.logFile = file;
                        logNextOffset = 0;
                        return;
                    }
                    if (file) {
                        ctrl.logFile = file;
                    }
                    var text = hasText(data.text) ? String(data.text) : '';
                    var offset = Number(data.offset);
                    // 服务端从头重放（offset=0 或与预期不一致）时覆盖，否则按 offset 增量追加。
                    if (!text) {
                        // 没有新增内容。
                    } else if (!ctrl.logText || !isFinite(offset) || offset === 0) {
                        ctrl.logText = text;
                    } else if (offset === logNextOffset) {
                        ctrl.logText += text;
                    } else {
                        ctrl.logText = text;
                    }
                    if (ctrl.logText.length > maxLogBytes) {
                        ctrl.logText = ctrl.logText.slice(ctrl.logText.length - maxLogBytes);
                        ctrl.logTrimmed = true;
                    }
                    var nextOffset = Number(data.next_offset);
                    if (isFinite(nextOffset) && nextOffset >= 0) {
                        logNextOffset = nextOffset;
                    } else if (isFinite(offset) && offset >= 0) {
                        logNextOffset = offset + text.length;
                    } else {
                        logNextOffset = ctrl.logText.length;
                    }
                    ctrl.logUpdatedAt = new Date();
                    if (options.manual) {
                        Toast.success('运行日志已刷新');
                    }
                }).catch(function (response) {
                    if (!currentLog(product, sequence, id)) {
                        return;
                    }
                    ctrl.logError = Api.errorMessage(response);
                    if (options.manual) {
                        Toast.error(ctrl.logError);
                    }
                }).finally(function () {
                    if (!currentLog(product, sequence, id)) {
                        return;
                    }
                    ctrl.logLoading = false;
                    if (fileSwitched) {
                        // 丢弃跨文件的那一页后立刻从新文件头部重读，而不是等下一轮 2 秒轮询。
                        loadLog();
                        return;
                    }
                    scheduleNextLogPoll();
                });
            }

            ctrl.refreshLog = function () {
                return loadLog({manual: true, reset: true});
            };

            ctrl.openDetail = function (row) {
                if (!row) {
                    return;
                }
                detailSequence++;
                resetLog();
                ctrl.activeDetailId = row.id || '';
                detailVersion = ++requestVersion;
                ctrl.detailJob = normalizeJob(row);
                ctrl.detailError = '';
                ctrl.detailRefreshing = false;
                ctrl.detailOpen = true;
                buildDetailFields();
                if (ctrl.activeDetailId) {
                    loadDetail(ctrl.activeDetailId);
                    loadLog({});
                }
            };

            ctrl.closeDetail = function () {
                detailSequence++;
                detailVersion = ++requestVersion;
                resetLog();
                ctrl.detailOpen = false;
                ctrl.activeDetailId = '';
                ctrl.detailJob = null;
                ctrl.detailFields = [];
                ctrl.progressFields = [];
                ctrl.detailError = '';
                ctrl.detailRefreshing = false;
            };

            function isCodisCurrent(side) {
                return !!side && side.kind === 'codis' && side.codisScope === 'current';
            }

            function needsTopology() {
                return !!ctrl.editor && (isCodisCurrent(ctrl.editor.source) || isCodisCurrent(ctrl.editor.target));
            }

            function targetTopologyBlocked() {
                return !!ctrl.editor && isCodisCurrent(ctrl.editor.target) &&
                    !!ctrl.topology && ctrl.topology.complete === false;
            }

            function sameEndpointAddress() {
                var editor = ctrl.editor;
                if (!editor || !editor.source || !editor.target) {
                    return false;
                }
                // 后端只比较 address：Codis 端点是 static（分片列表），不参与比较。
                if (editor.source.kind === 'codis' || editor.target.kind === 'codis') {
                    return false;
                }
                var source = String(editor.source.address || '').trim().toLowerCase();
                var target = String(editor.target.address || '').trim().toLowerCase();
                return !!source && source === target;
            }

            ctrl.sameEndpointAddress = sameEndpointAddress;

            ctrl.submitBlocked = function () {
                return targetTopologyBlocked() || sameEndpointAddress();
            };

            ctrl.topologyVisible = function () {
                return ctrl.editorOpen && needsTopology();
            };

            ctrl.topologyProblem = function () {
                return !!ctrl.topologyError || (!!ctrl.topology && ctrl.topology.complete === false);
            };

            ctrl.topologyTitle = function () {
                if (ctrl.topologyLoading) {
                    return '正在读取当前集群拓扑';
                }
                if (ctrl.topologyError) {
                    return '无法读取当前集群拓扑';
                }
                if (ctrl.topology && ctrl.topology.complete === false) {
                    return '当前集群拓扑未就绪';
                }
                return '当前集群 Codis 拓扑';
            };

            ctrl.topologySummary = function () {
                if (ctrl.topologyLoading) {
                    return '正在调用 topology/codis 接口读取 Group 与 slot 分配情况…';
                }
                if (ctrl.topologyError) {
                    return ctrl.topologyError + '；Codis 端点可能仍可用，但需要人工确认拓扑。';
                }
                if (!ctrl.topology) {
                    return '尚未读取当前集群拓扑。';
                }
                var groups = angular.isArray(ctrl.topology.groups) ? ctrl.topology.groups : [];
                var unassigned = Number(ctrl.topology.unassigned_slots) || 0;
                var actions = Number(ctrl.topology.slot_actions) || 0;
                var counted = false;
                var assigned = 0;
                groups.forEach(function (group) {
                    var count = Number(group && group.slot_count);
                    if (isFinite(count)) {
                        assigned += count;
                        counted = true;
                    }
                });
                if (!counted) {
                    assigned = Math.max(0, 1024 - unassigned);
                }
                var text = '当前集群：' + groups.length + ' 个分片 · ' + assigned + ' slots 已分配';
                if (ctrl.topology.online === false) {
                    text += '；Dashboard 报告集群离线';
                }
                if (unassigned || actions) {
                    text += '；未分配 ' + Format.number(unassigned) + ' 个 slots，' + Format.number(actions) + ' 个 slot 正在迁移';
                }
                if (targetTopologyBlocked()) {
                    text += '。目标端为 Codis 当前集群时要求拓扑完整，暂时无法提交。';
                }
                return text;
            };

            function loadTopology() {
                var product = ctrl.product;
                if (!product || destroyed) {
                    return;
                }
                var sequence = ++topologySequence;
                ctrl.topologyLoading = true;
                ctrl.topologyError = '';
                return Api.request('GET', product, '/api/topom/data-migration/topology/codis/{xauth}').then(function (data) {
                    if (destroyed || sequence !== topologySequence || product !== ctrl.product) {
                        return;
                    }
                    ctrl.topology = data || null;
                }).catch(function (response) {
                    if (destroyed || sequence !== topologySequence || product !== ctrl.product) {
                        return;
                    }
                    ctrl.topology = null;
                    ctrl.topologyError = Api.errorMessage(response);
                }).finally(function () {
                    if (destroyed || sequence !== topologySequence || product !== ctrl.product) {
                        return;
                    }
                    ctrl.topologyLoading = false;
                });
            }

            function ensureTopology() {
                if (!needsTopology() || ctrl.topologyLoading) {
                    return;
                }
                if (ctrl.topology && !ctrl.topologyError) {
                    return;
                }
                loadTopology();
            }

            ctrl.sideChanged = function (side) {
                ctrl.clearEditorError(side === 'source' ? 'sourceAddress' : 'targetAddress');
                ctrl.clearEditorError(side === 'source' ? 'sourceShards' : 'targetShards');
                ctrl.clearEditorError('sameAddress');
                ensureTopology();
            };

            ctrl.newTask = function () {
                if (busy() || destroyed) {
                    return;
                }
                ctrl.editor = newEditor();
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                ctrl.topology = null;
                ctrl.topologyError = '';
                ctrl.topologyLoading = false;
                ctrl.editorOpen = true;
                ensureTopology();
            };

            ctrl.closeEditor = function () {
                if (ctrl.submitting) {
                    return;
                }
                ctrl.editorOpen = false;
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                ctrl.topologyError = '';
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
                        scheduleNextLogPoll();
                    }
                });
            }

            function currentMutation(product, sequence, jobId) {
                return !destroyed && product === ctrl.product && sequence === mutationSequence &&
                    mutationJobId === jobId;
            }

            function splitList(value) {
                return String(value || '').split(',').map(function (item) {
                    return item.trim();
                }).filter(function (item) {
                    return item !== '';
                });
            }

            function splitNumberList(value) {
                var parts = splitList(value);
                var values = [];
                for (var i = 0; i < parts.length; i++) {
                    if (!/^\d+$/.test(parts[i])) {
                        return {error: '请使用逗号分隔的非负整数，例如 0,1'};
                    }
                    values.push(Number(parts[i]));
                }
                return {values: values};
            }

            function parseLimit(value) {
                var text = String(value === undefined || value === null ? '' : value).trim();
                if (text === '') {
                    // 后端用 0 表示“不覆盖 RedisShake 默认值”。
                    return 0;
                }
                if (!/^\d+$/.test(text)) {
                    return null;
                }
                var parsed = Number(text);
                return isFinite(parsed) && parsed >= 0 ? parsed : null;
            }

            function isHostPort(value) {
                var text = String(value === undefined || value === null ? '' : value).trim();
                return /^(\[[^\]]+\]|[^:\s]+):\d+$/.test(text);
            }

            // 与后端 migration.ValidateSlotCoverage 一致：范围合法、不重叠、完整覆盖 0-1023。
            function slotCoverageError(shards) {
                var owner = [];
                for (var sid = 0; sid < 1024; sid++) {
                    owner.push(-1);
                }
                for (var i = 0; i < shards.length; i++) {
                    for (var slot = shards[i].slot_start; slot <= shards[i].slot_end; slot++) {
                        if (owner[slot] !== -1) {
                            return 'slot ' + slot + ' 同时属于 ' + shards[owner[slot]].address +
                                ' 与 ' + shards[i].address + '，slot 范围不能重叠';
                        }
                        owner[slot] = i;
                    }
                }
                for (var missing = 0; missing < owner.length; missing++) {
                    if (owner[missing] === -1) {
                        return 'slot ' + missing + ' 没有被任何目标端分片覆盖；目标端 slot 范围必须完整覆盖 0-1023';
                    }
                }
                return '';
            }

            function parseShardLines(text, isTarget) {
                var lines = String(text || '').split(/\r?\n/).map(function (line) {
                    return line.trim();
                }).filter(function (line) {
                    return line !== '';
                });
                if (!lines.length) {
                    return {error: isTarget ?
                        '请至少填写一个 Codis 分片（host:port slot_start-slot_end）' :
                        '请至少填写一个 Codis 分片（host:port）'};
                }
                var shards = [];
                var seen = {};
                for (var i = 0; i < lines.length; i++) {
                    var line = lines[i];
                    var address = '';
                    if (isTarget) {
                        var matched = line.match(/^(\S+)\s+(\d+)\s*-\s*(\d+)$/);
                        if (!matched) {
                            return {error: '第 ' + (i + 1) + ' 行格式错误，应为 host:port slot_start-slot_end'};
                        }
                        address = matched[1];
                        var start = Number(matched[2]);
                        var end = Number(matched[3]);
                        if (start > end || end > 1023) {
                            return {error: '第 ' + (i + 1) + ' 行 slot 范围必须在 0-1023 且起始不大于结束'};
                        }
                        if (!isHostPort(address)) {
                            return {error: '第 ' + (i + 1) + ' 行地址必须是 host:port'};
                        }
                        if (seen[address]) {
                            return {error: '分片地址重复：' + address};
                        }
                        seen[address] = true;
                        shards.push({address: address, slot_start: start, slot_end: end});
                    } else {
                        if (/\s/.test(line)) {
                            return {error: '第 ' + (i + 1) + ' 行格式错误，应为 host:port'};
                        }
                        address = line;
                        if (!isHostPort(address)) {
                            return {error: '第 ' + (i + 1) + ' 行地址必须是 host:port'};
                        }
                        if (seen[address]) {
                            return {error: '分片地址重复：' + address};
                        }
                        seen[address] = true;
                        shards.push({address: address});
                    }
                }
                if (isTarget) {
                    var coverage = slotCoverageError(shards);
                    if (coverage) {
                        return {error: coverage};
                    }
                }
                return {shards: shards};
            }

            function collectSide(sideKey, isTarget) {
                var side = ctrl.editor[sideKey];
                var field = sideKey;
                var result = {
                    kind: side.kind,
                    address: '',
                    username: String(side.username || '').trim(),
                    password: side.password || '',
                    tls: !!side.tls
                };
                if (side.kind === 'codis') {
                    result.codis_scope = side.codisScope === 'manual' ? 'manual' : 'current';
                    if (result.codis_scope === 'manual') {
                        var parsed = parseShardLines(side.shardsText, isTarget);
                        if (parsed.error) {
                            ctrl.fieldErrors[field + 'Shards'] = parsed.error;
                            return null;
                        }
                        result.shards = parsed.shards;
                    }
                    return result;
                }
                var address = String(side.address || '').trim();
                if (!address) {
                    ctrl.fieldErrors[field + 'Address'] = isTarget ?
                        '请输入目标端地址（host:port）' : '请输入源端地址（host:port）';
                    return null;
                }
                if (!isHostPort(address)) {
                    ctrl.fieldErrors[field + 'Address'] = '地址格式必须是 host:port，例如 10.0.20.1:6379';
                    return null;
                }
                result.address = address;
                if (!isTarget) {
                    result.prefer_replica = !!side.preferReplica && ctrl.editor.mode === 'sync';
                }
                return result;
            }

            function collectFilter() {
                var filter = {
                    allow_key_prefix: splitList(ctrl.editor.filter.allowKeyPrefix),
                    block_key_prefix: splitList(ctrl.editor.filter.blockKeyPrefix),
                    allow_db: [],
                    block_db: [],
                    allow_command: splitList(ctrl.editor.filter.allowCommand),
                    block_command: splitList(ctrl.editor.filter.blockCommand)
                };
                var allowDb = splitNumberList(ctrl.editor.filter.allowDb);
                if (allowDb.error) {
                    ctrl.fieldErrors.allowDb = allowDb.error;
                } else {
                    filter.allow_db = allowDb.values;
                }
                var blockDb = splitNumberList(ctrl.editor.filter.blockDb);
                if (blockDb.error) {
                    ctrl.fieldErrors.blockDb = blockDb.error;
                } else {
                    filter.block_db = blockDb.values;
                }
                return filter;
            }

            function endpointBody(endpoint) {
                if (endpoint && endpoint.kind === 'codis' && endpoint.codis_scope === 'current') {
                    return {kind: 'codis', codis_scope: 'current'};
                }
                return endpoint;
            }

            function requestEndpointText(endpoint) {
                if (!endpoint) {
                    return '—';
                }
                if (endpoint.kind === 'codis') {
                    if (endpoint.codis_scope === 'manual') {
                        return 'Codis 集群（手动分片） · ' + (endpoint.shards || []).length + ' 个分片';
                    }
                    return 'Codis 集群（当前集群拓扑）';
                }
                return kindLabel(endpoint.kind) + ' · ' + display(endpoint.address);
            }

            function validateEditor() {
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                var name = String(ctrl.editor.name || '').trim();
                if (!name) {
                    ctrl.fieldErrors.name = '请输入任务名称';
                } else if (name.length > 64) {
                    ctrl.fieldErrors.name = '任务名称最多 64 个字符';
                }
                if (['sync', 'scan', 'rdb'].indexOf(ctrl.editor.mode) < 0) {
                    ctrl.fieldErrors.mode = '请选择迁移模式';
                }
                var source = collectSide('source', false);
                var target = collectSide('target', true);
                var filter = collectFilter();
                var qps = parseLimit(ctrl.editor.targetRedisMaxQps);
                if (qps === null) {
                    ctrl.fieldErrors.qps = '请输入不小于 0 的整数；留空表示使用默认值 300000';
                }
                var pipeline = parseLimit(ctrl.editor.pipelineCountLimit);
                if (pipeline === null) {
                    ctrl.fieldErrors.pipeline = '请输入不小于 0 的整数；留空表示使用默认值 1024';
                }
                if (sameEndpointAddress()) {
                    ctrl.fieldErrors.sameAddress = '源端与目标端地址相同，请修改后再提交';
                }
                var keys = Object.keys(ctrl.fieldErrors).filter(function (key) {
                    return hasText(ctrl.fieldErrors[key]);
                });
                if (keys.length) {
                    ctrl.drawerError = '请修正表单中的错误后重试';
                    return null;
                }
                return {
                    name: name,
                    mode: ctrl.editor.mode,
                    source: endpointBody(source),
                    target: endpointBody(target),
                    filter: filter,
                    restoreBehavior: ctrl.editor.restoreBehavior,
                    qps: qps,
                    pipeline: pipeline,
                    emptyDbBeforeSync: !!ctrl.editor.emptyDbBeforeSync
                };
            }

            ctrl.submitCreate = function () {
                if (busy() || destroyed || !ctrl.editor) {
                    return;
                }
                var value = validateEditor();
                if (!value) {
                    return;
                }
                var sourceText = requestEndpointText(value.source);
                var targetText = requestEndpointText(value.target);
                var details = [
                    '任务名称: ' + value.name,
                    '迁移模式: ' + modeLabel(value.mode),
                    '源端: ' + sourceText,
                    '目标端: ' + targetText,
                    'restore_behavior: ' + value.restoreBehavior,
                    'empty_db_before_sync: ' + (value.emptyDbBeforeSync ? 'true' : 'false')
                ];
                if (value.emptyDbBeforeSync) {
                    details.push('警告：创建后会立即对目标端执行 FLUSHALL，清空目标端全部数据。');
                }
                return confirmOperation({
                    title: '确认创建数据迁移任务？',
                    message: '任务创建后会立即启动 RedisShake Worker 并开始向目标端写入数据，请确认源端与目标端无误。',
                    details: details,
                    confirmText: '确认创建并开始迁移',
                    danger: true
                }, function (product) {
                    var sequence = ++mutationSequence;
                    mutationJobId = '';
                    ctrl.submitting = true;
                    ctrl.activeMutation = 'create';
                    ctrl.drawerError = '';
                    stopPolling();
                    return Api.request('PUT', product, '/api/topom/data-migration/create/{xauth}', {
                        name: value.name,
                        mode: value.mode,
                        source: value.source,
                        target: value.target,
                        filter: value.filter,
                        restore_behavior: value.restoreBehavior,
                        target_redis_max_qps: value.qps,
                        pipeline_count_limit: value.pipeline,
                        empty_db_before_sync: value.emptyDbBeforeSync
                    }).then(function (data) {
                        if (!currentMutation(product, sequence, '')) {
                            return;
                        }
                        ctrl.editorOpen = false;
                        ctrl.editor = newEditor();
                        ctrl.fieldErrors = {};
                        ctrl.drawerError = '';
                        ctrl.topology = null;
                        ctrl.topologyError = '';
                        var id = data && data.id;
                        pendingSelectId = id ? String(id) : '';
                        Activity.add(product, '创建数据迁移任务', sourceText + ' → ' + targetText, 'success');
                        Toast.success(id ? '迁移任务 ' + id + ' 已创建，正在刷新' : '数据迁移任务已创建，正在刷新');
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
                        scheduleNextLogPoll();
                    });
                });
            };

            ctrl.rowAction = function (row, action) {
                var name = angular.isString(action) ? action : action && (action.name || action.action || action.id);
                if (name === 'detail') {
                    ctrl.openDetail(row);
                } else if (name === 'stop') {
                    ctrl.stopJob(row);
                } else if (name === 'remove') {
                    ctrl.removeJob(row);
                }
            };

            function mutateJob(kind, row) {
                if (busy() || destroyed || !row || !row.id) {
                    return;
                }
                var id = String(row.id);
                var stopping = kind === 'stop';
                if (stopping && !isActive(row.state)) {
                    Toast.info('仅非终态数据迁移任务可以停止');
                    return;
                }
                if (!stopping && !isTerminal(row.state)) {
                    Toast.info('仅 succeeded、failed 或 stopped 任务可以删除');
                    return;
                }
                return confirmOperation({
                    title: stopping ? '确认停止数据迁移任务？' : '确认删除数据迁移任务？',
                    message: stopping ?
                        '停止会终止 RedisShake Worker，目标端已写入的数据不会回滚；任务将进入 stopped 终态。' :
                        '删除会移除该终态任务记录及其工作目录中的日志文件，此操作不可撤销。',
                    details: [
                        '任务 ID: ' + id,
                        '名称: ' + display(row.name),
                        '源端: ' + display(row.sourceDisplay),
                        '目标端: ' + display(row.targetDisplay),
                        'state: ' + display(row.state)
                    ],
                    confirmText: stopping ? '确认停止任务' : '确认删除任务',
                    danger: true
                }, function (product) {
                    var latest = findJob(id);
                    if (!latest) {
                        Toast.info('该任务已不在当前列表中，正在刷新');
                        loadJobs({background: true});
                        return;
                    }
                    if (stopping && !isActive(latest.state)) {
                        Toast.info('任务已进入终态，无需停止');
                        loadJobs({background: true});
                        return;
                    }
                    if (!stopping && !isTerminal(latest.state)) {
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
                    return Api.request('PUT', product, '/api/topom/data-migration/' + kind + '/{xauth}/' +
                        Api.segment(id)).then(function () {
                        if (!currentMutation(product, sequence, id)) {
                            return;
                        }
                        Activity.add(product, stopping ? '停止数据迁移任务' : '删除数据迁移任务',
                            id + ' · ' + display(latest.name), 'success');
                        Toast.success(stopping ? '数据迁移任务已停止，正在刷新' : '数据迁移任务已删除，正在刷新');
                        if (!stopping && ctrl.detailOpen && ctrl.activeDetailId === id) {
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
                        scheduleNextLogPoll();
                    });
                });
            }

            ctrl.stopJob = function (row) {
                return mutateJob('stop', row);
            };

            ctrl.removeJob = function (row) {
                return mutateJob('remove', row);
            };

            function resetForProduct() {
                loadSequence++;
                detailSequence++;
                mutationSequence++;
                topologySequence++;
                logSequence++;
                requestVersion++;
                detailVersion = requestVersion;
                mutationJobId = null;
                pendingSelectId = '';
                stopPolling();
                stopLogPolling();
                cancelActiveConfirmation();
                logNextOffset = 0;
                ctrl.jobs = [];
                ctrl.filteredRows = [];
                ctrl.metrics = [];
                ctrl.selected = [];
                ctrl.loaded = false;
                ctrl.initialLoading = true;
                ctrl.refreshing = false;
                ctrl.requestInFlight = false;
                ctrl.loadError = '';
                ctrl.refreshError = '';
                ctrl.operationError = '';
                ctrl.refreshFeedback = '';
                ctrl.forbidden = false;
                ctrl.featureDisabled = false;
                ctrl.disabledMessage = '';
                ctrl.lastUpdated = null;
                ctrl.editor = newEditor();
                ctrl.editorOpen = false;
                ctrl.fieldErrors = {};
                ctrl.drawerError = '';
                ctrl.submitting = false;
                ctrl.actionSubmitting = false;
                ctrl.confirming = false;
                ctrl.activeMutation = '';
                ctrl.topology = null;
                ctrl.topologyError = '';
                ctrl.topologyLoading = false;
                ctrl.detailOpen = false;
                ctrl.detailJob = null;
                ctrl.detailFields = [];
                ctrl.progressFields = [];
                ctrl.detailError = '';
                ctrl.detailRefreshing = false;
                ctrl.activeDetailId = '';
                ctrl.logText = '';
                ctrl.logFile = '';
                ctrl.logError = '';
                ctrl.logLoading = false;
                ctrl.logTrimmed = false;
                ctrl.logUpdatedAt = null;
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
                if (ctrl.product) {
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
                topologySequence++;
                logSequence++;
                requestVersion++;
                mutationJobId = null;
                stopPolling();
                stopLogPolling();
                cancelActiveConfirmation();
                listeners.forEach(function (unsubscribe) { unsubscribe(); });
                listeners = [];
            };

            listeners.push($scope.$on('cluster:refreshRequested', function (event, payload) {
                if (!destroyed && payload && payload.product === ctrl.product && !busy()) {
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
