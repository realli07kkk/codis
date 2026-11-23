// Package config holds the RedisShake runtime options.
//
// Vendored from RedisShake v4.8.0 (internal/config/config.go). The TOML /
// viper file loading of upstream LoadConfig is not used by codis-shake, which
// receives a JSON spec from codis-dashboard, so it is replaced by Default()
// which applies the same struct defaults that upstream applied at startup.
package config

import (
	"strings"

	"github.com/mcuadros/go-defaults"

	"github.com/CodisLabs/codis/pkg/shake/log"
)

type FilterOptions struct {
	AllowKeys         []string `mapstructure:"allow_keys" default:"[]"`
	AllowKeyPrefix    []string `mapstructure:"allow_key_prefix" default:"[]"`
	AllowKeySuffix    []string `mapstructure:"allow_key_suffix" default:"[]"`
	BlockKeys         []string `mapstructure:"block_keys" default:"[]"`
	BlockKeyPrefix    []string `mapstructure:"block_key_prefix" default:"[]"`
	BlockKeySuffix    []string `mapstructure:"block_key_suffix" default:"[]"`
	AllowKeyRegex     []string `mapstructure:"allow_key_regex" default:"[]"`
	BlockKeyRegex     []string `mapstructure:"block_key_regex" default:"[]"`
	AllowDB           []int    `mapstructure:"allow_db" default:"[]"`
	BlockDB           []int    `mapstructure:"block_db" default:"[]"`
	AllowCommand      []string `mapstructure:"allow_command" default:"[]"`
	BlockCommand      []string `mapstructure:"block_command" default:"[]"`
	AllowCommandGroup []string `mapstructure:"allow_command_group" default:"[]"`
	BlockCommandGroup []string `mapstructure:"block_command_group" default:"[]"`
	AllowSlot         []string `mapstructure:"allow_slot" default:"[]"`
	BlockSlot         []string `mapstructure:"block_slot" default:"[]"`
	Function          string   `mapstructure:"function" default:""`
}

type AdvancedOptions struct {
	Dir string `mapstructure:"dir" default:"data"`

	Ncpu int `mapstructure:"ncpu" default:"0"`

	PprofPort  int `mapstructure:"pprof_port" default:"0"`
	StatusPort int `mapstructure:"status_port" default:"0"`

	// log
	LogFile       string `mapstructure:"log_file" default:"shake.log"`
	LogLevel      string `mapstructure:"log_level" default:"info"`
	LogInterval   int    `mapstructure:"log_interval" default:"5"`
	LogRotation   bool   `mapstructure:"log_rotation" default:"true"`
	LogMaxSize    int    `mapstructure:"log_max_size" default:"512"`
	LogMaxAge     int    `mapstructure:"log_max_age" default:"7"`
	LogMaxBackups int    `mapstructure:"log_max_backups" default:"3"`
	LogCompress   bool   `mapstructure:"log_compress" default:"true"`

	// redis-shake gets key and value from rdb file, and uses RESTORE command to
	// create the key in target redis. Redis RESTORE will return a "Target key name
	// is busy" error when key already exists. You can use this configuration item
	// to change the default behavior of restore:
	// panic:   redis-shake will stop when meet "Target key name is busy" error.
	// rewrite: redis-shake will replace the key with new value.
	// skip:    redis-shake will skip restore the key when meet "Target key name is busy" error.
	//
	// Applicability:
	// - rdb_reader:     Applies (uses RESTORE command)
	// - sync_reader:    Only applies to RDB phase. AOF phase forwards commands directly without RESTORE.
	// - scan_reader:    Applies (uses RESTORE command)
	//
	// Limitation:
	// For large values exceeding target_redis_proto_max_bulk_len, individual commands (SET, HSET, etc.)
	// are used instead of RESTORE, which may not respect this setting.
	RDBRestoreCommandBehavior string `mapstructure:"rdb_restore_command_behavior" default:"panic"`

	PipelineCountLimit              uint64 `mapstructure:"pipeline_count_limit" default:"1024"`
	TargetRedisClientMaxQuerybufLen int64  `mapstructure:"target_redis_client_max_querybuf_len" default:"1024000000"`
	TargetRedisProtoMaxBulkLen      uint64 `mapstructure:"target_redis_proto_max_bulk_len" default:"512000000"`
	TargetRedisMaxQPS               int    `mapstructure:"target_redis_max_qps" default:"300000"`

	AwsPSync string `mapstructure:"aws_psync" default:""` // 10.0.0.1:6379@nmfu2sl5osync,10.0.0.1:6379@xhma21xfkssync

	EmptyDBBeforeSync bool `mapstructure:"empty_db_before_sync" default:"false"`
}

type ModuleOptions struct {
	TargetMBbloomVersion int `mapstructure:"target_mbbloom_version" default:"0"` // v1.0.0 <=> 10000
}

func (opt *AdvancedOptions) GetPSyncCommand(address string) string {
	items := strings.Split(opt.AwsPSync, ",")
	for _, item := range items {
		if strings.HasPrefix(item, address) {
			return strings.Split(item, "@")[1]
		}
	}
	log.Panicf("can not find aws psync command. address=[%s],aws_psync=[%s]", address, opt.AwsPSync)
	return ""
}

type ShakeOptions struct {
	Filter   FilterOptions
	Advanced AdvancedOptions
	Module   ModuleOptions
}

var Opt ShakeOptions

// Default returns the option set with every struct default applied, the
// same state upstream reached with defaults.SetDefaults(&Opt) before
// unmarshalling a TOML file.
func Default() ShakeOptions {
	var opt ShakeOptions
	defaults.SetDefaults(&opt)
	return opt
}
