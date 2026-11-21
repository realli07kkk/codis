.DEFAULT_GOAL = relink

PWD := $(shell pwd)
SRC := jemalloc-5.2.1

-include $(SRC)/Makefile

relink: unlink
	@for i in $(C_SRCS); do \
		name=$$(basename $$i); \
		if [ "$$name" = "zone.c" ]; then \
			target=je_zone_darwin.c; \
		else \
			target=je_$$name; \
		fi; \
		rm -f "$$target"; \
		ln -s $(SRC)/$$i "$$target"; \
	done

unlink:
	@rm -f je_*.c
