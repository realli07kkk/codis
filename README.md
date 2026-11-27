<img src="doc/pictures/logo-3.png" height=80></img>

[![Gitter](https://badges.gitter.im/Join%20Chat.svg)](https://gitter.im/CodisLabs/codis?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge)
[![Build Status](https://travis-ci.org/CodisLabs/codis.svg)](https://travis-ci.org/CodisLabs/codis)

Codis is a proxy based high performance Redis cluster solution written in Go. It is production-ready and widely used at [wandoujia.com](http://wandoujia.com) and many companies. You can see [Codis Releases](https://github.com/CodisLabs/codis/releases) for latest and most stable realeases.

## Donation
Donate if you want to help us maintaining this project. Thank you!
[See this issue for details](https://github.com/CodisLabs/codis/issues/976)

## Compared with Twemproxy and Redis Cluster
<table>
<tr><th></th><th>Codis</th><th>Twemproxy</th><th>Redis Cluster</th></tr>
<tr><td>resharding without restarting cluster</td><td>Yes</td><td>No</td><td>Yes</td></tr>
<tr><td>pipeline</td><td>Yes</td><td>Yes</td><td>No</td></tr>
<tr><td>hash tags for multi-key operations</td><td>Yes</td><td>Yes</td><td>Yes</td></tr>
<tr><td>multi-key operations while resharding</td><td>Yes</td><td>-</td><td>No(<a href="http://redis.io/topics/cluster-spec#multiple-keys-operations">details</a>)</td></tr>
<tr><td>Redis clients supporting</td><td>Any clients</td><td>Any clients</td><td>Clients have to support cluster protocol</td></tr>
</table>
"Resharding" means migrating the data in one slot from one redis server to another, usually happens while increasing/decreasing the number of redis servers.

## Other Features
* GUI website dashboard & admin tools
* Supports most of Redis commands, Fully compatible with Twemproxy(https://github.com/twitter/twemproxy)
* Proxies register their online records in etcd 3.7, so clients and cluster-mode SDKs can avoid dead proxies, see "High Availability" section.

## Version boundary

The current mainline supports only an externally deployed etcd 3.7.x as the coordinator (`go.etcd.io/etcd/client/v3 v3.7.4`): the control plane lives in etcd, business keys stay in codis-server. The new binaries carry no legacy drivers, no fallback switch, no dual write and no legacy proxy registration protocol; removed CLI/TOML options are rejected with the commit that last supported them.

The ZooKeeper, Consul and filesystem coordinators and the Jodis discovery mechanism were last supported at commit `c635ade0424a1448fb251f32b88d64ca7cf9a555`; check that out and run `make` as described in that commit. See the [etcd 3.7 release notes](doc/etcd_3_7_release_notes_zh.md) for the incompatible changes and upgrade notes.

## Tutorial

[简体中文](doc/tutorial_zh.md)
[English (WIP) ](doc/tutorial_en.md)

Quick start: prepare etcd 3.7.x, then follow [step 1 of the Chinese tutorial](doc/tutorial_zh.md#1-快速启动) (etcd connection file, the four startup commands, two groups and slot assignment, proxy online registration). The Chinese tutorial also documents the persistent topom lock and its manual recovery.

[All documents](doc/README.md)

## FAQ

[简体中文](doc/FAQ_zh.md)
[English (WIP) ](doc/FAQ_en.md)

## High Availability

[简体中文](doc/tutorial_zh.md#3-proxy-高可用与在线发现)
[English (WIP) ](doc/tutorial_en.md#proxy-availability-and-discovery)

## Architecture

![architecture](doc/pictures/architecture.png)

The drawing predates the etcd 3.7 line: the coordinator box in it stands for the control-plane store, which is now an externally deployed etcd 3.7.x. The current component interaction, namespaces and code map are in [architecture_zh.md](doc/architecture_zh.md).

## Snapshots

Proxy
![proxy](doc/pictures/snapshots1.png)

Slots
![slots](doc/pictures/snapshots2.png)

Group
![group](doc/pictures/snapshots3.png)

Sentinel
![sentinel](doc/pictures/snapshots4.png)

## Benchmarks
[See benchmark results](doc/benchmark.md)

## Authors

Active authors:
* [@spinlock9](https://github.com/spinlock) [微博@斯宾洛克](http://weibo.com/spinlock9)
* [@yangzhe1991](https://github.com/yangzhe1991) [微博@\_杨肉\_](http://weibo.com/yangzhe1991)

Emeritus authors:
* [@goroutine](https://github.com/ngaut) [微博@goroutine](http://weibo.com/u/1923497393)
* [@c4pt0r](https://github.com/c4pt0r) [微博@Dongxu\_Huang](http://weibo.com/c4pt0r)

Thanks:
* [@ivanzhaowy](https://github.com/ivanzhaowy)
* [@Apache9](https://github.com/apache9) [微博@Apache9](http://weibo.com/u/1876829375)

## License

Codis is licensed under MIT， see MIT-LICENSE.txt

-------------
*You are welcome to use Codis in your product, and feel free to let us know~ :)*
