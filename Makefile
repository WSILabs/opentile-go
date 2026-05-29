.PHONY: test cover parity vet bench bench-ndpi bench-ndpi-mt bench-svs bench-svs-mt

test:
	go test ./... -race -count=1

cover:
	OPENTILE_TESTDIR=$(PWD)/sample_files scripts/cover.sh

parity:
	OPENTILE_ORACLE_PYTHON=$${OPENTILE_ORACLE_PYTHON:-/private/tmp/opentile-py/bin/python} \
	OPENTILE_TESTDIR=$(PWD)/sample_files \
	  go test ./tests/oracle/... -tags parity -v -timeout 30m

vet:
	go vet ./...

bench:
	NDPI_BENCH_SLIDE=$(PWD)/sample_files/ndpi/CMU-1.ndpi \
	  go test ./formats/ndpi -bench=Tile -benchtime=3x -run=^$$ -v

# NDPI single-thread tile-decode benchmark. Built atop the test subject
# at cmd/bench/ndpi/. Fails if throughput drops below MIN_NDPI_MPIXS
# Mpix/s on CMU-1.ndpi — the v0.27 hard performance gate.
#
# Requires OPENTILE_TESTDIR pointing at sample_files/ (with the NDPI
# fixture present). Defaults to $PWD/sample_files.
OPENTILE_TESTDIR ?= $(PWD)/sample_files
MIN_NDPI_MPIXS ?= 220

bench-ndpi:
	@if [ ! -f "$(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi" ]; then \
		echo "fixture missing: $(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi"; \
		exit 1; \
	fi
	@go build -o /tmp/bench-opentile-ndpi ./cmd/bench/ndpi/
	@result=$$(/tmp/bench-opentile-ndpi -in "$(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi"); \
	echo "$$result"; \
	mpps=$$(echo "$$result" | tail -1 | sed -E 's/.* \(([0-9.]+) Mpix\/s.*/\1/'); \
	awk -v got="$$mpps" -v min="$(MIN_NDPI_MPIXS)" 'BEGIN { \
		if (got+0 < min+0) { \
			printf "FAIL: %.1f Mpix/s < %.1f Mpix/s threshold\n", got, min; \
			exit 1; \
		} else { \
			printf "PASS: %.1f Mpix/s >= %.1f Mpix/s threshold\n", got, min; \
		} \
	}'

# Multi-thread NDPI bench. Measurement only — no gate. Pre-v0.28
# capped at ~single-thread (mutex bottleneck); post-v0.28 scales via
# the decoder-handle pool. NDPI's specific number is muted because
# ReadRegion's per-call allocations dominate; SVS shows the cleaner
# pool win on the slow path.
bench-ndpi-mt:
	@if [ ! -f "$(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi" ]; then \
		echo "fixture missing: $(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi"; \
		exit 1; \
	fi
	@go build -o /tmp/bench-opentile-ndpi ./cmd/bench/ndpi/
	@/tmp/bench-opentile-ndpi -in "$(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi" -goroutines $$(sysctl -n hw.ncpu 2>/dev/null || nproc)

# SVS single-thread tile-decode bench. v0.28 hard gate for the
# cross-format pool's measured deliverable. MIN_SVS_MPIXS gate value
# is set after baseline measurement (Task 4.4).
MIN_SVS_MPIXS ?= 475

bench-svs:
	@if [ ! -f "$(OPENTILE_TESTDIR)/svs/CMU-1.svs" ]; then \
		echo "fixture missing: $(OPENTILE_TESTDIR)/svs/CMU-1.svs"; \
		exit 1; \
	fi
	@go build -o /tmp/bench-opentile-svs ./cmd/bench/svs/
	@result=$$(/tmp/bench-opentile-svs -in "$(OPENTILE_TESTDIR)/svs/CMU-1.svs"); \
	echo "$$result"; \
	if [ "$(MIN_SVS_MPIXS)" = "0" ]; then \
		echo "(no gate yet — set MIN_SVS_MPIXS once baseline is measured)"; \
		exit 0; \
	fi; \
	mpps=$$(echo "$$result" | tail -1 | sed -E 's/.* \(([0-9.]+) Mpix\/s.*/\1/'); \
	awk -v got="$$mpps" -v min="$(MIN_SVS_MPIXS)" 'BEGIN { \
		if (got+0 < min+0) { \
			printf "FAIL: %.1f Mpix/s < %.1f Mpix/s threshold\n", got, min; \
			exit 1; \
		} else { \
			printf "PASS: %.1f Mpix/s >= %.1f Mpix/s threshold\n", got, min; \
		} \
	}'

# Multi-thread SVS bench. Measurement only — no gate.
bench-svs-mt:
	@if [ ! -f "$(OPENTILE_TESTDIR)/svs/CMU-1.svs" ]; then \
		echo "fixture missing: $(OPENTILE_TESTDIR)/svs/CMU-1.svs"; \
		exit 1; \
	fi
	@go build -o /tmp/bench-opentile-svs ./cmd/bench/svs/
	@/tmp/bench-opentile-svs -in "$(OPENTILE_TESTDIR)/svs/CMU-1.svs" -goroutines $$(sysctl -n hw.ncpu 2>/dev/null || nproc)
