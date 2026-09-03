.PHONY: help run start stop restart status logs build test fmt clean

BIN  := taal
PORT ?= 8225
# per port, so running two on different ports does not confuse them and a
# start on a free port is never reported as already running
PID  := .taal-$(PORT).pid
LOG  := taal-$(PORT).log

help:
	@echo "taal"
	@echo ""
	@echo "  make run        run in this terminal, ctrl-c to stop"
	@echo "  make start      start in the background"
	@echo "  make stop       stop the background server"
	@echo "  make restart    rebuild and restart"
	@echo "  make status     is it running, and where"
	@echo "  make logs       follow the log"
	@echo ""
	@echo "  make test       run the tests"
	@echo "  make build      build the binary"
	@echo "  make clean      remove the binary, log and pid file"
	@echo ""
	@echo "  PORT=9000 make start   to use another port"

run: build
	./$(BIN) -port $(PORT)

build:
	go build -o $(BIN) .

# the pid file is what makes stop and status possible. a stale one from a
# crash is detected rather than trusted.
start: build
	@if [ -f $(PID) ] && kill -0 $$(cat $(PID)) 2>/dev/null; then \
		echo "already running (pid $$(cat $(PID)))"; \
		$(MAKE) --no-print-directory status; \
	else \
		rm -f $(PID); \
		./$(BIN) -port $(PORT) > $(LOG) 2>&1 & echo $$! > $(PID); \
		sleep 2; \
		if kill -0 $$(cat $(PID)) 2>/dev/null; then \
			$(MAKE) --no-print-directory status; \
		else \
			echo "it did not start:"; rm -f $(PID); tail -20 $(LOG); exit 1; \
		fi; \
	fi

# SIGTERM rather than kill, so it puts the audio output back on the way out
stop:
	@if [ -f $(PID) ] && kill -0 $$(cat $(PID)) 2>/dev/null; then \
		kill -TERM $$(cat $(PID)); \
		sleep 1; \
		rm -f $(PID); \
		echo "stopped"; \
	else \
		rm -f $(PID); \
		echo "not running"; \
	fi

restart:
	@$(MAKE) --no-print-directory stop
	@$(MAKE) --no-print-directory start

status:
	@if [ -f $(PID) ] && kill -0 $$(cat $(PID)) 2>/dev/null; then \
		echo "running   pid $$(cat $(PID))"; \
		echo "host      https://$$($(MAKE) --no-print-directory ip):$(PORT)/host"; \
		echo "speakers  https://$$($(MAKE) --no-print-directory ip):$(PORT)/"; \
	else \
		echo "not running   (make start)"; \
	fi

logs:
	@touch $(LOG); tail -f $(LOG)

test:
	go test -race ./...

fmt:
	gofmt -w .

ip:
	@ipconfig getifaddr en0 2>/dev/null \
		|| ipconfig getifaddr en1 2>/dev/null \
		|| hostname -I 2>/dev/null | awk '{print $$1}' \
		|| echo localhost

clean: stop
	rm -f $(BIN) $(LOG) $(PID)
