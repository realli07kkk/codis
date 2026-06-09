# Redis dependencies Makefile
# Builds all bundled dependencies for redis-server.
#
# Note: the bundled deps do not ship their own Makefiles, so this top-level
# Makefile encodes the minimal compile / archive commands for each one.  When
# the upstream Redis deps Makefiles are committed here, these targets should
# delegate to them instead.

CC ?= gcc
CXX ?= g++
AR ?= ar
CFLAGS ?= -O3 -fPIC
CXXFLAGS ?= -O3 -fPIC

.PHONY: all hiredis linenoise lua hdr_histogram fpconv fast_float xxhash jemalloc clean distclean

all: hiredis linenoise lua hdr_histogram fpconv fast_float xxhash jemalloc

# -- hiredis ------------------------------------------------------------------
hiredis:
	cd hiredis && $(CC) $(CFLAGS) -std=c99 -c alloc.c async.c dict.c hiredis.c net.c read.c sds.c sockcompat.c
	cd hiredis && $(AR) rcs libhiredis.a alloc.o async.o dict.o hiredis.o net.o read.o sds.o sockcompat.o
# Optional: build libhiredis_ssl.a when OpenSSL headers are available (BUILD_TLS).
# The '-' prefix makes this best-effort; missing openssl-devel is not a hard error.
	-cd hiredis && [ -f /usr/include/openssl/ssl.h ] && \
		$(CC) $(CFLAGS) -std=c99 -c ssl.c -o ssl.o && \
		$(AR) rcs libhiredis_ssl.a ssl.o; \
		true

# -- linenoise ----------------------------------------------------------------
linenoise:
	cd linenoise && $(CC) $(CFLAGS) -std=c99 -c linenoise.c -o linenoise.o

# -- lua ----------------------------------------------------------------------
lua:
	cd lua/src && $(CC) $(CFLAGS) -std=c99 -c *.c
	cd lua/src && $(AR) rcs liblua.a *.o

# -- hdr_histogram ------------------------------------------------------------
hdr_histogram:
	cd hdr_histogram && $(CC) $(CFLAGS) -std=c99 -DHDR_MALLOC_INCLUDE=\"hdr_redis_malloc.h\" -c hdr_histogram.c -o hdr_histogram.o
	cd hdr_histogram && $(AR) rcs libhdrhistogram.a hdr_histogram.o

# -- fpconv -------------------------------------------------------------------
fpconv:
	cd fpconv && $(CC) $(CFLAGS) -std=c99 -c fpconv_dtoa.c -o fpconv_dtoa.o
	cd fpconv && $(AR) rcs libfpconv.a fpconv_dtoa.o

# -- fast_float ---------------------------------------------------------------
fast_float:
	cd fast_float && $(CXX) $(CXXFLAGS) -std=c++17 -DFASTFLOAT_ALLOWS_LEADING_PLUS -c fast_float_strtod.cpp -o fast_float_strtod.o
	cd fast_float && $(AR) rcs libfast_float.a fast_float_strtod.o

# -- xxhash -------------------------------------------------------------------
xxhash:
	cd xxhash && $(CC) $(CFLAGS) -std=c99 -c xxhash.c -o xxhash.o
	cd xxhash && $(AR) rcs libxxhash.a xxhash.o

# -- jemalloc -----------------------------------------------------------------
jemalloc:
	@# Auto-build jemalloc if not already built.  autogen.sh may invoke
	@# ./configure internally, but we always run our own configure afterwards
	@# to guarantee --with-jemalloc-prefix=je_, --with-lg-quantum=3, and
	@# static-only build.  Requires autoconf/automake/libtool.
	@if [ ! -f jemalloc/lib/libjemalloc.a ]; then \
		echo "Building jemalloc (one-time)..."; \
		cd jemalloc && \
		[ -f configure ] || ./autogen.sh --with-version=5.3.0-0-g0 && \
		./configure --with-jemalloc-prefix=je_ --with-lg-quantum=3 --enable-static --disable-shared && \
		$(MAKE) -j4; \
	fi

# -- clean / distclean --------------------------------------------------------
clean:
	@echo "Cleaning deps build artifacts..."
	@rm -f hiredis/*.o hiredis/libhiredis.a hiredis/libhiredis_ssl.a
	@rm -f linenoise/*.o
	@rm -f lua/src/*.o lua/src/liblua.a
	@rm -f hdr_histogram/*.o hdr_histogram/libhdrhistogram.a
	@rm -f fpconv/*.o fpconv/libfpconv.a
	@rm -f fast_float/*.o fast_float/libfast_float.a
	@rm -f xxhash/*.o xxhash/libxxhash.a

distclean: clean
	@if [ -f jemalloc/Makefile ]; then \
		echo "Dist-cleaning jemalloc..."; \
		$(MAKE) -C jemalloc distclean; \
	fi
