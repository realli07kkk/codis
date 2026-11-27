#!/bin/bash

if [ $(kubectl get pods -l app=etcd |grep Running |wc -l) == 0 ]; then
    echo "start create etcd cluster"
    kubectl create -f etcd/etcd-service.yaml
    kubectl create -f etcd/etcd.yaml
    while [ $(kubectl get pods -l app=etcd |grep Running |wc -l) != 1 ]; do sleep 1; done;
    echo "finish create etcd cluster"
fi

product_name="codis-test"
#product_auth="auth"

### etcd client used by cleanup/buildup. The demo is a single member listening
### on the plain HTTP client port; for a TLS cluster pass --cacert/--cert/--key
### and use https endpoints.
codis_etcdctl() {
    kubectl exec etcd-0 -- etcdctl --endpoints=http://127.0.0.1:2379 "$@"
}

case "$1" in

### 清理原来codis遗留数据
cleanup)
    kubectl delete -f .
    # 如果etcd不在kubernetes上，需要在etcd机器上执行:
    #   etcdctl --endpoints={etcd-addr}:2379 del /codis3/$product_name/ --prefix
    codis_etcdctl del /codis3/$product_name/ --prefix
    ;;

### 创建新的codis集群
buildup)
    kubectl delete -f .
    # 如果etcd不在kubernetes上，需要在etcd机器上执行:
    #   etcdctl --endpoints={etcd-addr}:2379 del /codis3/$product_name/ --prefix
    codis_etcdctl del /codis3/$product_name/ --prefix
    # 首次建群预检：product 前缀为空才继续，非空停止本次新建，读失败直接报错
    if ! existing=$(codis_etcdctl get /codis3/$product_name/ --prefix); then
        echo "read etcd prefix /codis3/$product_name/ failed, abort"
        exit 1
    fi
    if [ -n "$existing" ]; then
        echo "product $product_name is not empty in etcd, keep the existing cluster or remove the prefix first"
        exit 1
    fi
    kubectl create -f codis-etcd-config.yaml
    kubectl create -f codis-service.yaml
    kubectl create -f codis-dashboard.yaml
    while [ $(kubectl get pods -l app=codis-dashboard |grep Running |wc -l) != 1 ]; do sleep 1; done;
    kubectl create -f codis-proxy.yaml
    kubectl create -f codis-server.yaml
    servers=$(grep "replicas" codis-server.yaml |awk  '{print $2}')
    while [ $(kubectl get pods -l app=codis-server |grep Running |wc -l) != $servers ]; do sleep 1; done;
    kubectl exec -it codis-server-0 -- codis-admin  --dashboard=codis-dashboard:18080 --rebalance --confirm
    kubectl create -f codis-ha.yaml
    kubectl create -f codis-fe.yaml
    sleep 60
    kubectl exec -it codis-dashboard-0 -- redis-cli -h codis-proxy -p 19000 PING
    if [ $? != 0 ]; then
        echo "buildup codis cluster with problems, plz check it!!"
    fi
    ;;

### 扩容／缩容 codis proxy
scale-proxy)
    kubectl scale rc codis-proxy --replicas=$2
    ;;

### 扩容／[缩容] codis server
scale-server)
    cur=$(kubectl get statefulset codis-server |tail -n 1 |awk '{print $3}')
    des=$2
    echo $cur
    echo $des
    if [ $cur == $des ]; then
        echo "current server == desired server, return"
    elif [ $cur -lt $des ]; then
        kubectl scale statefulsets codis-server --replicas=$des
        while [ $(kubectl get pods -l app=codis-server |grep Running |wc -l) != $2 ]; do sleep 1; done;
        kubectl exec -it codis-server-0 -- codis-admin  --dashboard=codis-dashboard:18080 --rebalance --confirm
    else
        echo "reduce the number of codis-server, does not support, please wait"
        # while [ $cur > $des ]
        # do
        #    cur=`expr $cur - 2`
        #    gid=$(expr $cur / 2 + 1)
        #    kubectl exec -it codis-server-0 -- codis-admin  --dashboard=codis-dashboard:18080 --slot-action --create-some --gid-from=$gid --gid-to=1 --num-slots=1024
        #    while [ $(kubectl exec -it codis-server-0 -- codis-admin  --dashboard=codis-dashboard:18080  --slots-status |grep "\"backend_addr_group_id\": $gid" |wc -l) != 0 ]; do echo "waiting slot migrating..."; sleep 1; done;
        #    kubectl scale statefulsets codis-server --replicas=$cur
        #    kubectl exec -it codis-server-0 -- codis-admin  --dashboard=codis-dashboard:18080 --remove-group --gid=$gid
        # done
        # kubectl scale statefulsets codis-server --replicas=$des
        # kubectl exec -it codis-server-0 -- codis-admin  --dashboard=codis-dashboard:18080 --rebalance --confirm
    fi
    ;;

*)
    echo "wrong argument(s)"
    ;;

esac
