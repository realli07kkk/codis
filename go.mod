module github.com/CodisLabs/codis

go 1.26.1

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/coreos/etcd v3.3.27+incompatible
	github.com/docopt/docopt-go v0.0.0-20180111231733-ee0de3bc6815
	github.com/emirpasic/gods v1.18.1
	github.com/go-martini/martini v0.0.0-20170121215854-22fa46961aab
	github.com/go-zookeeper/zk v1.0.4
	github.com/google/uuid v1.6.0
	github.com/hashicorp/consul/api v1.34.3
	github.com/hdt3213/rdb v1.3.2
	github.com/influxdata/influxdb v1.12.4
	github.com/martini-contrib/binding v0.0.0-20160701174519-05d3e151b6cf
	github.com/martini-contrib/gzip v0.0.0-20151124214156-6c035326b43f
	github.com/martini-contrib/render v0.0.0-20150707142108-ec18f8345a11
	github.com/oxtoacart/bpool v0.0.0-20190530202638-03653db5a59c
	github.com/redis/go-redis/v9 v9.20.0
	github.com/spinlock/jemalloc-go v0.0.0-20201010032256-e81523fb8524
	github.com/ugorji/go v1.2.14
	golang.org/x/net v0.55.0
	gopkg.in/alexcesaro/statsd.v2 v2.0.0
)

require (
	github.com/Masterminds/semver v1.4.2 // indirect
	github.com/Masterminds/sprig v2.16.0+incompatible // indirect
	github.com/aokoli/goutils v1.0.1 // indirect
	github.com/armon/go-metrics v0.4.1 // indirect
	github.com/benbjohnson/tmpl v1.0.0 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic v1.15.0 // indirect
	github.com/bytedance/sonic/loader v0.5.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/codegangsta/inject v0.0.0-20150114235600-33e0aa1cb7c0 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/fatih/color v1.16.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.5.0 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/hashicorp/serf v0.10.1 // indirect
	github.com/huandu/xstrings v1.0.0 // indirect
	github.com/imdario/mergo v0.3.5 // indirect
	github.com/influxdata/pkg-config v0.2.14 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.16.0 // indirect
	golang.org/x/arch v0.9.0 // indirect
	golang.org/x/crypto v0.51.0 // indirect
	golang.org/x/exp v0.0.0-20260218203240-3dfff04db8fa // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/telemetry v0.0.0-20260209163413-e7419c687ee4 // indirect
	golang.org/x/tools v0.42.0 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
)

replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go
