'use strict';

dashboard.component('appShell', {
    template: `
        <div class="app-frame" ng-class="{'sidebar-is-collapsed': $ctrl.collapsed}" ng-cloak>
            <header class="topbar">
                <button class="brand" type="button" ng-click="$ctrl.goHome()" aria-label="Codis Console 总览">
                    <span class="brand-mark" aria-hidden="true"><span></span><span></span><span></span></span>
                    <span class="brand-copy"><strong>Codis</strong><small>CONSOLE</small></span>
                </button>
                <nav class="top-navigation" aria-label="主要业务模块">
                    <button type="button" ng-repeat="item in $ctrl.topNavigation"
                        ng-class="{active: $ctrl.isTopActive(item)}"
                        ng-click="$ctrl.go(item.page)" ng-disabled="!$ctrl.route.product">
                        <ui-icon name="{{item.icon}}" size="17"></ui-icon>
                        <span>{{item.label}}</span>
                    </button>
                </nav>
                <div class="topbar-actions">
                    <span class="environment-badge" ng-if="$ctrl.mock"><span></span> Mock 环境</span>
                    <button class="icon-button" type="button" ng-click="$ctrl.refresh()" ng-disabled="!$ctrl.route.product"
                        aria-label="刷新当前集群" title="刷新当前集群">
                        <ui-icon name="refresh" size="18" ng-class="{'is-spinning': $ctrl.cluster.refreshing}"></ui-icon>
                    </button>
                    <button class="icon-button" type="button" ng-click="$ctrl.helpOpen = true" aria-label="打开帮助" title="帮助与快捷键">
                        <ui-icon name="help" size="18"></ui-icon>
                    </button>
                    <div class="user-menu-wrap">
                        <button class="user-menu-trigger" type="button" ng-click="$ctrl.userMenuOpen = !$ctrl.userMenuOpen"
                            aria-haspopup="menu" ng-attr-aria-expanded="{{$ctrl.userMenuOpen}}">
                            <span class="avatar" aria-hidden="true">CO</span>
                            <span class="user-name">Codis Operator</span>
                            <ui-icon name="chevron-down" size="14"></ui-icon>
                        </button>
                        <div class="user-popover" ng-if="$ctrl.userMenuOpen" role="menu">
                            <div class="user-popover-heading"><strong>Codis Operator</strong><span>集群运维管理员</span></div>
                            <div class="user-popover-row"><span>运行模式</span><b>{{$ctrl.mock ? '本地 Mock' : 'Dashboard API'}}</b></div>
                            <button type="button" role="menuitem" ng-click="$ctrl.toggleSidebar(); $ctrl.userMenuOpen = false">
                                <ui-icon name="panel" size="16"></ui-icon>{{$ctrl.collapsed ? '展开' : '折叠'}}侧栏
                            </button>
                        </div>
                    </div>
                </div>
            </header>

            <aside class="sidebar" aria-label="集群导航">
                <div class="product-picker" ng-class="{'is-compact': $ctrl.collapsed}">
                    <label for="product-select">当前集群</label>
                    <div class="product-select-wrap">
                        <span class="product-dot" aria-hidden="true"></span>
                        <select id="product-select" ng-model="$ctrl.route.product" ng-change="$ctrl.selectProduct($ctrl.route.product)"
                            ng-options="product as product for product in $ctrl.products" ng-disabled="$ctrl.loadingProducts" title="切换 Codis 集群">
                            <option value="" disabled>选择集群</option>
                        </select>
                        <ui-icon name="chevron-down" size="14"></ui-icon>
                    </div>
                </div>
                <nav class="sidebar-navigation">
                    <section ng-repeat="section in $ctrl.menu" class="menu-section">
                        <h2>{{section.label}}</h2>
                        <button type="button" ng-repeat="item in section.items" ng-click="$ctrl.go(item.page)"
                            ng-class="{active: $ctrl.isActive(item)}" ng-disabled="!$ctrl.route.product"
                            ng-attr-title="{{$ctrl.collapsed ? item.label : ''}}" ng-attr-aria-current="{{$ctrl.isActive(item) ? 'page' : undefined}}">
                            <ui-icon name="{{item.icon}}" size="18"></ui-icon>
                            <span>{{item.label}}</span>
                            <span class="menu-count" ng-if="item.page === 'proxies' && $ctrl.cluster.proxies.length">{{$ctrl.cluster.proxies.length}}</span>
                            <span class="menu-alert" ng-if="item.page === 'overview' && $ctrl.cluster.totals.issues" title="{{$ctrl.cluster.totals.issues}} 项异常">{{$ctrl.cluster.totals.issues}}</span>
                        </button>
                    </section>
                </nav>
                <div class="sidebar-footer">
                    <div class="sidebar-health" ng-if="!$ctrl.collapsed && $ctrl.route.product">
                        <span class="health-pulse" ng-class="{'has-issues': $ctrl.cluster.totals.issues}"></span>
                        <div><strong>{{$ctrl.cluster.totals.issues ? '需要关注' : '运行平稳'}}</strong><small>{{$ctrl.cluster.updatedAt ? ('更新于 ' + ($ctrl.cluster.updatedAt | date:'HH:mm:ss')) : '等待集群数据'}}</small></div>
                    </div>
                    <button class="collapse-button" type="button" ng-click="$ctrl.toggleSidebar()"
                        ng-attr-aria-label="{{$ctrl.collapsed ? '展开侧栏' : '折叠侧栏'}}" title="{{$ctrl.collapsed ? '展开侧栏' : '折叠侧栏'}}">
                        <ui-icon name="{{$ctrl.collapsed ? 'chevrons-right' : 'chevrons-left'}}" size="17"></ui-icon>
                        <span>{{$ctrl.collapsed ? '' : '折叠菜单'}}</span>
                    </button>
                </div>
            </aside>

            <main class="workspace" id="workspace" tabindex="-1">
                <div class="product-loading page-state-wrap" ng-if="$ctrl.loadingProducts">
                    <loading-skeleton variant="page"></loading-skeleton>
                </div>
                <empty-state ng-if="!$ctrl.loadingProducts && $ctrl.productsError" icon="alert" title="无法获取集群列表"
                    description="{{$ctrl.productsError}}" action-label="重新加载" on-action="$ctrl.loadProducts()"></empty-state>
                <empty-state ng-if="!$ctrl.loadingProducts && !$ctrl.productsError && !$ctrl.products.length" icon="database" title="还没有可管理的集群"
                    description="请先在 codis-fe 的 dashboard-list 或 Coordinator 中注册 Dashboard。"></empty-state>
                <empty-state ng-if="!$ctrl.loadingProducts && $ctrl.products.length && $ctrl.invalidProduct" icon="alert" title="集群不存在或已移除"
                    description="当前链接中的集群不在可用列表中，请从左侧重新选择。"></empty-state>

                <div class="page-container" ng-if="$ctrl.route.product && !$ctrl.productsError && !$ctrl.invalidProduct && !$ctrl.loadingProducts" ng-switch="$ctrl.route.page">
                    <overview-page ng-switch-when="overview" product="$ctrl.route.product"></overview-page>
                    <proxies-page ng-switch-when="proxies" product="$ctrl.route.product"></proxies-page>
                    <groups-page ng-switch-when="groups" product="$ctrl.route.product"></groups-page>
                    <slots-page ng-switch-when="slots" product="$ctrl.route.product"></slots-page>
                    <sentinels-page ng-switch-when="sentinels" product="$ctrl.route.product"></sentinels-page>
                    <security-page ng-switch-when="security" product="$ctrl.route.product"></security-page>
                    <analysis-page ng-switch-when="analysis" product="$ctrl.route.product"></analysis-page>
                    <recovery-page ng-switch-when="recovery" product="$ctrl.route.product"></recovery-page>
                    <migration-page ng-switch-when="migration" product="$ctrl.route.product"></migration-page>
                    <proxy-detail-page ng-switch-when="proxy-detail" product="$ctrl.route.product" resource-id="$ctrl.route.id"></proxy-detail-page>
                    <group-detail-page ng-switch-when="group-detail" product="$ctrl.route.product" resource-id="$ctrl.route.id"></group-detail-page>
                    <sentinel-detail-page ng-switch-when="sentinel-detail" product="$ctrl.route.product" resource-id="$ctrl.route.id"></sentinel-detail-page>
                </div>
            </main>

            <resource-drawer open="$ctrl.helpOpen" title="'控制台使用指南'" subtitle="'Codis Console · 运维工作台'"
                show-submit="false" width="640" on-close="$ctrl.helpOpen = false">
                <div class="help-content">
                    <section><span class="help-index">01</span><div><h3>从异常定位到资源详情</h3><p>总览页会汇总 Proxy、Redis Server 与 Sentinel 异常。点击异常项或资源名称即可进入详情，查看趋势、配置和本地操作记录。</p></div></section>
                    <section><span class="help-index">02</span><div><h3>保留你的工作上下文</h3><p>筛选、分页、侧栏状态和监控布局会保存在当前浏览器。抽屉编辑不会触发路由跳转，也不会打断列表滚动位置。</p></div></section>
                    <section><span class="help-index">03</span><div><h3>安全的危险操作</h3><p>删除、Promote、重平衡和 PITR 等操作均需二次确认。Rebalance 始终先生成预览，再由你确认执行。</p></div></section>
                    <section class="shortcut-grid"><h3>键盘操作</h3><p><kbd>Esc</kbd><span>关闭抽屉或对话框</span></p><p><kbd>Tab</kbd><span>在控件之间移动焦点</span></p></section>
                    <div class="mock-tip" ng-if="$ctrl.mock"><ui-icon name="info" size="18"></ui-icon><p>当前使用内置 Mock 数据，所有变更仅保存在页面运行态，不会请求真实 Dashboard。移除地址中的 <code>?mock=1</code> 即可返回真实接口。</p></div>
                </div>
            </resource-drawer>
        </div>
    `,
    controller: ['Api', 'Router', 'ClusterStore', 'Preferences', '$rootScope', '$scope', '$timeout',
        function (Api, Router, ClusterStore, Preferences, $rootScope, $scope, $timeout) {
            var ctrl = this;
            ctrl.mock = Api.isMock();
            ctrl.collapsed = Preferences.get('sidebar.collapsed', false);
            ctrl.products = [];
            ctrl.route = Router.parse();
            ctrl.cluster = ClusterStore.state(ctrl.route.product || 'default');
            ctrl.topNavigation = [
                {label: '集群总览', page: 'overview', icon: 'grid', match: ['overview']},
                {label: '资源管理', page: 'proxies', icon: 'server', match: ['proxies', 'groups', 'slots', 'sentinels', 'proxy-detail', 'group-detail', 'sentinel-detail']},
                {label: '运维中心', page: 'analysis', icon: 'activity', match: ['analysis', 'recovery', 'security']},
                {label: '数据集成', page: 'migration', icon: 'link', match: ['migration']}
            ];
            ctrl.menu = [
                {label: '工作台', items: [{label: '集群总览', page: 'overview', icon: 'grid'}]},
                {label: '资源与拓扑', items: [
                    {label: 'Proxy 实例', page: 'proxies', icon: 'proxy'},
                    {label: 'Group 与 Server', page: 'groups', icon: 'database'},
                    {label: 'Slots 分片', page: 'slots', icon: 'slots'},
                    {label: 'Sentinel 高可用', page: 'sentinels', icon: 'shield'}
                ]},
                {label: '运维工具', items: [
                    {label: '安全与限流', page: 'security', icon: 'lock'},
                    {label: 'RDB 数据分析', page: 'analysis', icon: 'analysis'},
                    {label: 'PITR 数据闪回', page: 'recovery', icon: 'history'}
                ]},
                {label: '数据集成', items: [
                    {label: '数据迁移', page: 'migration', icon: 'link'}
                ]}
            ];

            ctrl.loadProducts = function () {
                ctrl.loadingProducts = true;
                ctrl.productsError = '';
                return Api.list().then(function (products) {
                    ctrl.products = products || [];
                    ctrl.invalidProduct = ctrl.route.product && ctrl.products.indexOf(ctrl.route.product) < 0;
                    if (!ctrl.route.product && ctrl.products.length) {
                        $timeout(function () { Router.go(ctrl.products[0], 'overview'); });
                    }
                }).catch(function (response) {
                    ctrl.productsError = Api.errorMessage(response);
                }).finally(function () {
                    ctrl.loadingProducts = false;
                });
            };

            ctrl.selectProduct = function (product) {
                if (product) {
                    ctrl.invalidProduct = false;
                    Router.go(product, 'overview');
                }
            };
            ctrl.go = function (page) {
                if (ctrl.route.product) { Router.go(ctrl.route.product, page); }
            };
            ctrl.goHome = function () { ctrl.go('overview'); };
            ctrl.isActive = function (item) {
                return Router.basePage(ctrl.route.page) === item.page || ctrl.route.page === item.page;
            };
            ctrl.isTopActive = function (item) { return item.match.indexOf(ctrl.route.page) >= 0; };
            ctrl.toggleSidebar = function () {
                ctrl.collapsed = !ctrl.collapsed;
                Preferences.set('sidebar.collapsed', ctrl.collapsed);
            };
            ctrl.refresh = function () {
                if (!ctrl.route.product) { return; }
                $rootScope.$broadcast('cluster:refreshRequested', {product: ctrl.route.product});
            };

            $scope.$on('route:changed', function (event, route) {
                ctrl.route = route;
                ctrl.cluster = ClusterStore.state(route.product || 'default');
                ctrl.invalidProduct = ctrl.products.length > 0 && route.product && ctrl.products.indexOf(route.product) < 0;
                ctrl.userMenuOpen = false;
            });
            $scope.$on('cluster:updated', function (event, payload) {
                if (payload.product === ctrl.route.product) { ctrl.cluster = payload.state; }
            });
            ctrl.$onInit = ctrl.loadProducts;
        }
    ]
});
