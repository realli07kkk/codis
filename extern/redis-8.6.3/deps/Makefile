# Redis dependencies Makefile
# Builds all bundled dependencies for redis-server.

CC ?= gcc
CXX ?= g++
AR ?= ar
CFLAGS ?= -O3 -fPIC
CXXFLAGS ?= -O3 -fPIC

.PHONY: all hiredis linenoise lua hdr_histogram fpconv fast_float xxhash jemalloc

all: hiredis linenoise lua hdr_histogram fpconv fast_float xxhash jemalloc

hiredis:
	cd hiredis && $(CC) $(CFLAGS) -std=c99 -c alloc.c async.c dict.c hiredis.c net.c read.c sds.c sockcompat.c
	cd hiredis && $(AR) rcs libhiredis.a alloc.o async.o dict.o hiredis.o net.o read.o sds.o sockcompat.o

linenoise:
	cd linenoise && $(CC) $(CFLAGS) -std=c99 -c linenoise.c -o linenoise.o

lua:
	cd lua/src && $(CC) $(CFLAGS) -std=c99 -c *.c
	cd lua/src && $(AR) rcs liblua.a *.o

hdr_histogram:
	cd hdr_histogram && $(CC) $(CFLAGS) -std=c99 -DHDR_MALLOC_INCLUDE=\"hdr_redis_malloc.h\" -c hdr_histogram.c -o hdr_histogram.o
	cd hdr_histogram && $(AR) rcs libhdrhistogram.a hdr_histogram.o

fpconv:
	cd fpconv && $(CC) $(CFLAGS) -std=c99 -c fpconv_dtoa.c -o fpconv_dtoa.o
	cd fpconv && $(AR) rcs libfpconv.a fpconv_dtoa.o

fast_float:
	cd fast_float && $(CXX) $(CXXFLAGS) -std=c++17 -c fast_float_strtod.cpp -o fast_float_strtod.o
	cd fast_float && $(AR) rcs libfast_float.a fast_float_strtod.o

xxhash:
	cd xxhash && $(CC) $(CFLAGS) -std=c99 -c xxhash.c -o xxhash.o
	cd xxhash && $(AR) rcs libxxhash.a xxhash.o

jemalloc:
	@# Auto-build jemalloc if not already built. Requires autoconf/automake/libtool.
	@if [ ! -f jemalloc/lib/libjemalloc.a ]; then \
		echo "Building jemalloc (one-time)..."; \
		cd jemalloc && \
		[ -f configure ] || ./autogen.sh --with-version=5.3.0-0-g0 && \
		[ -f Makefile ] || ./configure --with-jemalloc-prefix=je_ --enable-static --disable-shared && \
		$(MAKE) -j4; \
	fi

distclean:
	@# no-op: individual dep distcleans are handled by jemalloc separately

clean:
	@# no-op: object files are cleaned by src/Makefile clean
