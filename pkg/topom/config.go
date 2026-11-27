// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"bytes"
	stderrors "errors"
	"net/url"
	"os"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/bytesize"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
	redisutils "github.com/CodisLabs/codis/pkg/utils/redis"
	"github.com/CodisLabs/codis/pkg/utils/timesize"
)

// CoordinatorNameEtcd is the only accepted coordinator_name.
const CoordinatorNameEtcd = "etcd"

// deprecatedCoordinatorSHA is the last commit that supported the zookeeper,
// filesystem and consul coordinators; configuration errors point at it.
const deprecatedCoordinatorSHA = "c635ade0424a1448fb251f32b88d64ca7cf9a555"

const DefaultConfig = `
##################################################
#                                                #
#                  Codis-Dashboard               #
#                                                #
##################################################

# Set Coordinator. etcd is the only supported coordinator.
# Endpoints come from, in order: --etcd, the endpoints field of the
# --etcd-config file, this coordinator_addr, then http://127.0.0.1:2379.
# Credentials and TLS material belong in the --etcd-config file.
coordinator_name = "etcd"
coordinator_addr = "http://127.0.0.1:2379"

# Set Codis Product Name/Auth.
product_name = "codis-demo"
product_auth = ""

# Set backend service auth. Empty values keep using product_auth.
backend_auth_username = ""
backend_auth_password = ""

# Set bind address for admin(rpc), tcp only.
admin_addr = "0.0.0.0:18080"

# Set configs for RDB analysis jobs.
rdb_analysis_workspace = "/tmp/codis-rdb-analysis"
rdb_analysis_max_upload_size = "1gb"
rdb_analysis_max_concurrent_jobs = 1
rdb_analysis_max_jobs_retained = 16
rdb_analysis_max_top_n = 100
rdb_analysis_remote_fetch_enabled = false
rdb_analysis_remote_fetch_auth = ""
rdb_analysis_remote_fetch_timeout = "30m"
rdb_analysis_remote_fetch_max_concurrent = 1

# Set configs for PITR (point-in-time recovery) jobs.
# pitr_enabled default false; enable only on a dashboard that shares a
# filesystem with the target Redis 8 Codis Servers and can run redis-check-aof.
pitr_enabled = false
pitr_redis_check_aof_bin = "bin/redis-check-aof"
pitr_max_concurrent_jobs = 1
pitr_job_timeout = "30m"
pitr_restart_command = ""

# Set configs for the data integration console (data migration driven by the
# embedded RedisShake kernel). Each task runs as one codis-shake worker process
# started by this dashboard; the binary must be executable on the dashboard host
# and must be able to reach the source and target Redis servers directly.
# data_migration_workspace holds spec.json, shake.log and console.log per task.
data_migration_enabled = true
data_migration_workspace = "/tmp/codis-data-migration"
data_migration_shake_bin = "bin/codis-shake"
data_migration_max_concurrent_jobs = 2
data_migration_max_jobs_retained = 50

# Set arguments for data migration (only accept 'sync' & 'semi-async').
migration_method = "semi-async"
migration_parallel_slots = 100
migration_async_maxbulks = 200
migration_async_maxbytes = "32mb"
migration_async_numkeys = 500
migration_timeout = "30s"

# Set configs for redis sentinel.
sentinel_client_timeout = "10s"
sentinel_quorum = 2
sentinel_parallel_syncs = 1
sentinel_down_after = "30s"
sentinel_failover_timeout = "5m"
sentinel_notification_script = ""
sentinel_client_reconfig_script = ""
`

type Config struct {
	CoordinatorName string `toml:"coordinator_name" json:"coordinator_name"`
	CoordinatorAddr string `toml:"coordinator_addr" json:"coordinator_addr"`

	// coordinatorAuthSet records that coordinator_auth was written in the
	// TOML file, even when its value is empty.
	coordinatorAuthSet bool

	AdminAddr string `toml:"admin_addr" json:"admin_addr"`

	HostAdmin string `toml:"-" json:"-"`

	ProductName string `toml:"product_name" json:"product_name"`
	ProductAuth string `toml:"product_auth" json:"-"`

	BackendAuthUsername string `toml:"backend_auth_username" json:"backend_auth_username"`
	BackendAuthPassword string `toml:"backend_auth_password" json:"-"`

	RDBAnalysisWorkspace                string            `toml:"rdb_analysis_workspace" json:"rdb_analysis_workspace"`
	RDBAnalysisMaxUploadSize            bytesize.Int64    `toml:"rdb_analysis_max_upload_size" json:"rdb_analysis_max_upload_size"`
	RDBAnalysisMaxConcurrentJobs        int               `toml:"rdb_analysis_max_concurrent_jobs" json:"rdb_analysis_max_concurrent_jobs"`
	RDBAnalysisMaxJobsRetained          int               `toml:"rdb_analysis_max_jobs_retained" json:"rdb_analysis_max_jobs_retained"`
	RDBAnalysisMaxTopN                  int               `toml:"rdb_analysis_max_top_n" json:"rdb_analysis_max_top_n"`
	RDBAnalysisRemoteFetchEnabled       bool              `toml:"rdb_analysis_remote_fetch_enabled" json:"rdb_analysis_remote_fetch_enabled"`
	RDBAnalysisRemoteFetchAuth          string            `toml:"rdb_analysis_remote_fetch_auth" json:"-"`
	RDBAnalysisRemoteFetchTimeout       timesize.Duration `toml:"rdb_analysis_remote_fetch_timeout" json:"rdb_analysis_remote_fetch_timeout"`
	RDBAnalysisRemoteFetchMaxConcurrent int               `toml:"rdb_analysis_remote_fetch_max_concurrent" json:"rdb_analysis_remote_fetch_max_concurrent"`

	PitrEnabled           bool              `toml:"pitr_enabled" json:"pitr_enabled"`
	PitrRedisCheckAofBin  string            `toml:"pitr_redis_check_aof_bin" json:"pitr_redis_check_aof_bin"`
	PitrMaxConcurrentJobs int               `toml:"pitr_max_concurrent_jobs" json:"pitr_max_concurrent_jobs"`
	PitrJobTimeout        timesize.Duration `toml:"pitr_job_timeout" json:"pitr_job_timeout"`
	PitrRestartCommand    string            `toml:"pitr_restart_command" json:"pitr_restart_command"`

	DataMigrationEnabled           bool   `toml:"data_migration_enabled" json:"data_migration_enabled"`
	DataMigrationWorkspace         string `toml:"data_migration_workspace" json:"data_migration_workspace"`
	DataMigrationShakeBin          string `toml:"data_migration_shake_bin" json:"data_migration_shake_bin"`
	DataMigrationMaxConcurrentJobs int    `toml:"data_migration_max_concurrent_jobs" json:"data_migration_max_concurrent_jobs"`
	DataMigrationMaxJobsRetained   int    `toml:"data_migration_max_jobs_retained" json:"data_migration_max_jobs_retained"`

	MigrationMethod        string            `toml:"migration_method" json:"migration_method"`
	MigrationParallelSlots int               `toml:"migration_parallel_slots" json:"migration_parallel_slots"`
	MigrationAsyncMaxBulks int               `toml:"migration_async_maxbulks" json:"migration_async_maxbulks"`
	MigrationAsyncMaxBytes bytesize.Int64    `toml:"migration_async_maxbytes" json:"migration_async_maxbytes"`
	MigrationAsyncNumKeys  int               `toml:"migration_async_numkeys" json:"migration_async_numkeys"`
	MigrationTimeout       timesize.Duration `toml:"migration_timeout" json:"migration_timeout"`

	SentinelClientTimeout        timesize.Duration `toml:"sentinel_client_timeout" json:"sentinel_client_timeout"`
	SentinelQuorum               int               `toml:"sentinel_quorum" json:"sentinel_quorum"`
	SentinelParallelSyncs        int               `toml:"sentinel_parallel_syncs" json:"sentinel_parallel_syncs"`
	SentinelDownAfter            timesize.Duration `toml:"sentinel_down_after" json:"sentinel_down_after"`
	SentinelFailoverTimeout      timesize.Duration `toml:"sentinel_failover_timeout" json:"sentinel_failover_timeout"`
	SentinelNotificationScript   string            `toml:"sentinel_notification_script" json:"sentinel_notification_script"`
	SentinelClientReconfigScript string            `toml:"sentinel_client_reconfig_script" json:"sentinel_client_reconfig_script"`
}

func NewDefaultConfig() *Config {
	c := &Config{}
	if _, err := toml.Decode(DefaultConfig, c); err != nil {
		log.PanicErrorf(err, "decode toml failed")
	}
	if err := c.Validate(); err != nil {
		log.PanicErrorf(err, "validate config failed")
	}
	return c
}

func (c *Config) LoadFromFile(path string) error {
	blob, err := os.ReadFile(path)
	if err != nil {
		return errors.New("invalid config file: file is not readable")
	}
	md, err := toml.Decode(string(blob), c)
	if err != nil {
		return tomlDecodeError(err)
	}
	c.coordinatorAuthSet = md.IsDefined("coordinator_auth")
	return c.Validate()
}

// tomlDecodeError keeps only the position of a malformed TOML document. The
// underlying message may quote the offending line, which can hold a credential,
// so neither the message nor the raw document is echoed back.
func tomlDecodeError(err error) error {
	var perr toml.ParseError
	if stderrors.As(err, &perr) {
		if perr.LastKey != "" {
			return errors.Errorf("invalid config file: malformed toml at line %d for field %q", perr.Position.Line, perr.LastKey)
		}
		return errors.Errorf("invalid config file: malformed toml at line %d", perr.Position.Line)
	}
	return errors.New("invalid config file: malformed toml")
}

const redactedSecret = "<redacted>"

func redactSecret(s string) string {
	if s == "" {
		return ""
	}
	return redactedSecret
}

// Redacted returns a copy of the config with credentials replaced by a
// placeholder. It is intended for logs and API responses.
func (c *Config) Redacted() *Config {
	if c == nil {
		return nil
	}
	x := *c
	x.ProductAuth = redactSecret(x.ProductAuth)
	x.BackendAuthPassword = redactSecret(x.BackendAuthPassword)
	x.RDBAnalysisRemoteFetchAuth = redactSecret(x.RDBAnalysisRemoteFetchAuth)
	return &x
}

// String renders the config for logs. Credentials are always redacted.
func (c *Config) String() string {
	if c == nil {
		return "<nil>"
	}
	return c.Redacted().encode()
}

func (c *Config) encode() string {
	var b bytes.Buffer
	e := toml.NewEncoder(&b)
	e.Indent = "    "
	e.Encode(c)
	return b.String()
}

func (c *Config) BackendAuthIdentity() redisutils.RedisAuthIdentity {
	if c.BackendAuthUsername != "" || c.BackendAuthPassword != "" {
		return redisutils.RedisAuthIdentity{
			Username: c.BackendAuthUsername,
			Password: c.BackendAuthPassword,
		}
	}
	return redisutils.PasswordAuthIdentity(c.ProductAuth)
}

func (c *Config) Validate() error {
	if c.CoordinatorName != CoordinatorNameEtcd {
		return errors.Errorf("invalid coordinator_name: only %q is supported; zookeeper/filesystem/consul coordinators were last supported at %s",
			CoordinatorNameEtcd, deprecatedCoordinatorSHA)
	}
	if c.coordinatorAuthSet {
		return errors.Errorf("coordinator_auth is no longer supported; put credentials in the --etcd-config file (old coordinators were last supported at %s)",
			deprecatedCoordinatorSHA)
	}
	if err := validateCoordinatorAddr(c.CoordinatorAddr); err != nil {
		return err
	}
	if c.AdminAddr == "" {
		return errors.New("invalid admin_addr")
	}
	if c.ProductName == "" {
		return errors.New("invalid product_name")
	}
	if c.BackendAuthUsername != "" && c.BackendAuthPassword == "" {
		return errors.New("invalid backend_auth_password")
	}
	if c.RDBAnalysisMaxUploadSize < 0 {
		return errors.New("invalid rdb_analysis_max_upload_size")
	}
	if c.RDBAnalysisMaxConcurrentJobs < 0 {
		return errors.New("invalid rdb_analysis_max_concurrent_jobs")
	}
	if c.RDBAnalysisMaxJobsRetained < 0 {
		return errors.New("invalid rdb_analysis_max_jobs_retained")
	}
	if c.RDBAnalysisMaxTopN < 0 {
		return errors.New("invalid rdb_analysis_max_top_n")
	}
	if c.RDBAnalysisRemoteFetchEnabled && c.RDBAnalysisRemoteFetchAuth == "" {
		return errors.New("invalid rdb_analysis_remote_fetch_auth")
	}
	if c.RDBAnalysisRemoteFetchTimeout < 0 {
		return errors.New("invalid rdb_analysis_remote_fetch_timeout")
	}
	if c.RDBAnalysisRemoteFetchMaxConcurrent < 0 {
		return errors.New("invalid rdb_analysis_remote_fetch_max_concurrent")
	}
	if c.PitrMaxConcurrentJobs < 0 {
		return errors.New("invalid pitr_max_concurrent_jobs")
	}
	if c.PitrJobTimeout < 0 {
		return errors.New("invalid pitr_job_timeout")
	}
	if c.DataMigrationMaxConcurrentJobs < 0 {
		return errors.New("invalid data_migration_max_concurrent_jobs")
	}
	if c.DataMigrationMaxJobsRetained < 0 {
		return errors.New("invalid data_migration_max_jobs_retained")
	}
	if _, ok := models.ParseForwardMethod(c.MigrationMethod); !ok {
		return errors.New("invalid migration_method")
	}
	if c.MigrationParallelSlots <= 0 {
		return errors.New("invalid migration_parallel_slots")
	}
	if c.MigrationAsyncMaxBulks <= 0 {
		return errors.New("invalid migration_async_maxbulks")
	}
	if c.MigrationAsyncMaxBytes <= 0 {
		return errors.New("invalid migration_async_maxbytes")
	}
	if c.MigrationAsyncNumKeys <= 0 {
		return errors.New("invalid migration_async_numkeys")
	}
	if c.MigrationTimeout <= 0 {
		return errors.New("invalid migration_timeout")
	}
	if c.SentinelClientTimeout <= 0 {
		return errors.New("invalid sentinel_client_timeout")
	}
	if c.SentinelQuorum <= 0 {
		return errors.New("invalid sentinel_quorum")
	}
	if c.SentinelParallelSyncs <= 0 {
		return errors.New("invalid sentinel_parallel_syncs")
	}
	if c.SentinelDownAfter <= 0 {
		return errors.New("invalid sentinel_down_after")
	}
	if c.SentinelFailoverTimeout <= 0 {
		return errors.New("invalid sentinel_failover_timeout")
	}
	return nil
}

// validateCoordinatorAddr applies the etcd endpoints rules to coordinator_addr:
// one or more comma separated http(s) URLs of one cluster, no embedded
// credentials, no path or query, no mixed schemes, no empty entry. Error texts
// name the rule and never echo the address. The rules are repeated here because
// pkg/topom must not import pkg/models/etcd (architecture D2).
func validateCoordinatorAddr(addr string) error {
	if strings.TrimSpace(addr) == "" {
		return errors.New("invalid coordinator_addr: at least one endpoint is required")
	}
	scheme := ""
	for _, raw := range strings.Split(addr, ",") {
		item := strings.TrimSpace(raw)
		if item == "" {
			return errors.New("invalid coordinator_addr: empty entry")
		}
		u, err := url.Parse(item)
		if err != nil {
			return errors.New("invalid coordinator_addr: malformed URL")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return errors.New("invalid coordinator_addr: scheme must be http or https")
		}
		if u.User != nil {
			return errors.New("invalid coordinator_addr: credentials are not allowed in the URL")
		}
		if u.Host == "" {
			return errors.New("invalid coordinator_addr: missing host")
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return errors.New("invalid coordinator_addr: query and fragment are not allowed")
		}
		if p := strings.TrimSuffix(u.Path, "/"); p != "" {
			return errors.New("invalid coordinator_addr: path is not allowed")
		}
		if scheme == "" {
			scheme = u.Scheme
		} else if scheme != u.Scheme {
			return errors.New("invalid coordinator_addr: cannot mix http and https")
		}
	}
	return nil
}
