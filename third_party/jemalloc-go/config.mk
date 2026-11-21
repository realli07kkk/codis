SRC := jemalloc-5.2.1

# Regenerate the host-specific jemalloc headers on Linux. The checked-in
# jemalloc headers are generated on macOS; cgo cannot compile them on Linux.
config:
	@cd $(SRC) && ./autogen.sh --with-jemalloc-prefix="je_"
	@cd $(SRC) && $(MAKE) include/jemalloc/internal/private_namespace.h
	@rm -rf jemalloc VERSION
	@ln -s $(SRC)/include/jemalloc jemalloc
	@ln -s $(SRC)/VERSION VERSION
	@$(MAKE) --no-print-directory -f help.mk relink

.PHONY: config
