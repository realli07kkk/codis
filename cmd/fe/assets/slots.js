'use strict';

dashboard.component('slotsPage', {
    bindings: {
        product: '<'
    },
    template: `
        <div class="slots-page">
            <style>
                .slots-page .slots-metrics {
                    grid-template-columns: repeat(3, minmax(0, 1fr));
                    gap: 12px;
                }
                .slots-page .slots-panel {
                    margin-bottom: 18px;
                }
                .slots-page .slots-runtime {
                    display: flex;
                    align-items: center;
                    justify-content: space-between;
                    gap: 18px;
                }
                .slots-page .slots-runtime__status {
                    display: flex;
                    min-width: 0;
                    align-items: center;
                    flex-wrap: wrap;
                    gap: 9px 14px;
                }
                .slots-page .slots-runtime__progress {
                    color: var(--text-secondary);
                    font-size: 12px;
                    overflow-wrap: anywhere;
                }
                .slots-page .slots-filter-heading {
                    display: flex;
                    align-items: baseline;
                    justify-content: space-between;
                    gap: 12px;
                    margin-bottom: 14px;
                }
                .slots-page .slots-filter-heading h2 {
                    margin: 0;
                }
                .slots-page .slots-result-count {
                    color: var(--text-tertiary);
                    font-size: 11px;
                    white-space: nowrap;
                }
                .slots-page .slots-result-count strong {
                    color: var(--primary);
                    font-size: 14px;
                    font-variant-numeric: tabular-nums;
                }
                .slots-page .slots-filter-grid {
                    display: grid;
                    grid-template-columns: minmax(220px, 1.6fr) repeat(3, minmax(140px, 1fr)) auto;
                    gap: 12px;
                    align-items: end;
                }
                .slots-page .slots-filter-grid .form-label {
                    margin-bottom: 5px;
                }
                .slots-page .slots-filter-actions {
                    display: flex;
                    min-height: 36px;
                    align-items: center;
                }
                .slots-page .slots-legend {
                    display: flex;
                    flex-wrap: wrap;
                    gap: 8px 16px;
                    margin: 0 0 16px;
                    padding: 0;
                    list-style: none;
                }
                .slots-page .slots-legend li {
                    display: inline-flex;
                    align-items: center;
                    gap: 6px;
                    color: var(--text-secondary);
                    font-size: 10px;
                    line-height: 18px;
                }
                .slots-page .slots-legend__swatch {
                    width: 14px;
                    min-width: 14px;
                    height: 14px;
                    padding: 0;
                    border-radius: 3px;
                    pointer-events: none;
                }
                .slots-page .slot-grid {
                    grid-template-columns: repeat(auto-fill, minmax(30px, 1fr));
                    gap: 4px;
                    contain: layout style;
                }
                .slots-page button.slot-cell {
                    height: 28px;
                    padding: 0 2px;
                    cursor: pointer;
                    font-family: inherit;
                    font-size: 9px;
                    line-height: 1;
                    transition: opacity 120ms ease, border-color 120ms ease, transform 120ms ease;
                }
                .slots-page button.slot-cell:hover,
                .slots-page button.slot-cell:focus-visible {
                    z-index: 1;
                    border-color: var(--primary);
                    outline: 2px solid rgba(86, 100, 255, 0.18);
                    outline-offset: 1px;
                    transform: translateY(-1px);
                }
                .slots-page .slot-cell.is-pending {
                    border-color: #E9D89E;
                    background: #FFF7DB;
                    color: #8A6412;
                }
                .slots-page .slot-cell.is-migrating {
                    border-color: #C9C0F3;
                    background: #F0EBFF;
                    color: #6650B4;
                }
                .slots-page .slot-cell.is-finished {
                    border-color: #CFEFE8;
                    background: var(--success-soft);
                    color: #007F6C;
                }
                .slots-page .slot-cell.is-unknown {
                    border-style: dashed;
                    background: #FAFAFC;
                    color: var(--text-tertiary);
                }
                .slots-page .slot-cell.is-dimmed {
                    opacity: 0.16;
                }
                .slots-page .slot-cell.is-dimmed:hover,
                .slots-page .slot-cell.is-dimmed:focus-visible {
                    opacity: 0.72;
                }
                .slots-page .slots-distribution-footer {
                    display: flex;
                    align-items: center;
                    justify-content: space-between;
                    gap: 12px;
                    margin-top: 14px;
                    color: var(--text-tertiary);
                    font-size: 10px;
                    line-height: 17px;
                }
                .slots-page .slots-detail-section {
                    margin-top: 20px;
                }
                .slots-page .slots-detail-section h3 {
                    margin: 0 0 10px;
                    color: var(--text-primary);
                    font-size: 13px;
                }
                .slots-page .slots-preview-summary {
                    display: flex;
                    align-items: center;
                    justify-content: space-between;
                    gap: 12px;
                    margin-bottom: 14px;
                    color: var(--text-secondary);
                    font-size: 12px;
                }
                .slots-page .slots-preview-group {
                    margin-bottom: 12px;
                    padding: 14px;
                    border: 1px solid var(--border);
                    border-radius: 8px;
                    background: #FAFAFC;
                }
                .slots-page .slots-preview-group__header {
                    display: flex;
                    align-items: center;
                    justify-content: space-between;
                    gap: 10px;
                    margin-bottom: 10px;
                    color: var(--text-primary);
                    font-size: 12px;
                }
                .slots-page .slots-preview-ranges {
                    display: flex;
                    flex-wrap: wrap;
                    gap: 6px;
                }
                .slots-page .slots-preview-ranges code {
                    padding: 3px 7px;
                    border: 1px solid #DEDFFC;
                    border-radius: 5px;
                    background: #FFFFFF;
                    color: #515BA7;
                    font-family: "SFMono-Regular", Consolas, monospace;
                    font-size: 10px;
                    font-variant-numeric: tabular-nums;
                }
                @media (max-width: 1100px) {
                    .slots-page .slots-filter-grid {
                        grid-template-columns: repeat(2, minmax(0, 1fr));
                    }
                    .slots-page .slots-filter-grid > :first-child {
                        grid-column: 1 / -1;
                    }
                }
                @media (max-width: 720px) {
                    .slots-page .slots-metrics {
                        grid-template-columns: repeat(2, minmax(0, 1fr));
                    }
                    .slots-page .slots-runtime,
                    .slots-page .slots-distribution-footer,
                    .slots-page .slots-preview-summary {
                        align-items: flex-start;
                        flex-direction: column;
                    }
                    .slots-page .slots-filter-grid {
                        grid-template-columns: 1fr;
                    }
                    .slots-page .slots-filter-grid > :first-child {
                        grid-column: auto;
                    }
                }
            </style>

            <page-header title="Slots 分片"
                description="查看 1024 个 Slot 的 Group 分配与迁移进度；按数量或范围创建迁移，并在执行自动 Rebalance 前核对完整预览。">
                <button type="button" class="button button-secondary" ng-click="$ctrl.refresh()"
                    ng-disabled="$ctrl.initialLoading || $ctrl.refreshing || $ctrl.submitting || !$ctrl.product">
                    <ui-icon name="refresh" size="16" ng-class="{'is-spinning': $ctrl.refreshing}"></ui-icon>
                    {{$ctrl.refreshing ? '刷新中' : '刷新'}}
                </button>
                <button type="button" class="button button-primary" ng-click="$ctrl.previewRebalance()"
                    ng-disabled="!$ctrl.loaded || $ctrl.refreshing || $ctrl.submitting || $ctrl.confirming || !$ctrl.groups.length">
                    <ui-icon name="slots" size="16"></ui-icon>
                    {{$ctrl.activeMutation === 'rebalance-preview' ? '生成预览中…' : '自动 Rebalance'}}
                </button>
            </page-header>

            <loading-skeleton ng-if="$ctrl.initialLoading" variant="page"></loading-skeleton>

            <empty-state ng-if="!$ctrl.initialLoading && !$ctrl.loaded && $ctrl.loadError"
                icon="alert" title="无法获取 Slots 数据" description="{{$ctrl.loadError}}"
                action-label="重新加载" on-action="$ctrl.retry()"></empty-state>

            <div ng-if="$ctrl.loaded">
                <div class="inline-alert inline-alert--error" ng-if="$ctrl.refreshError" role="alert">
                    <ui-icon name="alert" size="18"></ui-icon>
                    <div>
                        <strong>刷新失败，当前展示上次成功加载的数据</strong>
                        <p>{{$ctrl.refreshError}}</p>
                        <button type="button" class="button button-secondary" ng-click="$ctrl.retry()">重试</button>
                    </div>
                </div>
                <div class="inline-alert inline-alert--error" ng-if="$ctrl.operationError" role="alert">
                    <ui-icon name="alert" size="18"></ui-icon>
                    <div><strong>操作未完成</strong><p>{{$ctrl.operationError}}</p></div>
                </div>

                <div class="metrics-grid slots-metrics" aria-label="Slots 指标摘要">
                    <metric-card ng-repeat="metric in $ctrl.metrics track by metric.key" metric="metric"></metric-card>
                </div>

                <section class="panel slots-panel slots-runtime" aria-label="迁移执行控制">
                    <div class="slots-runtime__status">
                        <status-tag status="$ctrl.actionStateTone()" label="{{$ctrl.actionStateLabel() || '—'}}"></status-tag>
                        <span class="slots-runtime__progress">执行状态：{{$ctrl.actionProgressLabel() || '—'}}</span>
                        <span class="slots-runtime__progress">Action 记录：{{$ctrl.actionRows.length}} 项</span>
                    </div>
                    <div class="card-actions">
                        <button type="button" class="button button-secondary" ng-click="$ctrl.openInterval()"
                            ng-disabled="$ctrl.submitting || $ctrl.confirming || $ctrl.refreshing">
                            <ui-icon name="clock" size="15"></ui-icon>编辑执行间隔
                        </button>
                        <button type="button" class="button" ng-class="$ctrl.action.disabled ? 'button-primary' : 'button-danger'"
                            ng-click="$ctrl.toggleAction()" ng-disabled="$ctrl.action.disabled === null || $ctrl.submitting || $ctrl.confirming || $ctrl.refreshing">
                            <ui-icon name="{{$ctrl.action.disabled ? 'play' : 'pause'}}" size="15"></ui-icon>
                            {{$ctrl.action.disabled ? '启用 Action' : '停用 Action'}}
                        </button>
                    </div>
                </section>

                <section class="panel slots-panel" aria-labelledby="slots-filter-title">
                    <div class="slots-filter-heading">
                        <h2 id="slots-filter-title" class="card-title">筛选 Slot</h2>
                        <span class="slots-result-count" aria-live="polite">共 <strong>{{$ctrl.filteredSlots.length}}</strong> / 1024 个匹配结果</span>
                    </div>
                    <div class="slots-filter-grid">
                        <label>
                            <span class="form-label">Slot ID / Group 关键词</span>
                            <input class="form-control" type="search" ng-model="$ctrl.filters.search"
                                ng-model-options="{debounce: 180}" ng-change="$ctrl.filtersChanged()"
                                placeholder="例如：120、Slot-120、Group-2" autocomplete="off">
                        </label>
                        <label>
                            <span class="form-label">状态</span>
                            <select class="form-control" ng-model="$ctrl.filters.status" ng-change="$ctrl.filtersChanged()"
                                ng-options="option.value as option.label for option in $ctrl.statusOptions" aria-label="Slot 状态筛选">
                                <option value="">全部状态</option>
                            </select>
                        </label>
                        <label>
                            <span class="form-label">Group</span>
                            <select class="form-control" ng-model="$ctrl.filters.group" ng-change="$ctrl.filtersChanged()"
                                ng-options="option.value as option.label for option in $ctrl.groupFilterOptions" aria-label="Slot Group 筛选">
                                <option value="">全部 Group</option>
                            </select>
                        </label>
                        <label>
                            <span class="form-label">分布筛选方式</span>
                            <select class="form-control" ng-model="$ctrl.filters.display" ng-change="$ctrl.filtersChanged()" aria-label="分布筛选方式">
                                <option value="highlight">保留全局并高亮匹配</option>
                                <option value="filter">仅显示匹配 Slot</option>
                            </select>
                        </label>
                        <div class="slots-filter-actions">
                            <button type="button" class="button button-secondary" ng-click="$ctrl.clearFilters()"
                                ng-disabled="!$ctrl.hasSlotFilters()">清除筛选</button>
                        </div>
                    </div>
                </section>

                <section class="panel slots-panel" aria-labelledby="slots-distribution-title">
                    <div class="card-header">
                        <div>
                            <h2 id="slots-distribution-title" class="card-title">1024 Slot 分布</h2>
                            <p class="form-hint">每格对应一个 Slot；点击可查看只读详情。高亮模式会淡化不匹配项，过滤模式会移除不匹配项。</p>
                        </div>
                        <span class="form-hint">Slot 0–1023</span>
                    </div>

                    <div class="inline-alert inline-alert--warning" ng-if="!$ctrl.groups.length" role="status">
                        <ui-icon name="alert" size="18"></ui-icon>
                        <div>
                            <strong>当前集群没有可用 Group</strong>
                            <p>分布仍按后端实际返回展示；按数量、按范围迁移及自动 Rebalance 暂不可用，请先创建 Group。</p>
                        </div>
                    </div>

                    <ul class="slots-legend" aria-label="Slot 状态图例">
                        <li ng-repeat="item in $ctrl.legend track by item.key">
                            <span class="slot-cell slots-legend__swatch" ng-class="item.className" aria-hidden="true"></span>
                            <span>{{item.label}}</span>
                        </li>
                    </ul>

                    <empty-state ng-if="$ctrl.hasSlotFilters() && !$ctrl.filteredSlots.length"
                        icon="search" title="没有匹配的 Slot"
                        description="请调整 Slot ID、Group 关键词或状态筛选；也可以清除筛选恢复完整分布。"
                        action-label="清除筛选" on-action="$ctrl.clearFilters()"></empty-state>

                    <div class="slot-grid" ng-if="!$ctrl.hasSlotFilters() || $ctrl.filteredSlots.length"
                        role="group" aria-label="1024 个 Slot，点击格子查看详情">
                        <button type="button" ng-repeat="slot in $ctrl.visibleSlots track by slot.uid"
                            class="slot-cell {{::slot.className}}" data-slot-id="{{::slot.id}}"
                            aria-label="{{::slot.ariaLabel}}" title="{{::slot.title}}">
                            <span aria-hidden="true">{{::slot.text}}</span>
                        </button>
                    </div>

                    <div class="slots-distribution-footer">
                        <span>{{$ctrl.refreshFeedback || '有非终态 Action 时每 3 秒自动刷新；其余时间不轮询。'}}</span>
                        <span>最后更新：{{$ctrl.lastUpdateText()}}</span>
                    </div>
                </section>

                <section class="panel slots-panel" aria-labelledby="slots-actions-title">
                    <div class="card-header">
                        <div>
                            <h2 id="slots-actions-title" class="card-title">迁移 Action <small>{{$ctrl.actionRows.length}} 项</small></h2>
                            <p class="form-hint">只有 pending action 可以删除；preparing、prepared、migrating 等非 pending 状态由后端继续处理。</p>
                        </div>
                        <div class="card-actions">
                            <button type="button" class="button button-secondary" ng-click="$ctrl.openSome()"
                                ng-disabled="$ctrl.groups.length < 2 || $ctrl.submitting || $ctrl.confirming">
                                <ui-icon name="plus" size="15"></ui-icon>按数量迁移
                            </button>
                            <button type="button" class="button button-primary" ng-click="$ctrl.openRange()"
                                ng-disabled="!$ctrl.groups.length || $ctrl.submitting || $ctrl.confirming">
                                <ui-icon name="plus" size="15"></ui-icon>按范围迁移
                            </button>
                        </div>
                    </div>
                    <codis-data-table rows="$ctrl.actionRows" columns="$ctrl.actionColumns"
                        selected="$ctrl.actionSelected" page="$ctrl.actionPage" page-size="12"
                        sort-key="$ctrl.actionSortKey" sort-direction="$ctrl.actionSortDirection"
                        selectable="false" row-key="sid" empty-title="暂无迁移 Action"
                        empty-description="创建迁移或执行 Rebalance 后，等待与执行中的任务会显示在这里。"
                        on-row-action="$ctrl.handleAction(row, action)"></codis-data-table>
                </section>
            </div>

            <resource-drawer open="$ctrl.detailOpen" title="$ctrl.detailTitle"
                subtitle="'Slot 基础信息与迁移状态 · 只读，不提供直接编辑'"
                width="560" show-submit="false" on-close="$ctrl.closeDetail()">
                <div ng-if="$ctrl.selectedSlot">
                    <dl class="key-value-grid">
                        <div class="key-value-item"><dt>Slot ID</dt><dd>{{$ctrl.selectedSlot.id}}</dd></div>
                        <div class="key-value-item"><dt>当前 Group</dt><dd>{{$ctrl.selectedSlot.sourceLabel}}</dd></div>
                        <div class="key-value-item"><dt>分布状态</dt><dd>{{$ctrl.selectedSlot.statusLabel}}</dd></div>
                        <div class="key-value-item"><dt>Target Group</dt><dd>{{$ctrl.selectedSlot.targetLabel}}</dd></div>
                        <div class="key-value-item"><dt>Action State</dt><dd>{{$ctrl.selectedSlot.state || '—'}}</dd></div>
                        <div class="key-value-item"><dt>Action Index</dt><dd>{{$ctrl.selectedSlot.actionIndexLabel}}</dd></div>
                    </dl>
                    <section class="slots-detail-section">
                        <h3>迁移状态</h3>
                        <div class="inline-alert inline-alert--warning" ng-if="$ctrl.selectedSlot.state">
                            <ui-icon name="activity" size="18"></ui-icon>
                            <div>
                                <strong>{{$ctrl.selectedSlot.statusLabel}}</strong>
                                <p>{{$ctrl.selectedSlot.sourceLabel}} → {{$ctrl.selectedSlot.targetLabel}}；状态与进度由 Dashboard 返回。</p>
                            </div>
                        </div>
                        <div class="inline-alert inline-alert--info" ng-if="!$ctrl.selectedSlot.state">
                            <ui-icon name="info" size="18"></ui-icon>
                            <p>当前没有迁移 Action。此抽屉仅用于查看，不会伪造或提交 Slot 编辑。</p>
                        </div>
                    </section>
                </div>
            </resource-drawer>

            <resource-drawer open="$ctrl.someOpen" title="'按数量创建迁移'"
                subtitle="'create-some · 从源 Group 选择指定数量的可迁移 Slot'"
                width="560" submitting="$ctrl.submitting" show-submit="true"
                submit-disabled="$ctrl.submitting || $ctrl.confirming || $ctrl.groups.length < 2"
                submit-label="创建迁移" error="$ctrl.someError"
                on-close="$ctrl.closeSome()" on-submit="$ctrl.submitSome()">
                <div class="form-grid">
                    <div class="form-group" ng-class="{'has-error': $ctrl.someErrors.src_gid}">
                        <label class="form-label" for="slots-some-source">源 Group（src_gid）</label>
                        <select id="slots-some-source" class="form-control" ng-model="$ctrl.someForm.src_gid"
                            ng-options="group.id as group.name for group in $ctrl.groups"
                            ng-disabled="$ctrl.submitting || $ctrl.confirming" ng-change="$ctrl.clearFormError('some', 'src_gid')"></select>
                        <p class="form-hint">整数 Group ID，范围 1–9999。</p>
                        <p class="form-error" ng-if="$ctrl.someErrors.src_gid" role="alert">{{$ctrl.someErrors.src_gid}}</p>
                    </div>
                    <div class="form-group" ng-class="{'has-error': $ctrl.someErrors.dst_gid}">
                        <label class="form-label" for="slots-some-target">目标 Group（dst_gid）</label>
                        <select id="slots-some-target" class="form-control" ng-model="$ctrl.someForm.dst_gid"
                            ng-options="group.id as group.name for group in $ctrl.groups"
                            ng-disabled="$ctrl.submitting || $ctrl.confirming" ng-change="$ctrl.clearFormError('some', 'dst_gid')"></select>
                        <p class="form-hint">必须与源 Group 不同。</p>
                        <p class="form-error" ng-if="$ctrl.someErrors.dst_gid" role="alert">{{$ctrl.someErrors.dst_gid}}</p>
                    </div>
                    <div class="form-group form-group--full" ng-class="{'has-error': $ctrl.someErrors.num}">
                        <label class="form-label" for="slots-some-number">迁移数量（num）</label>
                        <input id="slots-some-number" class="form-control" type="text" inputmode="numeric"
                            ng-model="$ctrl.someForm.num" ng-disabled="$ctrl.submitting || $ctrl.confirming"
                            ng-change="$ctrl.clearFormError('some', 'num')" autocomplete="off" placeholder="1–1024" required>
                        <p class="form-hint">请输入 1–1024 的整数；实际可迁移数量由后端根据源 Group 校验。</p>
                        <p class="form-error" ng-if="$ctrl.someErrors.num" role="alert">{{$ctrl.someErrors.num}}</p>
                    </div>
                </div>
                <div class="inline-alert inline-alert--warning">
                    <ui-icon name="alert" size="18"></ui-icon>
                    <p>确认后会创建 pending 迁移 Action，并由 Dashboard 执行数据迁移。请求失败时保留当前输入。</p>
                </div>
            </resource-drawer>

            <resource-drawer open="$ctrl.rangeOpen" title="'按范围创建迁移'"
                subtitle="'create-range · 将闭区间 [beg, end] 内的 Slot 迁移到目标 Group'"
                width="560" submitting="$ctrl.submitting" show-submit="true"
                submit-disabled="$ctrl.submitting || $ctrl.confirming || !$ctrl.groups.length"
                submit-label="创建迁移" error="$ctrl.rangeError"
                on-close="$ctrl.closeRange()" on-submit="$ctrl.submitRange()">
                <div class="form-grid">
                    <div class="form-group" ng-class="{'has-error': $ctrl.rangeErrors.beg}">
                        <label class="form-label" for="slots-range-beg">起始 Slot（beg）</label>
                        <input id="slots-range-beg" class="form-control" type="text" inputmode="numeric"
                            ng-model="$ctrl.rangeForm.beg" ng-disabled="$ctrl.submitting || $ctrl.confirming"
                            ng-change="$ctrl.clearFormError('range', 'beg')" autocomplete="off" placeholder="0" required>
                        <p class="form-hint">整数，范围 0–1023，包含起始值。</p>
                        <p class="form-error" ng-if="$ctrl.rangeErrors.beg" role="alert">{{$ctrl.rangeErrors.beg}}</p>
                    </div>
                    <div class="form-group" ng-class="{'has-error': $ctrl.rangeErrors.end}">
                        <label class="form-label" for="slots-range-end">结束 Slot（end）</label>
                        <input id="slots-range-end" class="form-control" type="text" inputmode="numeric"
                            ng-model="$ctrl.rangeForm.end" ng-disabled="$ctrl.submitting || $ctrl.confirming"
                            ng-change="$ctrl.clearFormError('range', 'end')" autocomplete="off" placeholder="1023" required>
                        <p class="form-hint">整数，范围 0–1023，且不得小于 beg。</p>
                        <p class="form-error" ng-if="$ctrl.rangeErrors.end" role="alert">{{$ctrl.rangeErrors.end}}</p>
                    </div>
                    <div class="form-group form-group--full" ng-class="{'has-error': $ctrl.rangeErrors.gid}">
                        <label class="form-label" for="slots-range-group">目标 Group（gid）</label>
                        <select id="slots-range-group" class="form-control" ng-model="$ctrl.rangeForm.gid"
                            ng-options="group.id as group.name for group in $ctrl.groups"
                            ng-disabled="$ctrl.submitting || $ctrl.confirming" ng-change="$ctrl.clearFormError('range', 'gid')"></select>
                        <p class="form-hint">整数 Group ID，范围 1–9999；目标必须已存在。</p>
                        <p class="form-error" ng-if="$ctrl.rangeErrors.gid" role="alert">{{$ctrl.rangeErrors.gid}}</p>
                    </div>
                </div>
                <div class="inline-alert inline-alert--warning">
                    <ui-icon name="alert" size="18"></ui-icon>
                    <p>范围为闭区间，0 和 1023 都是有效边界。确认后创建迁移 Action，失败时保留当前输入。</p>
                </div>
            </resource-drawer>

            <resource-drawer open="$ctrl.intervalOpen" title="'编辑 Action 执行间隔'"
                subtitle="'interval · 每次 Slot 数据迁移之间的等待时间，单位微秒'"
                width="520" submitting="$ctrl.submitting" show-submit="true"
                submit-disabled="$ctrl.submitting || $ctrl.confirming" submit-label="保存执行间隔"
                error="$ctrl.intervalError" on-close="$ctrl.closeInterval()" on-submit="$ctrl.submitInterval()">
                <div class="form-group" ng-class="{'has-error': $ctrl.intervalErrors.interval}">
                    <label class="form-label" for="slots-action-interval">执行间隔（微秒）</label>
                    <input id="slots-action-interval" class="form-control" type="text" inputmode="numeric"
                        ng-model="$ctrl.intervalForm.interval" ng-disabled="$ctrl.submitting || $ctrl.confirming"
                        ng-change="$ctrl.clearFormError('interval', 'interval')" autocomplete="off" required>
                    <p class="form-hint">必须是非负整数；0 表示不额外等待。失败时保留输入，不会关闭抽屉。</p>
                    <p class="form-error" ng-if="$ctrl.intervalErrors.interval" role="alert">{{$ctrl.intervalErrors.interval}}</p>
                </div>
            </resource-drawer>

            <resource-drawer open="$ctrl.rebalanceOpen" title="'自动 Rebalance 执行预览'"
                subtitle="'预览由 confirm=0 生成；只有在此抽屉确认后才会调用 confirm=1'"
                width="760" submitting="$ctrl.submitting" show-submit="true"
                submit-disabled="$ctrl.submitting || $ctrl.confirming || !$ctrl.rebalancePreview || !$ctrl.rebalancePreview.total"
                submit-label="确认执行 Rebalance" error="$ctrl.rebalanceError"
                on-close="$ctrl.closeRebalance()" on-submit="$ctrl.executeRebalance()">
                <div ng-if="$ctrl.rebalancePreview">
                    <div class="inline-alert inline-alert--warning">
                        <ui-icon name="alert" size="18"></ui-icon>
                        <div>
                            <strong>这是变更预览，尚未执行 Rebalance</strong>
                            <p>以下按 Group 汇总连续的 Slot 闭区间。执行时 Dashboard 会根据当前拓扑处理；若期间发生变化，请重新生成预览。</p>
                        </div>
                    </div>
                    <div class="slots-preview-summary">
                        <span>预览共涉及 <strong>{{$ctrl.rebalancePreview.totalLabel}}</strong> 个 Slot、{{$ctrl.rebalancePreview.groups.length}} 个目标 Group。</span>
                        <button type="button" class="button button-secondary" ng-click="$ctrl.previewRebalance()"
                            ng-disabled="$ctrl.submitting || $ctrl.confirming || $ctrl.refreshing">重新生成预览</button>
                    </div>
                    <empty-state ng-if="!$ctrl.rebalancePreview.total" icon="check" title="当前无需 Rebalance"
                        description="confirm=0 未返回需要调整的 Slot，因此无需提交执行。"></empty-state>
                    <section class="slots-preview-group" ng-repeat="group in $ctrl.rebalancePreview.groups track by group.gid">
                        <header class="slots-preview-group__header">
                            <strong>Group-{{group.gid}}</strong>
                            <span>{{group.countLabel}} Slots</span>
                        </header>
                        <div class="slots-preview-ranges" aria-label="Group-{{group.gid}} 的连续 Slot 范围">
                            <code ng-repeat="range in group.ranges track by range.key"
                                title="Slot {{range.beg}} 到 {{range.end}}（闭区间）">{{range.text}}</code>
                        </div>
                    </section>
                </div>
            </resource-drawer>
        </div>
    `,
    controller: ['Api', 'Activity', 'ClusterStore', 'Confirm', 'Format', 'Preferences', 'Toast',
        '$element', '$scope', '$timeout',
        function (Api, Activity, ClusterStore, Confirm, Format, Preferences, Toast, $element, $scope, $timeout) {
            var ctrl = this;
            var SLOT_COUNT = 1024;
            var MAX_SLOT_ID = SLOT_COUNT - 1;
            var MAX_GROUP_ID = 9999;
            var MAX_SAFE_INTEGER = Number.MAX_SAFE_INTEGER || 9007199254740991;
            var TERMINAL_STATES = {
                '': true,
                finished: true,
                done: true,
                success: true,
                succeeded: true,
                failed: true,
                error: true,
                canceled: true,
                cancelled: true
            };
            var STATUS_LABELS = {
                assigned: 'Assigned',
                offline: 'Offline',
                unknown: 'Unknown',
                pending: 'Pending',
                preparing: 'Preparing',
                prepared: 'Prepared',
                migrating: 'Migrating',
                finished: 'Finished'
            };
            var STATUS_CLASSES = {
                assigned: 'is-assigned',
                offline: 'is-error',
                unknown: 'is-unknown',
                pending: 'is-pending',
                preparing: 'is-warning',
                prepared: 'is-warning',
                migrating: 'is-migrating',
                finished: 'is-finished'
            };

            var destroyed = false;
            var loadSequence = 0;
            var refreshSequence = 0;
            var mutationSequence = 0;
            var previewSequence = 0;
            var loadPromise = null;
            var refreshPromise = null;
            var pollTimer = null;
            var renderVersion = 0;
            var lastAppliedState = null;
            var lastAppliedAt = null;
            var activeConfirmation = null;
            var listeners = [];

            ctrl.loaded = false;
            ctrl.initialLoading = true;
            ctrl.refreshing = false;
            ctrl.loadError = '';
            ctrl.refreshError = '';
            ctrl.operationError = '';
            ctrl.refreshFeedback = '';
            ctrl.cluster = null;
            ctrl.groups = [];
            ctrl.groupFilterOptions = [];
            ctrl.slots = [];
            ctrl.slotById = Object.create(null);
            ctrl.visibleSlots = [];
            ctrl.filteredSlots = [];
            ctrl.actionRows = [];
            ctrl.actionSelected = [];
            ctrl.actionPage = 1;
            ctrl.actionSortKey = 'sid';
            ctrl.actionSortDirection = 'asc';
            ctrl.metrics = [];
            ctrl.summary = emptySummary();
            ctrl.action = emptyAction();
            ctrl.hasSlotData = false;
            ctrl.updatedAt = null;
            ctrl.submitting = false;
            ctrl.confirming = false;
            ctrl.activeMutation = '';

            ctrl.detailOpen = false;
            ctrl.detailTitle = 'Slot 详情';
            ctrl.selectedSlot = null;
            ctrl.someOpen = false;
            ctrl.rangeOpen = false;
            ctrl.intervalOpen = false;
            ctrl.rebalanceOpen = false;
            ctrl.rebalancePreview = null;
            ctrl.rebalancePreviewProduct = '';
            ctrl.someErrors = {};
            ctrl.rangeErrors = {};
            ctrl.intervalErrors = {};
            ctrl.someError = '';
            ctrl.rangeError = '';
            ctrl.intervalError = '';
            ctrl.rebalanceError = '';
            ctrl.someForm = newSomeForm();
            ctrl.rangeForm = newRangeForm();
            ctrl.intervalForm = {interval: ''};

            ctrl.statusOptions = [
                {value: 'assigned', label: 'Assigned · 已分配'},
                {value: 'offline', label: 'Offline · Group 0'},
                {value: 'pending', label: 'Pending · 等待执行'},
                {value: 'preparing', label: 'Preparing · 准备中'},
                {value: 'prepared', label: 'Prepared · 已准备'},
                {value: 'migrating', label: 'Migrating · 迁移中'},
                {value: 'finished', label: 'Finished · 已完成'},
                {value: 'unknown', label: 'Unknown · 未上报'}
            ];
            ctrl.legend = [
                {key: 'assigned', label: 'Assigned · 已分配 Group', className: 'is-assigned'},
                {key: 'offline', label: 'Offline · Group 0', className: 'is-error'},
                {key: 'pending', label: 'Pending · 等待迁移', className: 'is-pending'},
                {key: 'preparing', label: 'Preparing / Prepared · 准备迁移', className: 'is-warning'},
                {key: 'migrating', label: 'Migrating · 正在迁移', className: 'is-migrating'},
                {key: 'finished', label: 'Finished · 迁移完成', className: 'is-finished'},
                {key: 'unknown', label: 'Unknown · 未上报', className: 'is-unknown'}
            ];
            ctrl.filters = {search: '', status: '', group: '', display: 'highlight'};
            ctrl.actionColumns = [
                {key: 'sid', label: 'Slot ID', sortable: true, type: 'number'},
                {key: 'state', label: 'State', sortable: true, type: 'status'},
                {key: 'source', label: 'Source / Group', sortable: true},
                {key: 'target', label: 'Target / Group', sortable: true},
                {
                    key: 'operations',
                    label: '操作',
                    type: 'actions',
                    actions: [{
                        name: 'remove',
                        label: '删除',
                        icon: 'trash',
                        danger: true,
                        title: '只有 pending action 可删除',
                        disabled: function (row) {
                            return ctrl.submitting || ctrl.confirming || !row || row.state !== 'pending';
                        }
                    }]
                }
            ];

            function emptySummary() {
                return {
                    assigned: null,
                    offline: null,
                    migrating: null,
                    pending: 0,
                    active: 0,
                    reported: 0,
                    unreported: SLOT_COUNT
                };
            }

            function emptyAction() {
                return {
                    disabled: null,
                    interval: null,
                    failed: null,
                    progress: null
                };
            }

            function newSomeForm() {
                return {src_gid: '', dst_gid: '', num: '1'};
            }

            function newRangeForm() {
                return {beg: '0', end: '1023', gid: ''};
            }

            function preferenceKey() {
                return 'slots.' + (ctrl.product || 'default') + '.filters';
            }

            function readPreferences() {
                var saved = Preferences.get(preferenceKey(), {});
                saved = saved && typeof saved === 'object' ? saved : {};
                var validStatuses = {
                    '': true,
                    assigned: true,
                    offline: true,
                    pending: true,
                    preparing: true,
                    prepared: true,
                    migrating: true,
                    finished: true,
                    unknown: true
                };
                var status = validStatuses[saved.status] ? saved.status : '';
                ctrl.actionPage = Number(saved.actionPage) > 0 ? Math.floor(Number(saved.actionPage)) : 1;
                ctrl.actionSortKey = ['sid', 'state', 'source', 'target'].indexOf(saved.actionSortKey) >= 0 ? saved.actionSortKey : 'sid';
                ctrl.actionSortDirection = saved.actionSortDirection === 'desc' ? 'desc' : 'asc';
                return {
                    search: saved.search === undefined || saved.search === null ? '' : String(saved.search),
                    status: status,
                    group: saved.group === undefined || saved.group === null ? '' : String(saved.group),
                    display: saved.display === 'filter' ? 'filter' : 'highlight'
                };
            }

            function persistFilters() {
                Preferences.set(preferenceKey(), {
                    search: ctrl.filters.search,
                    status: ctrl.filters.status,
                    group: ctrl.filters.group,
                    display: ctrl.filters.display,
                    actionPage: ctrl.actionPage,
                    actionSortKey: ctrl.actionSortKey,
                    actionSortDirection: ctrl.actionSortDirection
                });
            }

            function hasValue(value) {
                return value !== undefined && value !== null && value !== '';
            }

            function parseInteger(value, minimum, maximum) {
                if (!hasValue(value)) {
                    return null;
                }
                var text = String(value).trim();
                if (!/^\d+$/.test(text)) {
                    return null;
                }
                var parsed = Number(text);
                if (!isFinite(parsed) || Math.floor(parsed) !== parsed || parsed < minimum || parsed > maximum) {
                    return null;
                }
                return parsed;
            }

            function groupName(id) {
                if (id === null || id === undefined) {
                    return '—';
                }
                return Number(id) === 0 ? 'Offline (Group 0)' : 'Group-' + id;
            }

            function statusLabel(state) {
                return STATUS_LABELS[state] || (state ? String(state).toUpperCase() : 'Unknown');
            }

            function statusClass(state) {
                return STATUS_CLASSES[state] || 'is-warning';
            }

            function isNonTerminal(state) {
                return !!state && !TERMINAL_STATES[String(state).toLowerCase()];
            }

            function hasErrors(errors) {
                return Object.keys(errors).some(function (key) { return !!errors[key]; });
            }

            function groupExists(id) {
                return ctrl.groups.some(function (group) { return group.id === id; });
            }

            function setMutationError(kind, message) {
                if (kind === 'some') {
                    ctrl.someError = message;
                } else if (kind === 'range') {
                    ctrl.rangeError = message;
                } else if (kind === 'interval') {
                    ctrl.intervalError = message;
                } else if (kind === 'rebalance') {
                    ctrl.rebalanceError = message;
                } else {
                    ctrl.operationError = message;
                }
            }

            ctrl.clearFormError = function (kind, field) {
                var errors = kind === 'some' ? ctrl.someErrors : (kind === 'range' ? ctrl.rangeErrors : ctrl.intervalErrors);
                if (errors) {
                    errors[field] = '';
                }
                setMutationError(kind, '');
            };

            ctrl.actionStateLabel = function () {
                if (ctrl.action.disabled === true) { return '已停用'; }
                if (ctrl.action.disabled === false) { return '已启用'; }
                return '';
            };

            ctrl.actionStateTone = function () {
                if (ctrl.action.disabled === true) { return 'WARNING'; }
                if (ctrl.action.disabled === false) { return 'HEALTHY'; }
                return 'UNKNOWN';
            };

            ctrl.actionProgressLabel = function () {
                return ctrl.action.progress || '';
            };

            ctrl.lastUpdateText = function () {
                return ctrl.updatedAt ? Format.date(ctrl.updatedAt, true) : '—';
            };

            function updateMetrics() {
                ctrl.metrics = [
                    {
                        key: 'total',
                        title: 'Slot 总数',
                        value: SLOT_COUNT,
                        caption: ctrl.summary.unreported ? ctrl.summary.unreported + ' 个未上报，以 Unknown 占位' : '固定分片空间 · Slot 0–1023',
                        icon: 'slots',
                        theme: 'neutral'
                    },
                    {
                        key: 'assigned',
                        title: '已分配',
                        value: ctrl.summary.assigned,
                        caption: ctrl.hasSlotData ? '当前 Group ID 大于 0' : '等待 Slot 数据',
                        icon: 'database',
                        theme: 'neutral'
                    },
                    {
                        key: 'offline',
                        title: 'Offline',
                        value: ctrl.summary.offline,
                        caption: ctrl.hasSlotData ? 'Group ID 等于 0' : '等待 Slot 数据',
                        icon: 'alert',
                        theme: 'neutral'
                    },
                    {
                        key: 'migrating',
                        title: 'Migrating',
                        value: ctrl.summary.migrating,
                        caption: ctrl.hasSlotData ? ctrl.summary.pending + ' 个 Pending · ' + ctrl.summary.active + ' 个非终态' : '等待 Slot 数据',
                        icon: 'activity',
                        theme: 'neutral'
                    },
                    {
                        key: 'interval',
                        title: '执行间隔',
                        value: ctrl.action.interval,
                        unit: 'µs',
                        caption: '每次迁移等待的微秒数',
                        icon: 'clock',
                        theme: 'neutral'
                    },
                    {
                        key: 'disabled',
                        title: 'Action 启停',
                        value: ctrl.actionStateLabel() || null,
                        caption: ctrl.action.progress || '执行状态未知',
                        icon: 'refresh',
                        theme: 'neutral'
                    }
                ];
            }

            function normalizeSlot(raw, sid) {
                var present = !!raw;
                raw = raw || {};
                var action = raw.action || {};
                var groupId = parseInteger(raw.group_id, 0, MAX_SAFE_INTEGER);
                var targetId = parseInteger(action.target_id, 0, MAX_SAFE_INTEGER);
                var state = hasValue(action.state) ? String(action.state).trim().toLowerCase() : '';
                var actionIndex = parseInteger(action.index, 0, MAX_SAFE_INTEGER);
                var key = state || (!present || groupId === null ? 'unknown' : (groupId === 0 ? 'offline' : 'assigned'));
                var source = present ? groupName(groupId) : '—';
                var target = groupName(targetId);
                var label = statusLabel(key);
                var aria = 'Slot id: ' + sid + '；Group: ' + source + '；状态: ' + label + '；target: ' + target;
                var title = aria + '；Action index: ' + (actionIndex === null ? '—' : actionIndex);
                if (!present) {
                    title += '；后端未上报此 Slot';
                }
                var searchParts = [
                    sid,
                    'slot ' + sid,
                    'slot-' + sid,
                    'slot:' + sid,
                    source,
                    'group ' + (groupId === null ? '' : groupId),
                    'group-' + (groupId === null ? '' : groupId),
                    'group:' + (groupId === null ? '' : groupId),
                    key,
                    label,
                    target
                ];
                return {
                    id: sid,
                    groupId: groupId,
                    targetId: targetId,
                    state: state,
                    actionIndex: actionIndex,
                    actionIndexLabel: actionIndex === null ? '—' : String(actionIndex),
                    statusKey: key,
                    statusLabel: label,
                    sourceLabel: source,
                    targetLabel: target,
                    className: statusClass(key) + ' slot-state-' + key,
                    ariaLabel: aria,
                    title: title,
                    text: String(sid),
                    searchText: searchParts.join(' ').toLowerCase(),
                    present: present
                };
            }

            function buildGroupFilterOptions(rawSlots) {
                var ids = Object.create(null);
                ctrl.groups.forEach(function (group) { ids[group.id] = true; });
                rawSlots.forEach(function (slot) {
                    var gid = parseInteger(slot.group_id, 0, MAX_SAFE_INTEGER);
                    var target = slot.action ? parseInteger(slot.action.target_id, 0, MAX_SAFE_INTEGER) : null;
                    if (gid !== null) { ids[gid] = true; }
                    if (target !== null) { ids[target] = true; }
                });
                var options = Object.keys(ids).map(function (id) {
                    return Number(id);
                }).sort(function (left, right) {
                    return left - right;
                }).map(function (id) {
                    return {value: String(id), label: groupName(id)};
                });
                options.push({value: 'unknown', label: 'Unknown · 未上报'});
                ctrl.groupFilterOptions = options;
            }

            function matchesFilters(slot) {
                var search = String(ctrl.filters.search || '').trim().toLowerCase();
                if (search && slot.searchText.indexOf(search) < 0) {
                    return false;
                }
                if (ctrl.filters.status && slot.statusKey !== ctrl.filters.status) {
                    return false;
                }
                if (ctrl.filters.group === 'unknown') {
                    return slot.present === false || slot.groupId === null;
                }
                if (ctrl.filters.group !== '') {
                    return slot.groupId !== null && String(slot.groupId) === String(ctrl.filters.group);
                }
                return true;
            }

            ctrl.hasSlotFilters = function () {
                return !!String(ctrl.filters.search || '').trim() || !!ctrl.filters.status || ctrl.filters.group !== '';
            };

            function applyFilters(persist) {
                var active = ctrl.hasSlotFilters();
                var onlyMatches = ctrl.filters.display === 'filter';
                var filtered = [];
                var visible = [];
                var version = ++renderVersion;
                ctrl.slots.forEach(function (slot) {
                    var matches = matchesFilters(slot);
                    if (matches) {
                        filtered.push(slot);
                    }
                    if (onlyMatches && !matches) {
                        return;
                    }
                    visible.push({
                        uid: version + '-' + slot.id,
                        id: slot.id,
                        className: slot.className + (active && !matches ? ' is-dimmed' : ''),
                        ariaLabel: slot.ariaLabel,
                        title: slot.title,
                        text: slot.text
                    });
                });
                ctrl.filteredSlots = filtered;
                ctrl.visibleSlots = visible;
                if (persist) {
                    persistFilters();
                }
            }

            ctrl.filtersChanged = function () {
                applyFilters(true);
            };

            ctrl.clearFilters = function () {
                ctrl.filters.search = '';
                ctrl.filters.status = '';
                ctrl.filters.group = '';
                applyFilters(true);
            };

            function hasNonTerminalActions() {
                return ctrl.actionRows.some(function (row) { return isNonTerminal(row.state); });
            }

            function stopPolling() {
                if (pollTimer) {
                    $timeout.cancel(pollTimer);
                    pollTimer = null;
                }
            }

            function syncPolling() {
                stopPolling();
                if (destroyed || !ctrl.loaded || !hasNonTerminalActions()) {
                    return;
                }
                pollTimer = $timeout(function () {
                    pollTimer = null;
                    if (destroyed) {
                        return;
                    }
                    if (ctrl.submitting || ctrl.confirming || ctrl.refreshing || ctrl.initialLoading) {
                        syncPolling();
                        return;
                    }
                    refreshCluster({background: true, automatic: true});
                }, 3000);
            }

            function applyCluster(state) {
                if (!state || (state === lastAppliedState && state.updatedAt === lastAppliedAt)) {
                    return;
                }
                lastAppliedState = state;
                lastAppliedAt = state.updatedAt;
                ctrl.cluster = state;
                ctrl.loaded = true;
                ctrl.initialLoading = false;
                ctrl.loadError = '';
                ctrl.updatedAt = state.updatedAt || null;
                ctrl.groups = (angular.isArray(state.groups) ? state.groups : []).map(function (group) {
                    return {id: Number(group.id), name: 'Group-' + group.id};
                }).filter(function (group) {
                    return isFinite(group.id) && group.id > 0;
                }).sort(function (left, right) {
                    return left.id - right.id;
                });

                var rawSlots = angular.isArray(state.slots) ? state.slots : [];
                ctrl.hasSlotData = !!(state.raw && angular.isArray(state.raw.slots));
                var byId = Object.create(null);
                var reported = 0;
                rawSlots.forEach(function (slot) {
                    var sid = parseInteger(slot && slot.id, 0, MAX_SLOT_ID);
                    if (sid !== null && !Object.prototype.hasOwnProperty.call(byId, sid)) {
                        byId[sid] = slot;
                        reported++;
                    }
                });

                ctrl.summary = emptySummary();
                ctrl.summary.reported = reported;
                ctrl.summary.unreported = SLOT_COUNT - reported;
                if (ctrl.hasSlotData) {
                    ctrl.summary.assigned = 0;
                    ctrl.summary.offline = 0;
                    ctrl.summary.migrating = 0;
                }
                ctrl.slots = [];
                ctrl.slotById = Object.create(null);
                ctrl.actionRows = [];
                for (var sid = 0; sid < SLOT_COUNT; sid++) {
                    var slot = normalizeSlot(byId[sid], sid);
                    ctrl.slots.push(slot);
                    ctrl.slotById[sid] = slot;
                    if (slot.present && ctrl.hasSlotData) {
                        if (slot.groupId !== null && slot.groupId > 0) {
                            ctrl.summary.assigned++;
                        } else if (slot.groupId === 0) {
                            ctrl.summary.offline++;
                        }
                        if (slot.state === 'migrating') {
                            ctrl.summary.migrating++;
                        }
                        if (slot.state === 'pending') {
                            ctrl.summary.pending++;
                        }
                        if (isNonTerminal(slot.state)) {
                            ctrl.summary.active++;
                        }
                    }
                    if (slot.state) {
                        ctrl.actionRows.push({
                            sid: slot.id,
                            state: slot.state,
                            source: slot.sourceLabel,
                            target: slot.targetLabel,
                            canRemove: slot.state === 'pending'
                        });
                    }
                }
                var rawAction = state.slotAction || {};
                var progress = rawAction.progress || {};
                ctrl.action = {
                    disabled: typeof rawAction.disabled === 'boolean' ? rawAction.disabled : null,
                    interval: parseInteger(rawAction.interval, 0, MAX_SAFE_INTEGER),
                    failed: progress.status === 'failed',
                    progress: hasValue(progress.status) ? String(progress.status) : ''
                };
                if (ctrl.detailOpen && ctrl.selectedSlot) {
                    ctrl.selectedSlot = ctrl.slotById[ctrl.selectedSlot.id] || null;
                }
                buildGroupFilterOptions(rawSlots);
                updateMetrics();
                applyFilters(false);
                syncPolling();
            }

            function currentProduct(product, sequence, type) {
                var currentSequence = type === 'load' ? loadSequence : (type === 'refresh' ? refreshSequence : (type === 'preview' ? previewSequence : mutationSequence));
                return !destroyed && product === ctrl.product && sequence === currentSequence;
            }

            function refreshDoneText(options) {
                var time = Format.time(new Date());
                if (options.afterMutation) { return '变更后刷新完成 · ' + time; }
                if (options.manual) { return '手动刷新完成 · ' + time; }
                if (options.shell) { return 'Shell 刷新完成 · ' + time; }
                if (options.automatic) { return '非终态 Action 自动刷新完成 · ' + time; }
                return '后台刷新完成 · ' + time;
            }

            function loadInitial(options) {
                options = options || {};
                var product = ctrl.product;
                if (!product) {
                    ctrl.initialLoading = false;
                    ctrl.loadError = '请先选择集群';
                    return;
                }
                if (loadPromise) {
                    return loadPromise;
                }
                var sequence = ++loadSequence;
                ctrl.initialLoading = !ctrl.loaded;
                ctrl.loadError = '';
                ctrl.refreshError = '';
                stopPolling();
                loadPromise = ClusterStore.load(product).then(function (state) {
                    if (!currentProduct(product, sequence, 'load')) { return; }
                    applyCluster(state);
                    ctrl.refreshFeedback = 'Slots 数据加载完成 · ' + Format.time(new Date());
                    if (options.manual) { Toast.success('Slots 数据已重新加载'); }
                }).catch(function (response) {
                    if (!currentProduct(product, sequence, 'load')) { return; }
                    var message = Api.errorMessage(response);
                    if (ctrl.loaded) {
                        ctrl.refreshError = message;
                    } else {
                        ctrl.loadError = message;
                    }
                    if (options.manual) { Toast.error(message); }
                }).finally(function () {
                    if (!currentProduct(product, sequence, 'load')) { return; }
                    ctrl.initialLoading = false;
                    loadPromise = null;
                    syncPolling();
                });
                return loadPromise;
            }

            function refreshCluster(options) {
                options = options || {};
                var product = ctrl.product;
                if (!product || destroyed) { return; }
                if (ctrl.initialLoading && !ctrl.loaded) {
                    return loadPromise;
                }
                if (ctrl.refreshing && !options.force) {
                    return refreshPromise;
                }
                var sequence = ++refreshSequence;
                ctrl.refreshing = true;
                ctrl.refreshError = '';
                ctrl.refreshFeedback = options.manual ? '正在手动刷新…' : '正在后台刷新…';
                stopPolling();
                refreshPromise = ClusterStore.refresh(product).then(function (state) {
                    if (!currentProduct(product, sequence, 'refresh')) { return; }
                    applyCluster(state);
                    ctrl.refreshFeedback = refreshDoneText(options);
                    if (options.manual) { Toast.success('Slots 数据已刷新'); }
                }).catch(function (response) {
                    if (!currentProduct(product, sequence, 'refresh')) { return; }
                    var message = Api.errorMessage(response);
                    if (ctrl.loaded) {
                        ctrl.refreshError = message;
                        ctrl.refreshFeedback = '刷新失败，已保留上次成功加载的数据';
                    } else {
                        ctrl.loadError = message;
                    }
                    if (options.manual) { Toast.error(message); }
                }).finally(function () {
                    if (!currentProduct(product, sequence, 'refresh')) { return; }
                    ctrl.refreshing = false;
                    refreshPromise = null;
                    syncPolling();
                });
                return refreshPromise;
            }

            ctrl.refresh = function () {
                if (ctrl.refreshing || ctrl.submitting || ctrl.initialLoading) { return; }
                return refreshCluster({manual: true, background: ctrl.loaded});
            };

            ctrl.retry = function () {
                if (ctrl.loaded) {
                    return refreshCluster({manual: true, background: true, force: true});
                }
                return loadInitial({manual: true});
            };

            ctrl.openSlot = function (sid) {
                var slot = ctrl.slotById[sid];
                if (!slot || ctrl.submitting) { return; }
                ctrl.selectedSlot = slot;
                ctrl.detailTitle = 'Slot ' + slot.id + ' · 只读详情';
                ctrl.detailOpen = true;
            };

            ctrl.closeDetail = function () {
                ctrl.detailOpen = false;
                ctrl.selectedSlot = null;
            };

            ctrl.openSome = function () {
                if (ctrl.submitting || ctrl.confirming) { return; }
                if (ctrl.groups.length < 2) {
                    Toast.info('按数量迁移至少需要两个可用 Group');
                    return;
                }
                ctrl.someForm = {
                    src_gid: ctrl.groups[0].id,
                    dst_gid: ctrl.groups[1].id,
                    num: '1'
                };
                ctrl.someErrors = {};
                ctrl.someError = '';
                ctrl.someOpen = true;
            };

            ctrl.closeSome = function () {
                if (ctrl.submitting) { return; }
                ctrl.someOpen = false;
                ctrl.someErrors = {};
                ctrl.someError = '';
            };

            ctrl.openRange = function () {
                if (ctrl.submitting || ctrl.confirming) { return; }
                if (!ctrl.groups.length) {
                    Toast.info('请先创建可用 Group');
                    return;
                }
                ctrl.rangeForm = {beg: '0', end: '1023', gid: ctrl.groups[0].id};
                ctrl.rangeErrors = {};
                ctrl.rangeError = '';
                ctrl.rangeOpen = true;
            };

            ctrl.closeRange = function () {
                if (ctrl.submitting) { return; }
                ctrl.rangeOpen = false;
                ctrl.rangeErrors = {};
                ctrl.rangeError = '';
            };

            ctrl.openInterval = function () {
                if (ctrl.submitting || ctrl.confirming) { return; }
                ctrl.intervalForm = {
                    interval: ctrl.action.interval === null ? '' : String(ctrl.action.interval)
                };
                ctrl.intervalErrors = {};
                ctrl.intervalError = '';
                ctrl.intervalOpen = true;
            };

            ctrl.closeInterval = function () {
                if (ctrl.submitting) { return; }
                ctrl.intervalOpen = false;
                ctrl.intervalErrors = {};
                ctrl.intervalError = '';
            };

            ctrl.closeRebalance = function () {
                if (ctrl.submitting) { return; }
                ctrl.rebalanceOpen = false;
                ctrl.rebalancePreview = null;
                ctrl.rebalancePreviewProduct = '';
                ctrl.rebalanceError = '';
            };

            function validateSome() {
                var errors = {};
                var src = parseInteger(ctrl.someForm.src_gid, 1, MAX_GROUP_ID);
                var dst = parseInteger(ctrl.someForm.dst_gid, 1, MAX_GROUP_ID);
                var num = parseInteger(ctrl.someForm.num, 1, SLOT_COUNT);
                if (src === null) {
                    errors.src_gid = '源 Group 必须是 1–9999 的整数';
                } else if (!groupExists(src)) {
                    errors.src_gid = '源 Group 不存在，请刷新后重新选择';
                }
                if (dst === null) {
                    errors.dst_gid = '目标 Group 必须是 1–9999 的整数';
                } else if (!groupExists(dst)) {
                    errors.dst_gid = '目标 Group 不存在，请刷新后重新选择';
                }
                if (src !== null && dst !== null && src === dst) {
                    errors.dst_gid = '源 Group 与目标 Group 必须不同';
                }
                if (num === null) {
                    errors.num = '迁移数量必须是 1–1024 的整数';
                }
                ctrl.someErrors = errors;
                ctrl.someError = hasErrors(errors) ? '请修正表单中的错误后重试' : '';
                return hasErrors(errors) ? null : {src: src, dst: dst, num: num};
            }

            function validateRange() {
                var errors = {};
                var beg = parseInteger(ctrl.rangeForm.beg, 0, MAX_SLOT_ID);
                var end = parseInteger(ctrl.rangeForm.end, 0, MAX_SLOT_ID);
                var gid = parseInteger(ctrl.rangeForm.gid, 1, MAX_GROUP_ID);
                if (beg === null) {
                    errors.beg = '起始 Slot 必须是 0–1023 的整数';
                }
                if (end === null) {
                    errors.end = '结束 Slot 必须是 0–1023 的整数';
                } else if (beg !== null && beg > end) {
                    errors.end = '结束 Slot 不得小于起始 Slot';
                }
                if (gid === null) {
                    errors.gid = '目标 Group 必须是 1–9999 的整数';
                } else if (!groupExists(gid)) {
                    errors.gid = '目标 Group 不存在，请刷新后重新选择';
                }
                ctrl.rangeErrors = errors;
                ctrl.rangeError = hasErrors(errors) ? '请修正表单中的错误后重试' : '';
                return hasErrors(errors) ? null : {beg: beg, end: end, gid: gid};
            }

            function cancelActiveConfirmation() {
                if (activeConfirmation && Confirm.state.current === activeConfirmation) {
                    Confirm.cancel('destroyed');
                }
                activeConfirmation = null;
                ctrl.confirming = false;
            }

            function confirmAndMutate(config) {
                if (ctrl.submitting || ctrl.confirming || destroyed) { return; }
                var product = ctrl.product;
                if (!product) { return; }
                ctrl.confirming = true;
                stopPolling();
                var promise = Confirm.open({
                    title: config.title,
                    message: config.message,
                    details: config.details,
                    confirmText: config.confirmText || '确认执行',
                    danger: !!config.danger
                });
                activeConfirmation = Confirm.state.current;
                return promise.then(function () {
                    if (destroyed || product !== ctrl.product) { return; }
                    ctrl.confirming = false;
                    activeConfirmation = null;
                    var sequence = ++mutationSequence;
                    ctrl.submitting = true;
                    ctrl.activeMutation = config.kind;
                    setMutationError(config.kind, '');
                    if (config.kind !== 'rebalance') {
                        ctrl.operationError = '';
                    }
                    return Api.request(config.method || 'PUT', product, config.path).then(function () {
                        if (!currentProduct(product, sequence, 'mutation')) { return; }
                        if (config.onSuccess) {
                            config.onSuccess();
                        }
                        Activity.add(product, config.activity, config.activityTarget || 'Slots', 'success');
                        Toast.success(config.successMessage);
                        refreshCluster({background: true, afterMutation: true, force: true});
                    }, function (response) {
                        if (!currentProduct(product, sequence, 'mutation')) { return; }
                        var message = Api.errorMessage(response);
                        setMutationError(config.kind, message);
                        Toast.error(message);
                    }).finally(function () {
                        if (!currentProduct(product, sequence, 'mutation')) { return; }
                        ctrl.submitting = false;
                        ctrl.activeMutation = '';
                        syncPolling();
                    });
                }, function () {
                    if (!destroyed && product === ctrl.product) {
                        ctrl.confirming = false;
                        activeConfirmation = null;
                        syncPolling();
                    }
                });
            }

            ctrl.submitSome = function () {
                if (ctrl.submitting || ctrl.confirming) { return; }
                var value = validateSome();
                if (!value) { return; }
                confirmAndMutate({
                    kind: 'some',
                    title: '按数量创建 Slot 迁移？',
                    message: '将从 ' + groupName(value.src) + ' 迁移 ' + value.num + ' 个 Slot 到 ' + groupName(value.dst) + '。',
                    details: 'src_gid: ' + value.src + '\ndst_gid: ' + value.dst + '\nnum: ' + value.num,
                    confirmText: '确认创建迁移',
                    danger: true,
                    path: '/api/topom/slots/action/create-some/{xauth}/' + value.src + '/' + value.dst + '/' + value.num,
                    activity: '按数量创建 Slot 迁移',
                    activityTarget: groupName(value.src) + ' → ' + groupName(value.dst) + ' · ' + value.num + ' Slots',
                    successMessage: '已创建 ' + value.num + ' 个 Slot 迁移 Action，正在刷新',
                    onSuccess: function () {
                        ctrl.someOpen = false;
                        ctrl.someErrors = {};
                        ctrl.someError = '';
                    }
                });
            };

            ctrl.submitRange = function () {
                if (ctrl.submitting || ctrl.confirming) { return; }
                var value = validateRange();
                if (!value) { return; }
                confirmAndMutate({
                    kind: 'range',
                    title: '按范围创建 Slot 迁移？',
                    message: '将把闭区间 [' + value.beg + ', ' + value.end + '] 内的 Slot 迁移到 ' + groupName(value.gid) + '。',
                    details: 'beg: ' + value.beg + '\nend: ' + value.end + '\ngid: ' + value.gid,
                    confirmText: '确认创建迁移',
                    danger: true,
                    path: '/api/topom/slots/action/create-range/{xauth}/' + value.beg + '/' + value.end + '/' + value.gid,
                    activity: '按范围创建 Slot 迁移',
                    activityTarget: 'Slots [' + value.beg + ', ' + value.end + '] → ' + groupName(value.gid),
                    successMessage: '范围迁移 Action 已创建，正在刷新',
                    onSuccess: function () {
                        ctrl.rangeOpen = false;
                        ctrl.rangeErrors = {};
                        ctrl.rangeError = '';
                    }
                });
            };

            ctrl.submitInterval = function () {
                if (ctrl.submitting || ctrl.confirming) { return; }
                var interval = parseInteger(ctrl.intervalForm.interval, 0, MAX_SAFE_INTEGER);
                if (interval === null) {
                    ctrl.intervalErrors.interval = '执行间隔必须是非负安全整数（单位：微秒）';
                    ctrl.intervalError = '执行间隔未通过校验，请修改后重试';
                    return;
                }
                ctrl.intervalErrors = {};
                confirmAndMutate({
                    kind: 'interval',
                    title: '修改 Action 执行间隔？',
                    message: '将把 Slot 迁移执行间隔设置为 ' + interval + ' 微秒。',
                    details: 'interval: ' + interval + ' µs',
                    confirmText: '确认保存',
                    danger: false,
                    path: '/api/topom/slots/action/interval/{xauth}/' + interval,
                    activity: '修改 Slot Action 执行间隔',
                    activityTarget: interval + ' µs',
                    successMessage: 'Action 执行间隔已更新，正在刷新',
                    onSuccess: function () {
                        ctrl.intervalOpen = false;
                        ctrl.intervalErrors = {};
                        ctrl.intervalError = '';
                    }
                });
            };

            ctrl.toggleAction = function () {
                if (ctrl.submitting || ctrl.confirming || ctrl.action.disabled === null) { return; }
                var disabled = !ctrl.action.disabled;
                confirmAndMutate({
                    kind: 'toggle',
                    title: disabled ? '停用 Slot Action？' : '启用 Slot Action？',
                    message: disabled ? '停用后，等待中的迁移将暂停推进；已有 Action 不会被删除。' : '启用后，Dashboard 将继续处理等待中的 Slot 迁移 Action。',
                    details: 'disabled: ' + (disabled ? 1 : 0),
                    confirmText: disabled ? '确认停用' : '确认启用',
                    danger: disabled,
                    path: '/api/topom/slots/action/disabled/{xauth}/' + (disabled ? 1 : 0),
                    activity: disabled ? '停用 Slot Action' : '启用 Slot Action',
                    activityTarget: '集群 Slot Action',
                    successMessage: disabled ? 'Slot Action 已停用，正在刷新' : 'Slot Action 已启用，正在刷新'
                });
            };

            ctrl.handleAction = function (row, action) {
                if (action === 'remove') {
                    ctrl.removeSlotAction(row);
                }
            };

            ctrl.removeSlotAction = function (row) {
                if (!row || ctrl.submitting || ctrl.confirming) { return; }
                var current = ctrl.slotById[row.sid];
                if (!current || current.state !== 'pending') {
                    Toast.info('只有仍处于 pending 状态的 Slot Action 可以删除');
                    return;
                }
                confirmAndMutate({
                    kind: 'remove',
                    title: '删除 Pending 迁移 Action？',
                    message: '将删除 Slot ' + current.id + ' 的 pending Action（' + current.sourceLabel + ' → ' + current.targetLabel + '）。',
                    details: 'sid: ' + current.id + '\nstate: pending\n此操作仅移除等待中的任务，不会直接编辑 Slot Group。',
                    confirmText: '确认删除',
                    danger: true,
                    path: '/api/topom/slots/action/remove/{xauth}/' + current.id,
                    activity: '删除 Pending Slot Action',
                    activityTarget: 'Slot-' + current.id,
                    successMessage: 'Slot ' + current.id + ' 的 pending Action 已删除，正在刷新'
                });
            };

            function buildRebalancePreview(data) {
                if (data !== null && data !== undefined && typeof data !== 'object') {
                    return {error: 'Rebalance 预览格式无效，未执行任何变更'};
                }
                data = data || {};
                var groupSlots = Object.create(null);
                for (var sid = 0; sid < SLOT_COUNT; sid++) {
                    var raw = data[sid];
                    if (!hasValue(raw)) {
                        continue;
                    }
                    var gid = parseInteger(raw, 1, MAX_SAFE_INTEGER);
                    if (gid === null) {
                        return {error: 'Rebalance 预览中 Slot ' + sid + ' 的目标 Group 无效，已阻止执行'};
                    }
                    if (!groupSlots[gid]) {
                        groupSlots[gid] = [];
                    }
                    groupSlots[gid].push(sid);
                }
                var total = 0;
                var groups = Object.keys(groupSlots).map(function (key) {
                    return Number(key);
                }).sort(function (left, right) {
                    return left - right;
                }).map(function (gid) {
                    var ids = groupSlots[gid];
                    var ranges = [];
                    var beg = -1;
                    var previous = -1;
                    ids.forEach(function (slotId) {
                        if (beg < 0) {
                            beg = slotId;
                            previous = slotId;
                            return;
                        }
                        if (slotId === previous + 1) {
                            previous = slotId;
                            return;
                        }
                        ranges.push({beg: beg, end: previous});
                        beg = slotId;
                        previous = slotId;
                    });
                    if (beg >= 0) {
                        ranges.push({beg: beg, end: previous});
                    }
                    ranges = ranges.map(function (range) {
                        return {
                            key: gid + '-' + range.beg + '-' + range.end,
                            beg: range.beg,
                            end: range.end,
                            text: '[' + range.beg + ', ' + range.end + ']'
                        };
                    });
                    total += ids.length;
                    return {
                        gid: gid,
                        count: ids.length,
                        countLabel: Format.number(ids.length),
                        ranges: ranges
                    };
                });
                return {
                    groups: groups,
                    total: total,
                    totalLabel: Format.number(total)
                };
            }

            ctrl.previewRebalance = function () {
                if (ctrl.submitting || ctrl.confirming || ctrl.refreshing || destroyed) { return; }
                var product = ctrl.product;
                if (!product || !ctrl.groups.length) {
                    Toast.info('自动 Rebalance 至少需要一个可用 Group');
                    return;
                }
                var sequence = ++previewSequence;
                ctrl.submitting = true;
                ctrl.activeMutation = 'rebalance-preview';
                ctrl.operationError = '';
                ctrl.rebalanceError = '';
                ctrl.rebalanceOpen = false;
                ctrl.rebalancePreview = null;
                ctrl.rebalancePreviewProduct = '';
                stopPolling();
                return Api.request('PUT', product, '/api/topom/slots/rebalance/{xauth}/0').then(function (data) {
                    if (!currentProduct(product, sequence, 'preview')) { return; }
                    var preview = buildRebalancePreview(data);
                    if (preview.error) {
                        ctrl.operationError = preview.error;
                        Toast.error(preview.error);
                        return;
                    }
                    ctrl.rebalancePreview = preview;
                    ctrl.rebalancePreviewProduct = product;
                    ctrl.rebalanceOpen = true;
                    Toast.info('Rebalance 预览已生成，确认后才会执行');
                }, function (response) {
                    if (!currentProduct(product, sequence, 'preview')) { return; }
                    var message = Api.errorMessage(response);
                    ctrl.operationError = message;
                    Toast.error(message);
                }).finally(function () {
                    if (!currentProduct(product, sequence, 'preview')) { return; }
                    ctrl.submitting = false;
                    ctrl.activeMutation = '';
                    syncPolling();
                });
            };

            ctrl.executeRebalance = function () {
                if (ctrl.submitting || ctrl.confirming) { return; }
                if (!ctrl.rebalanceOpen || !ctrl.rebalancePreview || ctrl.rebalancePreviewProduct !== ctrl.product) {
                    Toast.error('必须先获取有效的 Rebalance 预览');
                    return;
                }
                if (!ctrl.rebalancePreview.total) {
                    Toast.info('预览没有需要调整的 Slot，无需执行 Rebalance');
                    return;
                }
                var groupSummary = ctrl.rebalancePreview.groups.map(function (group) {
                    return 'Group-' + group.gid + ': ' + group.count + ' Slots';
                }).join('\n');
                confirmAndMutate({
                    kind: 'rebalance',
                    title: '执行自动 Rebalance？',
                    message: '将根据刚才的预览执行 Rebalance，共涉及 ' + ctrl.rebalancePreview.total + ' 个 Slot。此操作会创建迁移任务。',
                    details: groupSummary + '\n\nconfirm=1 仅在 confirm=0 预览后提交。',
                    confirmText: '确认执行 Rebalance',
                    danger: true,
                    path: '/api/topom/slots/rebalance/{xauth}/1',
                    activity: '执行自动 Slot Rebalance',
                    activityTarget: ctrl.rebalancePreview.total + ' Slots',
                    successMessage: '自动 Rebalance 已提交，正在刷新迁移 Action',
                    onSuccess: function () {
                        ctrl.rebalanceOpen = false;
                        ctrl.rebalancePreview = null;
                        ctrl.rebalancePreviewProduct = '';
                        ctrl.rebalanceError = '';
                    }
                });
            };

            function resetForProduct() {
                loadSequence++;
                refreshSequence++;
                mutationSequence++;
                previewSequence++;
                loadPromise = null;
                refreshPromise = null;
                stopPolling();
                cancelActiveConfirmation();
                lastAppliedState = null;
                lastAppliedAt = null;
                ctrl.loaded = false;
                ctrl.initialLoading = true;
                ctrl.refreshing = false;
                ctrl.loadError = '';
                ctrl.refreshError = '';
                ctrl.operationError = '';
                ctrl.refreshFeedback = '';
                ctrl.cluster = null;
                ctrl.groups = [];
                ctrl.groupFilterOptions = [];
                ctrl.slots = [];
                ctrl.slotById = Object.create(null);
                ctrl.visibleSlots = [];
                ctrl.filteredSlots = [];
                ctrl.actionRows = [];
                ctrl.actionSelected = [];
                ctrl.actionPage = 1;
                ctrl.summary = emptySummary();
                ctrl.action = emptyAction();
                ctrl.hasSlotData = false;
                ctrl.updatedAt = null;
                ctrl.submitting = false;
                ctrl.confirming = false;
                ctrl.activeMutation = '';
                ctrl.detailOpen = false;
                ctrl.selectedSlot = null;
                ctrl.someOpen = false;
                ctrl.rangeOpen = false;
                ctrl.intervalOpen = false;
                ctrl.rebalanceOpen = false;
                ctrl.rebalancePreview = null;
                ctrl.rebalancePreviewProduct = '';
                ctrl.someErrors = {};
                ctrl.rangeErrors = {};
                ctrl.intervalErrors = {};
                ctrl.someError = '';
                ctrl.rangeError = '';
                ctrl.intervalError = '';
                ctrl.rebalanceError = '';
                ctrl.someForm = newSomeForm();
                ctrl.rangeForm = newRangeForm();
                ctrl.intervalForm = {interval: ''};
                updateMetrics();
            }

            function initializeProduct() {
                if (!ctrl.product) {
                    ctrl.initialLoading = false;
                    ctrl.loadError = '请先选择集群';
                    return;
                }
                ctrl.filters = readPreferences();
                var state = ClusterStore.state(ctrl.product);
                if (state.updatedAt) {
                    applyCluster(state);
                    ctrl.initialLoading = false;
                    ctrl.refreshFeedback = '已恢复上次加载的数据，可手动刷新';
                } else {
                    loadInitial();
                }
            }

            ctrl.$onInit = function () {
                initializeProduct();
            };

            ctrl.$onChanges = function (changes) {
                if (changes.product && !changes.product.isFirstChange() &&
                    changes.product.currentValue !== changes.product.previousValue) {
                    resetForProduct();
                    initializeProduct();
                }
            };

            function onGridClick(event) {
                var node = event.target;
                var root = $element[0];
                while (node && node !== root) {
                    if (node.getAttribute && node.hasAttribute('data-slot-id')) {
                        var sid = parseInteger(node.getAttribute('data-slot-id'), 0, MAX_SLOT_ID);
                        if (sid !== null) {
                            $scope.$evalAsync(function () { ctrl.openSlot(sid); });
                        }
                        return;
                    }
                    node = node.parentNode;
                }
            }

            ctrl.$postLink = function () {
                $element.on('click', onGridClick);
            };

            ctrl.$onDestroy = function () {
                destroyed = true;
                loadSequence++;
                refreshSequence++;
                mutationSequence++;
                previewSequence++;
                stopPolling();
                cancelActiveConfirmation();
                listeners.forEach(function (unsubscribe) { unsubscribe(); });
                listeners = [];
                $element.off('click', onGridClick);
            };

            listeners.push($scope.$watchGroup([
                function () { return ctrl.actionPage; },
                function () { return ctrl.actionSortKey; },
                function () { return ctrl.actionSortDirection; }
            ], function () {
                if (!destroyed && ctrl.product) { persistFilters(); }
            }));
            listeners.push($scope.$on('cluster:updated', function (event, payload) {
                if (!destroyed && payload && payload.product === ctrl.product) {
                    applyCluster(payload.state);
                }
            }));
            listeners.push($scope.$on('cluster:refreshRequested', function (event, payload) {
                if (!destroyed && payload && payload.product === ctrl.product && !ctrl.submitting && !ctrl.confirming) {
                    refreshCluster({background: true, shell: true});
                }
            }));
        }
    ]
});
