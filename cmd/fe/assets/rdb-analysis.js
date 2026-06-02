'use strict';

function initRDBAnalysis($scope, $http, $timeout) {
    var pollTimer = null;
    var flameRowsLimit = 100;

    function stopPoll() {
        if (pollTimer !== null) {
            $timeout.cancel(pollTimer);
            pollTimer = null;
        }
    }

    function buildOptions() {
        var separators = [];
        if ($scope.rdb_options.prefix_separators) {
            var parts = $scope.rdb_options.prefix_separators.split(",");
            for (var i = 0; i < parts.length; i++) {
                var part = parts[i].trim();
                if (part !== "") {
                    separators.push(part);
                }
            }
        }
        return {
            top_n: parseInt($scope.rdb_options.top_n, 10) || 20,
            prefix_separators: separators,
            max_depth: parseInt($scope.rdb_options.max_depth, 10) || 0,
            regex: $scope.rdb_options.regex || "",
            include_expired: !!$scope.rdb_options.include_expired
        };
    }

    function isFinished(status) {
        return status === "done" || status === "error" || status === "canceled";
    }

    function buildFlameRows(root) {
        var rows = [];
        function walk(node, depth, path) {
            if (!node || rows.length >= flameRowsLimit) {
                return;
            }
            var name = path ? path + "/" + node.name : node.name;
            if (depth > 0) {
                rows.push({
                    name: name,
                    depth: depth - 1,
                    size: node.value || 0,
                    size_readable: humanSize(node.value || 0)
                });
            }
            var children = (node.children || []).slice(0);
            children.sort(function (a, b) {
                if ((b.value || 0) === (a.value || 0)) {
                    return (a.name || "").localeCompare(b.name || "");
                }
                return (b.value || 0) - (a.value || 0);
            });
            for (var i = 0; i < children.length && rows.length < flameRowsLimit; i++) {
                walk(children[i], depth + 1, name);
            }
        }
        walk(root, 0, "");
        return rows;
    }

    function pollJob(jobID) {
        var codis_name = $scope.codis_name;
        if (!isValidInput(codis_name) || !jobID) {
            return;
        }
        var xauth = genXAuth(codis_name);
        var url = concatUrl("/api/topom/rdb-analysis/" + xauth + "/" + jobID, codis_name);
        $http.get(url).then(function (resp) {
            if ($scope.codis_name !== codis_name) {
                return;
            }
            $scope.rdb_current_job = resp.data;
            $scope.rdb_flame_rows = buildFlameRows(resp.data.flamegraph);
            $scope.rdb_error = resp.data.error || "";
            if (!isFinished(resp.data.status)) {
                pollTimer = $timeout(function () {
                    pollJob(jobID);
                }, 1500);
            } else {
                pollTimer = null;
            }
        }, function (failedResp) {
            pollTimer = null;
            alertErrorResp(failedResp);
        });
    }

    function startPolling(jobID) {
        stopPoll();
        pollJob(jobID);
    }

    function rdbErrorText(failedResp) {
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

    function showRDBError(failedResp) {
        $scope.rdb_error = rdbErrorText(failedResp);
    }

    $scope.resetRDBAnalysis = function () {
        stopPoll();
        $scope.rdb_current_job = null;
        $scope.rdb_flame_rows = [];
        $scope.rdb_error = "";
        $scope.rdb_upload_file = null;
        $scope.rdb_path = "";
        $scope.rdb_remote_server = "";
        $scope.rdb_options = {
            top_n: 20,
            prefix_separators: ":",
            max_depth: 3,
            regex: "",
            include_expired: false
        };
    };

    $scope.setRDBAnalysisFile = function (files) {
        $scope.$apply(function () {
            $scope.rdb_upload_file = files && files.length ? files[0] : null;
        });
    };

    $scope.rdbAnalysisRunning = function () {
        return $scope.rdb_current_job && !isFinished($scope.rdb_current_job.status);
    };

    $scope.rdbProgressPercent = function () {
        var job = $scope.rdb_current_job;
        if (!job || !job.file_size || job.file_size <= 0) {
            return 0;
        }
        var value = Math.floor((job.bytes_read * 100) / job.file_size);
        if (value < 0) {
            return 0;
        }
        if (value > 100) {
            return 100;
        }
        return value;
    };

    $scope.rdbReadableSize = function (size) {
        return humanSize(size || 0);
    };

    $scope.startRDBAnalysisByPath = function () {
        var codis_name = $scope.codis_name;
        if (!isValidInput(codis_name) || !isValidInput($scope.rdb_path) || $scope.rdbAnalysisRunning()) {
            return;
        }
        var xauth = genXAuth(codis_name);
        var url = concatUrl("/api/topom/rdb-analysis/start/" + xauth, codis_name);
        $http.put(url, {
            path: $scope.rdb_path,
            options: buildOptions()
        }).then(function (resp) {
            $scope.rdb_error = "";
            $scope.rdb_current_job = {id: resp.data.id, status: "queued"};
            $scope.rdb_flame_rows = [];
            startPolling(resp.data.id);
        }, function (failedResp) {
            showRDBError(failedResp);
        });
    };

    $scope.startRDBAnalysisUpload = function () {
        var codis_name = $scope.codis_name;
        if (!isValidInput(codis_name) || !$scope.rdb_upload_file || $scope.rdbAnalysisRunning()) {
            return;
        }
        var xauth = genXAuth(codis_name);
        var url = concatUrl("/api/topom/rdb-analysis/upload/" + xauth, codis_name);
        var form = new FormData();
        form.append("file", $scope.rdb_upload_file);
        form.append("options", angular.toJson(buildOptions()));
        $http.post(url, form, {
            transformRequest: angular.identity,
            headers: {"Content-Type": undefined}
        }).then(function (resp) {
            $scope.rdb_error = "";
            $scope.rdb_current_job = {id: resp.data.id, status: "queued"};
            $scope.rdb_flame_rows = [];
            startPolling(resp.data.id);
        }, function (failedResp) {
            showRDBError(failedResp);
        });
    };

    $scope.startRDBAnalysisRemoteFetch = function () {
        var codis_name = $scope.codis_name;
        if (!isValidInput(codis_name) || !isValidInput($scope.rdb_remote_server) || $scope.rdbAnalysisRunning()) {
            return;
        }
        var xauth = genXAuth(codis_name);
        var url = concatUrl("/api/topom/rdb-analysis/remote-fetch/" + xauth, codis_name);
        $http.put(url, {
            server_addr: $scope.rdb_remote_server,
            options: buildOptions()
        }).then(function (resp) {
            $scope.rdb_error = "";
            $scope.rdb_current_job = {id: resp.data.id, status: "queued"};
            $scope.rdb_flame_rows = [];
            startPolling(resp.data.id);
        }, function (failedResp) {
            showRDBError(failedResp);
        });
    };

    $scope.cancelRDBAnalysis = function () {
        var job = $scope.rdb_current_job;
        var codis_name = $scope.codis_name;
        if (!job || !job.id || !isValidInput(codis_name)) {
            return;
        }
        var xauth = genXAuth(codis_name);
        var url = concatUrl("/api/topom/rdb-analysis/cancel/" + xauth + "/" + job.id, codis_name);
        $http.put(url).then(function () {
            stopPoll();
            startPolling(job.id);
        }, function (failedResp) {
            showRDBError(failedResp);
        });
    };

    $scope.clearRDBAnalysis = function () {
        var job = $scope.rdb_current_job;
        var codis_name = $scope.codis_name;
        stopPoll();
        if (!job || !job.id || !isValidInput(codis_name)) {
            $scope.rdb_current_job = null;
            $scope.rdb_flame_rows = [];
            return;
        }
        var xauth = genXAuth(codis_name);
        var url = concatUrl("/api/topom/rdb-analysis/remove/" + xauth + "/" + job.id, codis_name);
        $http.put(url).then(function () {
            $scope.rdb_current_job = null;
            $scope.rdb_flame_rows = [];
            $scope.rdb_error = "";
        }, function () {
            $scope.rdb_current_job = null;
            $scope.rdb_flame_rows = [];
        });
    };
}
