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
	@mkdir -p bin
	@rm -f bin/codis-server
	make -j4 -C $(REDIS3_DIR)/
	@cp -f $(REDIS3_DIR)/src/redis-server  bin/codis-server
	@cp -f $(REDIS3_DIR)/src/redis-benchmark bin/
	@cp -f $(REDIS3_DIR)/src/redis-cli bin/
	@cp -f $(REDIS3_DIR)/src/redis-sentinel bin/
	@cp -f $(REDIS3_DIR)/redis.conf config/
	@sed -e "s/^sentinel/# sentinel/g" $(REDIS3_DIR)/sentinel.conf > config/sentinel.conf

codis-server-redis8:
	@mkdir -p bin config
	@rm -f bin/codis-server-redis8
	make -j4 -C $(REDIS8_DIR)/
	@cp -f $(REDIS8_DIR)/src/redis-server bin/codis-server-redis8
	@cp -f $(REDIS8_DIR)/src/redis-benchmark bin/redis-benchmark-redis8
	@cp -f $(REDIS8_DIR)/src/redis-cli bin/redis-cli-redis8
	@cp -f $(REDIS8_DIR)/src/redis-sentinel bin/redis-sentinel-redis8
	@cp -f $(REDIS8_DIR)/redis.conf config/redis8.conf
	@sed -e "s/^sentinel/# sentinel/g" $(REDIS8_DIR)/sentinel.conf > config/sentinel8.conf

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
