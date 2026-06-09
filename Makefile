.DEFAULT_GOAL := build-all

VERSION_LDFLAGS = -ldflags "$$(cat bin/version.ldflags)"
REDIS3_DIR ?= extern/redis-3.2.11
REDIS8_DIR ?= extern/redis-8.6.3
TARGET_PLATFORMS ?= darwin/arm64 linux/amd64 linux/arm64
HOST_OS := $(shell go env GOHOSTOS)
HOST_ARCH := $(shell go env GOHOSTARCH)
TARGET_OS ?= $(HOST_OS)
TARGET_ARCH ?= $(HOST_ARCH)
TARGET_LABEL := $(TARGET_OS)-$(TARGET_ARCH)
TARGET_VAR := $(subst -,_,$(TARGET_LABEL))
TARGET_DIR := bin/$(TARGET_LABEL)
NO_REDIS_DIR := bin/$(TARGET_LABEL)-no-redis
HOST_CONFIG_BIN := scripts/tmp/default-config-bin
ALLOW_CROSS_FULL_BUILD ?= 0
PROXY_JEMALLOC ?= 1
SUPPORTED_TARGET_OSES := darwin linux
SUPPORTED_TARGET_ARCHES := amd64 arm64

TARGET_CC_VAR := PLATFORM_CC_$(TARGET_VAR)
TARGET_CXX_VAR := PLATFORM_CXX_$(TARGET_VAR)
TARGET_CGO_ENABLED_VAR := PLATFORM_CGO_ENABLED_$(TARGET_VAR)
TARGET_REDIS_MAKE_ARGS_VAR := PLATFORM_REDIS_MAKE_ARGS_$(TARGET_VAR)
TARGET_CC := $($(TARGET_CC_VAR))
TARGET_CXX := $($(TARGET_CXX_VAR))
TARGET_CGO_ENABLED := $(or $($(TARGET_CGO_ENABLED_VAR)),$(CGO_ENABLED),1)
TARGET_REDIS_MAKE_ARGS := $($(TARGET_REDIS_MAKE_ARGS_VAR))
GO_BUILD_ENV = GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) CGO_ENABLED=$(TARGET_CGO_ENABLED) $(if $(TARGET_CC),CC="$(TARGET_CC)")
REDIS_BUILD_ENV = $(if $(TARGET_CC),CC="$(TARGET_CC)") $(if $(TARGET_CXX),CXX="$(TARGET_CXX)") $(TARGET_REDIS_MAKE_ARGS)

.PHONY: build-all host-build-all build-host codis-deps validate-target-platform check-target-platforms check-target-platform default-configs build-platforms release-platforms build-platform build-platform-artifact build-no-redis-platform codis-dashboard codis-proxy codis-admin codis-ha codis-fe codis-server codis-server-redis3 codis-server-redis8 clean-gotest clean distclean gotest gobench docker demo

build-all: host-build-all

host-build-all: codis-server codis-dashboard codis-proxy codis-admin codis-ha codis-fe clean-gotest

build-host: host-build-all

build-platforms: codis-deps
	@$(MAKE) --no-print-directory check-target-platforms
	@set -e; \
	for platform in $(TARGET_PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		$(MAKE) --no-print-directory build-platform-artifact TARGET_OS=$$os TARGET_ARCH=$$arch; \
	done
	@$(MAKE) --no-print-directory clean-gotest

release-platforms: build-platforms

codis-deps:
	@mkdir -p bin config scripts/tmp && bash version

validate-target-platform:
	@if [ -z "$(TARGET_OS)" ] || [ -z "$(TARGET_ARCH)" ]; then \
		echo "TARGET_OS and TARGET_ARCH are required"; \
		exit 2; \
	fi
	@case "$(TARGET_OS)" in \
		darwin|linux) ;; \
		*) echo "unsupported TARGET_OS=$(TARGET_OS); supported: $(SUPPORTED_TARGET_OSES)"; exit 2 ;; \
	esac
	@case "$(TARGET_ARCH)" in \
		amd64|arm64) ;; \
		*) echo "unsupported TARGET_ARCH=$(TARGET_ARCH); supported: $(SUPPORTED_TARGET_ARCHES)"; exit 2 ;; \
	esac
	@case "$(TARGET_LABEL)" in \
		*[!A-Za-z0-9._-]*|*..*) echo "unsafe target platform label: $(TARGET_LABEL)"; exit 2 ;; \
	esac

check-target-platforms:
	@if [ -z "$(strip $(TARGET_PLATFORMS))" ]; then \
		echo "TARGET_PLATFORMS must not be empty"; \
		exit 2; \
	fi
	@set -e; \
	for platform in $(TARGET_PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		$(MAKE) --no-print-directory check-target-platform TARGET_OS=$$os TARGET_ARCH=$$arch; \
	done

check-target-platform: validate-target-platform
	@if [ "$(TARGET_OS)/$(TARGET_ARCH)" != "$(HOST_OS)/$(HOST_ARCH)" ]; then \
		if [ "$(ALLOW_CROSS_FULL_BUILD)" != "1" ]; then \
			echo "full platform build for $(TARGET_OS)/$(TARGET_ARCH) from host $(HOST_OS)/$(HOST_ARCH) requires explicit C/cgo cross toolchain"; \
			echo "set ALLOW_CROSS_FULL_BUILD=1 and $(TARGET_CC_VAR)=<target-cc>, or narrow TARGET_PLATFORMS"; \
			exit 2; \
		fi; \
		if [ -z "$(TARGET_CC)" ]; then \
			echo "missing target C compiler for $(TARGET_OS)/$(TARGET_ARCH): set $(TARGET_CC_VAR)=<target-cc>"; \
			exit 2; \
		fi; \
	fi

default-configs: codis-deps
	@mkdir -p $(HOST_CONFIG_BIN) config
	go build $(VERSION_LDFLAGS) -o $(HOST_CONFIG_BIN)/codis-dashboard ./cmd/dashboard
	@$(HOST_CONFIG_BIN)/codis-dashboard --default-config > config/dashboard.toml
	go build -tags "cgo_jemalloc" $(VERSION_LDFLAGS) -o $(HOST_CONFIG_BIN)/codis-proxy ./cmd/proxy
	@$(HOST_CONFIG_BIN)/codis-proxy --default-config > config/proxy.toml
	@awk '1; /^[[:space:]]*databases[[:space:]]+[0-9]+([[:space:]]*#.*)?$$/ && !injected { print ""; print "# Enable Codis 1024-slot mode for the packaged Codis Server."; print "codis-enabled yes"; print ""; print "# Codis Redis 8 slot migration auth. Empty values keep using requirepass for"; print "# backward-compatible migration auth. Set both fields to use Redis ACL named user."; print "codis-migration-auth-user \"\""; print "codis-migration-auth-pass \"\""; injected=1 }' $(REDIS8_DIR)/redis.conf | sed -e 's/[[:blank:]]*$$//' > config/redis.conf
	@grep -q '^codis-enabled yes$$' config/redis.conf
	@sed -e "s/^sentinel/# sentinel/g" -e 's/[[:blank:]]*$$//' $(REDIS8_DIR)/sentinel.conf > config/sentinel.conf
	@awk '1; /^protected-mode no$$/ { print ""; print "# Codis packaging note: Docker and Kubernetes examples rely on network-layer"; print "# isolation when Sentinel protected mode is disabled. Bare-metal deployments"; print "# should restrict exposure with firewall rules or override this setting." }' config/sentinel.conf > config/sentinel.conf.tmp
	@mv config/sentinel.conf.tmp config/sentinel.conf

build-platform: build-platform-artifact

build-platform-artifact: codis-deps
	@$(MAKE) --no-print-directory check-target-platform TARGET_OS=$(TARGET_OS) TARGET_ARCH=$(TARGET_ARCH)
	@$(MAKE) --no-print-directory default-configs
	@echo "building $(TARGET_LABEL) into $(TARGET_DIR)"
	@rm -rf $(TARGET_DIR)
	@mkdir -p $(TARGET_DIR)
	$(GO_BUILD_ENV) go build $(VERSION_LDFLAGS) -o $(TARGET_DIR)/codis-dashboard ./cmd/dashboard
	$(GO_BUILD_ENV) go build -tags "cgo_jemalloc" $(VERSION_LDFLAGS) -o $(TARGET_DIR)/codis-proxy ./cmd/proxy
	$(GO_BUILD_ENV) go build $(VERSION_LDFLAGS) -o $(TARGET_DIR)/codis-admin ./cmd/admin
	$(GO_BUILD_ENV) go build $(VERSION_LDFLAGS) -o $(TARGET_DIR)/codis-ha ./cmd/ha
	$(GO_BUILD_ENV) go build $(VERSION_LDFLAGS) -o $(TARGET_DIR)/codis-fe ./cmd/fe
	@rm -rf $(TARGET_DIR)/assets; cp -rf cmd/fe/assets $(TARGET_DIR)/
	@$(MAKE) --no-print-directory --quiet -C $(REDIS8_DIR) TARGET_OS= TARGET_ARCH= clean
	$(REDIS_BUILD_ENV) $(MAKE) -j4 -C $(REDIS8_DIR)/ TARGET_OS= TARGET_ARCH=
	@cp -f $(REDIS8_DIR)/src/redis-server $(TARGET_DIR)/codis-server
	@cp -f $(REDIS8_DIR)/src/redis-benchmark $(TARGET_DIR)/
	@cp -f $(REDIS8_DIR)/src/redis-cli $(TARGET_DIR)/
	@cp -f $(REDIS8_DIR)/src/redis-sentinel $(TARGET_DIR)/
	@cp -f config/dashboard.toml config/proxy.toml config/redis.conf config/sentinel.conf $(TARGET_DIR)/
	@test -x $(TARGET_DIR)/codis-dashboard
	@test -x $(TARGET_DIR)/codis-proxy
	@test -x $(TARGET_DIR)/codis-admin
	@test -x $(TARGET_DIR)/codis-ha
	@test -x $(TARGET_DIR)/codis-fe
	@test -x $(TARGET_DIR)/codis-server
	@set -e; \
	case "$(TARGET_OS)/$(TARGET_ARCH)" in \
		darwin/amd64) artifact_pattern='Mach-O .*x86_64' ;; \
		darwin/arm64) artifact_pattern='Mach-O .*arm64' ;; \
		linux/amd64) artifact_pattern='ELF .*x86-64' ;; \
		linux/arm64) artifact_pattern='ELF .*(aarch64|ARM64|ARM aarch64)' ;; \
		*) echo "missing artifact file check for $(TARGET_OS)/$(TARGET_ARCH)"; exit 2 ;; \
	esac; \
	for artifact in $(TARGET_DIR)/codis-dashboard $(TARGET_DIR)/codis-proxy $(TARGET_DIR)/codis-server; do \
		artifact_info=$$(file $$artifact); \
		echo "$$artifact_info"; \
		echo "$$artifact_info" | grep -Eq "$$artifact_pattern" || { \
			echo "$$artifact does not match $(TARGET_OS)/$(TARGET_ARCH)"; \
			exit 2; \
		}; \
	done

build-no-redis-platform: codis-deps
	@$(MAKE) --no-print-directory check-target-platform TARGET_OS=$(TARGET_OS) TARGET_ARCH=$(TARGET_ARCH)
	@$(MAKE) --no-print-directory default-configs
	@echo "building no-redis $(TARGET_LABEL) into $(NO_REDIS_DIR)"
	@rm -rf $(NO_REDIS_DIR)
	@mkdir -p $(NO_REDIS_DIR)
	$(GO_BUILD_ENV) go build $(VERSION_LDFLAGS) -o $(NO_REDIS_DIR)/codis-dashboard ./cmd/dashboard
	$(GO_BUILD_ENV) go build $(VERSION_LDFLAGS) -o $(NO_REDIS_DIR)/codis-admin ./cmd/admin
	$(GO_BUILD_ENV) go build $(VERSION_LDFLAGS) -o $(NO_REDIS_DIR)/codis-ha ./cmd/ha
	$(GO_BUILD_ENV) go build $(VERSION_LDFLAGS) -o $(NO_REDIS_DIR)/codis-fe ./cmd/fe
	@if [ "$(PROXY_JEMALLOC)" = "1" ]; then \
			$(GO_BUILD_ENV) go build -tags "cgo_jemalloc" $(VERSION_LDFLAGS) -o $(NO_REDIS_DIR)/codis-proxy ./cmd/proxy; \
		else \
			$(GO_BUILD_ENV) go build $(VERSION_LDFLAGS) -o $(NO_REDIS_DIR)/codis-proxy ./cmd/proxy; \
		fi
	@rm -rf $(NO_REDIS_DIR)/assets; cp -rf cmd/fe/assets $(NO_REDIS_DIR)/
	@cp -f config/dashboard.toml config/proxy.toml config/redis.conf config/sentinel.conf $(NO_REDIS_DIR)/

codis-dashboard: codis-deps
	go build $(VERSION_LDFLAGS) -o bin/codis-dashboard ./cmd/dashboard
	@./bin/codis-dashboard --default-config > config/dashboard.toml

codis-proxy: codis-deps
	go build -tags "cgo_jemalloc" $(VERSION_LDFLAGS) -o bin/codis-proxy ./cmd/proxy
	@./bin/codis-proxy --default-config > config/proxy.toml

codis-admin: codis-deps
	go build $(VERSION_LDFLAGS) -o bin/codis-admin ./cmd/admin

codis-ha: codis-deps
	go build $(VERSION_LDFLAGS) -o bin/codis-ha ./cmd/ha

codis-fe: codis-deps
	go build $(VERSION_LDFLAGS) -o bin/codis-fe ./cmd/fe
	@rm -rf bin/assets; cp -rf cmd/fe/assets bin/

codis-server:
	@mkdir -p bin config
	@rm -f bin/codis-server
	@which g++ >/dev/null 2>&1 || { echo "ERROR: g++ required to build codis-server (fast_float C++17 dependency)."; echo "Install: dnf install -y gcc-c++ (EL/Rocky) or apt install -y g++"; exit 1; }
	@which autoconf >/dev/null 2>&1 || { echo "ERROR: autoconf required to build codis-server (jemalloc autogen)."; echo "Install: dnf install -y autoconf automake libtool"; exit 1; }
	make -j4 -C $(REDIS8_DIR)/
	@cp -f $(REDIS8_DIR)/src/redis-server  bin/codis-server
	@cp -f $(REDIS8_DIR)/src/redis-benchmark bin/
	@cp -f $(REDIS8_DIR)/src/redis-cli bin/
	@cp -f $(REDIS8_DIR)/src/redis-sentinel bin/
	@awk '1; /^[[:space:]]*databases[[:space:]]+[0-9]+([[:space:]]*#.*)?$$/ && !injected { print ""; print "# Enable Codis 1024-slot mode for the packaged Codis Server."; print "codis-enabled yes"; print ""; print "# Codis Redis 8 slot migration auth. Empty values keep using requirepass for"; print "# backward-compatible migration auth. Set both fields to use Redis ACL named user."; print "codis-migration-auth-user \"\""; print "codis-migration-auth-pass \"\""; injected=1 }' $(REDIS8_DIR)/redis.conf | sed -e 's/[[:blank:]]*$$//' > config/redis.conf
	@grep -q '^codis-enabled yes$$' config/redis.conf
	@sed -e "s/^sentinel/# sentinel/g" -e 's/[[:blank:]]*$$//' $(REDIS8_DIR)/sentinel.conf > config/sentinel.conf
	@awk '1; /^protected-mode no$$/ { print ""; print "# Codis packaging note: Docker and Kubernetes examples rely on network-layer"; print "# isolation when Sentinel protected mode is disabled. Bare-metal deployments"; print "# should restrict exposure with firewall rules or override this setting." }' config/sentinel.conf > config/sentinel.conf.tmp
	@mv config/sentinel.conf.tmp config/sentinel.conf

codis-server-redis3:
	@mkdir -p bin
	@rm -f bin/codis-server-redis3
	make -j4 -C $(REDIS3_DIR)/
	@cp -f $(REDIS3_DIR)/src/redis-server  bin/codis-server-redis3
	@cp -f $(REDIS3_DIR)/src/redis-benchmark bin/redis-benchmark-redis3
	@cp -f $(REDIS3_DIR)/src/redis-cli bin/redis-cli-redis3
	@cp -f $(REDIS3_DIR)/src/redis-sentinel bin/redis-sentinel-redis3

codis-server-redis8: codis-server
	@mkdir -p bin config
	@rm -f bin/codis-server-redis8
	@cp -f bin/codis-server bin/codis-server-redis8
	@cp -f bin/redis-benchmark bin/redis-benchmark-redis8
	@cp -f bin/redis-cli bin/redis-cli-redis8
	@cp -f bin/redis-sentinel bin/redis-sentinel-redis8
	@cp -f config/redis.conf config/redis8.conf
	@cp -f config/sentinel.conf config/sentinel8.conf

clean-gotest:
	@rm -rf ./pkg/topom/gotest.tmp

clean: clean-gotest
	@rm -rf bin
	@rm -rf scripts/tmp

distclean: clean
	@make --no-print-directory --quiet -C $(REDIS3_DIR) distclean
	@if [ -d "$(REDIS8_DIR)" ]; then make --no-print-directory --quiet -C $(REDIS8_DIR) distclean; fi

gotest: codis-deps
	go test $(VERSION_LDFLAGS) ./cmd/... ./pkg/...

gobench: codis-deps
	go test $(VERSION_LDFLAGS) -gcflags -l -bench=. -v ./pkg/...

docker:
	docker build --force-rm -t codis-image .

demo:
	pushd example && make
