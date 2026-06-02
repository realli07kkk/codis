module github.com/CodisLabs/codis

go 1.26.1

require (
	github.com/BurntSushi/toml v0.2.1-0.20160717150709-99064174e013
	github.com/coreos/etcd v3.3.27+incompatible
	github.com/docopt/docopt-go v0.0.0-20160216232012-784ddc588536
	github.com/emirpasic/gods v1.9.0
	github.com/garyburd/redigo v1.0.1-0.20170208211623-48545177e92a
	github.com/go-martini/martini v0.0.0-20160908070901-fe605b5cd210
	github.com/google/uuid v1.5.0
	github.com/hashicorp/consul/api v1.34.2
	github.com/hdt3213/rdb v1.3.2
	github.com/influxdata/influxdb v1.1.1-0.20170109231301-8c2cfd14af25
	github.com/martini-contrib/binding v0.0.0-20160701174519-05d3e151b6cf
	github.com/martini-contrib/gzip v0.0.0-20151124214156-6c035326b43f
	github.com/martini-contrib/render v0.0.0-20150707142108-ec18f8345a11
	github.com/oxtoacart/bpool v0.0.0-20150712133111-4e1c5567d7c2
	github.com/samuel/go-zookeeper v0.0.0-20161028232340-1d7be4effb13
	github.com/spinlock/jemalloc-go v0.0.0-20161230074307-26719b2ee618
	github.com/ugorji/go v1.2.14
	golang.org/x/net v0.51.0
	gopkg.in/alexcesaro/statsd.v2 v2.0.0
)

require (
	github.com/armon/go-metrics v0.4.1 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic v1.15.0 // indirect
	github.com/bytedance/sonic/loader v0.5.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/codegangsta/inject v0.0.0-20150114235600-33e0aa1cb7c0 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
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
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.2.9 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	golang.org/x/arch v0.9.0 // indirect
	golang.org/x/exp v0.0.0-20260218203240-3dfff04db8fa // indirect
	golang.org/x/sys v0.41.0 // indirect
)

replace github.com/spinlock/jemalloc-go => ./third_party/jemalloc-go
