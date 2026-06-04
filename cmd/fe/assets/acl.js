'use strict';

function initACLEditor($scope, $http, $timeout) {
    function aclErrorText(failedResp) {
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

    function splitRules(text) {
        var rules = [];
        var parts = (text || "").split(/\s+/);
        for (var i = 0; i < parts.length; i++) {
            var token = parts[i].trim();
            if (token !== "") {
                rules.push(token);
            }
        }
        return rules;
    }

    function cloneACLUsers() {
        var users = [];
        for (var i = 0; i < ($scope.acl_users || []).length; i++) {
            var user = $scope.acl_users[i];
            users.push({
                name: user.name,
                enabled: !!user.enabled,
                rules: (user.rules || []).slice(0)
            });
        }
        return users;
    }

    function submitACLUsers(users, success) {
        var codis_name = $scope.codis_name;
        if (!isValidInput(codis_name)) {
            return;
        }
        var xauth = genXAuth(codis_name);
        var url = concatUrl("/api/topom/acl/" + xauth, codis_name);
        var req = {
            enabled: !!($scope.acl_model && $scope.acl_model.enabled),
            users: users
        };
        $http.put(url, req).then(function () {
            $scope.acl_error = "";
            if (success) {
                success();
            }
            $scope.loadACL();
        }, function (failedResp) {
            $scope.acl_error = aclErrorText(failedResp);
        });
    }

    $scope.resetACLEditor = function () {
        $scope.acl_model = {revision: 0, enabled: false, sync_status: "not_configured", users: []};
        $scope.acl_users = [];
        $scope.acl_error = "";
        $scope.acl_editing = false;
        $scope.acl_edit = null;
    };

    $scope.loadACL = function () {
        var codis_name = $scope.codis_name;
        if (!isValidInput(codis_name)) {
            return;
        }
        var xauth = genXAuth(codis_name);
        var url = concatUrl("/api/topom/acl/" + xauth, codis_name);
        $http.get(url).then(function (resp) {
            if ($scope.codis_name !== codis_name) {
                return;
            }
            $scope.acl_model = resp.data;
            $scope.acl_users = resp.data.users || [];
            $scope.acl_error = "";
        }, function (failedResp) {
            $scope.acl_error = aclErrorText(failedResp);
        });
    };

    $scope.submitACLConfig = function () {
        alertAction("Apply ACL revision", function () {
            submitACLUsers(cloneACLUsers(), null);
        });
    };

    $scope.newACLUser = function () {
        $scope.acl_error = "";
        $scope.acl_editing = true;
        $scope.acl_edit = {
            is_new: true,
            name: "",
            enabled: true,
            new_password: "",
            rules_text: ""
        };
    };

    $scope.editACLUser = function (user) {
        $scope.acl_error = "";
        $scope.acl_editing = true;
        $scope.acl_edit = {
            is_new: false,
            name: user.name,
            enabled: !!user.enabled,
            new_password: "",
            rules_text: (user.rules || []).join("\n")
        };
    };

    $scope.cancelACLUserEdit = function () {
        $scope.acl_editing = false;
        $scope.acl_edit = null;
    };

    $scope.saveACLUser = function () {
        if (!$scope.acl_edit) {
            return;
        }
        var edit = $scope.acl_edit;
        var name = (edit.name || "").trim();
        var rules = splitRules(edit.rules_text);
        if (name === "" || rules.length === 0) {
            $scope.acl_error = "invalid acl user";
            return;
        }
        var users = cloneACLUsers();
        var updated = {
            name: name,
            enabled: !!edit.enabled,
            rules: rules
        };
        if (edit.new_password) {
            updated.new_password = edit.new_password;
        }
        var found = false;
        for (var i = 0; i < users.length; i++) {
            if (users[i].name === name) {
                users[i] = updated;
                found = true;
                break;
            }
        }
        if (!found) {
            users.push(updated);
        }
        alertAction("Save ACL user " + name, function () {
            submitACLUsers(users, function () {
                edit.new_password = "";
                $scope.cancelACLUserEdit();
            });
        });
    };

    $scope.deleteACLUser = function (user) {
        var users = [];
        for (var i = 0; i < ($scope.acl_users || []).length; i++) {
            if ($scope.acl_users[i].name !== user.name) {
                users.push({
                    name: $scope.acl_users[i].name,
                    enabled: !!$scope.acl_users[i].enabled,
                    rules: ($scope.acl_users[i].rules || []).slice(0)
                });
            }
        }
        alertAction("Delete ACL user " + user.name, function () {
            submitACLUsers(users, null);
        });
    };

    $scope.resetACLEditor();
}
