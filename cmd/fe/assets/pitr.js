'use strict';

// initPitr wires the PITR (point-in-time recovery) region: a create form
// (server addr + truncate timestamp) and a job list that refreshes on demand
// and while any job is non-terminal. Mirrors the qps-limit / rdb-analysis FE
// conventions: all calls go through the dashboard reverse proxy with xauth,
// secrets are never read from or written to the FE.
function initPitr($scope, $http, $timeout) {
    var pollHandle = null;

    function pitrErrorText(failedResp) {
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

    function isTerminal(state) {
        return state === "succeeded" || state === "failed" || state === "cancelled";
    }

    function anyNonTerminal() {
        if (!$scope.pitr_jobs) {
            return false;
        }
        for (var i = 0; i < $scope.pitr_jobs.length; i++) {
            if (!isTerminal($scope.pitr_jobs[i].state)) {
                return true;
            }
        }
        return false;
    }

    function schedulePoll() {
        cancelPoll();
        if (!anyNonTerminal()) {
            return;
        }
        pollHandle = $timeout(function () {
            pollHandle = null;
            $scope.loadPitrJobs(true);
        }, 2000);
    }

    function cancelPoll() {
        if (pollHandle !== null) {
            $timeout.cancel(pollHandle);
            pollHandle = null;
        }
    }

    $scope.resetPitrEditor = function () {
        $scope.pitr_create = {server_addr: "", truncate_ts: ""};
        $scope.pitr_jobs = null;
        $scope.pitr_error = "";
        $scope.pitr_creating = false;
        cancelPoll();
    };

    $scope.loadPitrJobs = function (isPoll) {
        var codis_name = $scope.codis_name;
        if (!isValidInput(codis_name)) {
            return;
        }
        var xauth = genXAuth(codis_name);
        var url = concatUrl("/api/topom/pitr/jobs/" + xauth, codis_name);
        $http.get(url).then(function (resp) {
            if ($scope.codis_name !== codis_name) {
                return;
            }
            $scope.pitr_jobs = resp.data || [];
            if (!isPoll) {
                $scope.pitr_error = "";
            }
            schedulePoll();
        }, function (failedResp) {
            if (!isPoll) {
                $scope.pitr_error = pitrErrorText(failedResp);
            }
        });
    };

    $scope.submitPitrCreate = function () {
        var codis_name = $scope.codis_name;
        if (!isValidInput(codis_name)) {
            return;
        }
        var serverAddr = ($scope.pitr_create.server_addr || "").trim();
        var tsText = ($scope.pitr_create.truncate_ts || "").trim();
        if (!serverAddr) {
            $scope.pitr_error = "missing server_addr";
            return;
        }
        if (!/^\d+$/.test(tsText)) {
            $scope.pitr_error = "invalid truncate_ts (unix seconds)";
            return;
        }
        var ts = Number(tsText);
        alertAction("Create PITR job for " + serverAddr + " @ " + tsText, function () {
            var xauth = genXAuth(codis_name);
            var url = concatUrl("/api/topom/pitr/create/" + xauth, codis_name);
            $scope.pitr_creating = true;
            $http.put(url, {
                server_addr: serverAddr,
                truncate_ts: ts
            }).then(function () {
                $scope.pitr_creating = false;
                $scope.pitr_error = "";
                $scope.pitr_create = {server_addr: "", truncate_ts: ""};
                $scope.loadPitrJobs();
            }, function (failedResp) {
                $scope.pitr_creating = false;
                $scope.pitr_error = pitrErrorText(failedResp);
            });
        });
    };

    $scope.cancelPitrJob = function (id) {
        var codis_name = $scope.codis_name;
        var xauth = genXAuth(codis_name);
        var url = concatUrl("/api/topom/pitr/cancel/" + xauth + "/" + id, codis_name);
        $http.put(url).then(function () {
            $scope.loadPitrJobs();
        }, function (failedResp) {
            $scope.pitr_error = pitrErrorText(failedResp);
        });
    };

    $scope.removePitrJob = function (id) {
        var codis_name = $scope.codis_name;
        alertAction("Remove PITR job " + id, function () {
            var xauth = genXAuth(codis_name);
            var url = concatUrl("/api/topom/pitr/remove/" + xauth + "/" + id, codis_name);
            $http.put(url).then(function () {
                $scope.loadPitrJobs();
            }, function (failedResp) {
                $scope.pitr_error = pitrErrorText(failedResp);
            });
        });
    };

    $scope.$on("$destroy", cancelPoll);
    $scope.resetPitrEditor();
}
