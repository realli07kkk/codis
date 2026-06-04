'use strict';

function initQPSLimitEditor($scope, $http, $timeout) {
    var maxSafeInteger = Number.MAX_SAFE_INTEGER || 9007199254740991;

    function qpsLimitErrorText(failedResp) {
        if (!failedResp) {
            return "error response";
        }
        if ((failedResp.status === 1500 || failedResp.status === 800) && failedResp.data) {
            return failedResp.data.Cause || angular.toJson(failedResp.data);
        }
        if (failedResp.data) {
            return failedResp.data.toString();
        }
        return "error response";
    }

    function parseLimit() {
        var raw = "";
        if ($scope.qps_limit_edit && $scope.qps_limit_edit.limit_text !== undefined) {
            raw = $scope.qps_limit_edit.limit_text.toString().trim();
        }
        if (!/^\d+$/.test(raw)) {
            $scope.qps_limit_error = "invalid qps limit";
            return null;
        }
        var limit = Number(raw);
        if (!isFinite(limit) || Math.floor(limit) !== limit || limit < 0 || limit > maxSafeInteger) {
            $scope.qps_limit_error = "invalid qps limit";
            return null;
        }
        return limit;
    }

    $scope.resetQPSLimitEditor = function () {
        $scope.qps_limit_model = {revision: 0, limit: 0, enabled: false, sync_status: "not_configured"};
        $scope.qps_limit_edit = {limit_text: "0"};
        $scope.qps_limit_error = "";
        $scope.qps_limit_saving = false;
    };

    $scope.loadQPSLimit = function () {
        var codis_name = $scope.codis_name;
        if (!isValidInput(codis_name)) {
            return;
        }
        var xauth = genXAuth(codis_name);
        var url = concatUrl("/api/topom/proxy/qps-limit/" + xauth, codis_name);
        $http.get(url).then(function (resp) {
            if ($scope.codis_name !== codis_name) {
                return;
            }
            $scope.qps_limit_model = resp.data;
            $scope.qps_limit_edit = {limit_text: (resp.data.limit || 0).toString()};
            $scope.qps_limit_error = "";
        }, function (failedResp) {
            $scope.qps_limit_error = qpsLimitErrorText(failedResp);
        });
    };

    $scope.submitQPSLimit = function () {
        var codis_name = $scope.codis_name;
        if (!isValidInput(codis_name)) {
            return;
        }
        var limit = parseLimit();
        if (limit === null) {
            return;
        }
        alertAction("Apply proxy QPS limit", function () {
            var xauth = genXAuth(codis_name);
            var url = concatUrl("/api/topom/proxy/qps-limit/" + xauth, codis_name);
            $scope.qps_limit_saving = true;
            $http.put(url, {limit: limit}).then(function () {
                $scope.qps_limit_error = "";
                $scope.qps_limit_saving = false;
                $scope.loadQPSLimit();
            }, function (failedResp) {
                $scope.qps_limit_saving = false;
                $scope.qps_limit_error = qpsLimitErrorText(failedResp);
            });
        });
    };

    $scope.resetQPSLimitEditor();
}
