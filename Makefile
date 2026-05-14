.DEFAULT_GOAL := build-all

VERSION_LDFLAGS = -ldflags "$$(cat bin/version.ldflags)"
REDIS3_DIR ?= extern/redis-3.2.11
REDIS8_DIR ?= extern/redis-8.6.3

build-all: codis-server codis-dashboard codis-proxy codis-admin codis-ha codis-fe clean-gotest

codis-deps:
	@mkdir -p bin config && bash version

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
	make -j4 -C $(REDIS8_DIR)/
	@cp -f $(REDIS8_DIR)/src/redis-server  bin/codis-server
	@cp -f $(REDIS8_DIR)/src/redis-benchmark bin/
	@cp -f $(REDIS8_DIR)/src/redis-cli bin/
	@cp -f $(REDIS8_DIR)/src/redis-sentinel bin/
	@awk '1; /^[[:space:]]*databases[[:space:]]+[0-9]+([[:space:]]*#.*)?$$/ && !injected { print ""; print "# Enable Codis 1024-slot mode for the packaged Codis Server."; print "codis-enabled yes"; injected=1 }' $(REDIS8_DIR)/redis.conf | sed -e 's/[[:blank:]]*$$//' > config/redis.conf
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
