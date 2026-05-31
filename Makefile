.PHONY: test cover parity vet bench bench-ndpi bench-ndpi-mt bench-svs bench-svs-mt bench-ndpi-mem

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
MIN_NDPI_MPIXS ?= 270

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

# Peak-RSS gate for the NDPI ScaledStrips (DZI) path. Runs the
# no-backpressure worst case under GOMEMLIMIT=2GiB (the recommended
# deployment config). Thresholds are intentionally HIGHER than real
# wsitools RSS because this harness drops strips (no consumer
# backpressure) — it bounds the library's worst case, not the app's.
# Thresholds set from post-fix measurement (v0.30) with ~15-20% headroom.
# Measured peaks under GOMEMLIMIT=2GiB: CMU-1@256 ~1948 MiB; OS-2@256
# ~2037 MiB; OS-2@1024 ~2751 MiB (the @1024 case is higher due to the
# irreducible full-width output strip buffer, not the tile cache).
# MAXPEAK_OS2 covers both OS-2 runs, so it tracks the @1024 peak.
MAXPEAK_CMU ?= 2300
MAXPEAK_OS2 ?= 3300

bench-ndpi-mem: ## NDPI ScaledStrips peak-RSS gate (DZI path)
	@go build -o /tmp/ndpi-strips ./cmd/bench/ndpi-strips/
	GOMEMLIMIT=2GiB /tmp/ndpi-strips -in "$(OPENTILE_TESTDIR)/ndpi/CMU-1.ndpi" -dzitile 256  -maxpeak $(MAXPEAK_CMU)
	GOMEMLIMIT=2GiB /tmp/ndpi-strips -in "$(OPENTILE_TESTDIR)/ndpi/OS-2.ndpi"  -dzitile 256  -maxpeak $(MAXPEAK_OS2)
	GOMEMLIMIT=2GiB /tmp/ndpi-strips -in "$(OPENTILE_TESTDIR)/ndpi/OS-2.ndpi"  -dzitile 1024 -maxpeak $(MAXPEAK_OS2)

.PHONY: bench-all bench-compare

# Cross-format throughput regression gate. Local/manual (like the other
# bench-* targets — NOT run in CI; GitHub runners are too slow/variable
# for absolute Mpix/s floors). Runs the pure-Go internal benchmarks and
# fails if a gated format/pattern drops below its floor. Floors are
# ~85% of measured single-thread baselines on the developer machine;
# only stable decode/assembly patterns are gated (svs/tile is pure
# compressed-fetch overhead and too noisy to gate). Re-baseline floors
# when a deliberate speedup raises the bar.
bench-all: ## Cross-format throughput regression gate (local)
	@OPENTILE_TESTDIR="$(OPENTILE_TESTDIR)" go test ./bench/ \
		-bench 'BenchmarkRead/(ndpi|svs)/.*/single' -run '^$$' -benchtime 100x 2>/dev/null \
		| tee /tmp/bench-all.txt
	@awk ' \
	  BEGIN { \
	    floor["ndpi/tile/single"]=290; floor["ndpi/decodedtile/single"]=520; floor["ndpi/readregion/single"]=540; \
	    floor["svs/decodedtile/single"]=380; floor["svs/readregion/single"]=440; \
	    fail=0 \
	  } \
	  /Mpix\/s/ { \
	    name=$$1; sub(/-[0-9]+$$/,"",name); sub(/^BenchmarkRead\//,"",name); \
	    for (i=1;i<=NF;i++) if ($$i=="Mpix/s") v=$$(i-1); \
	    if (name in floor) { \
	      if (v+0 < floor[name]+0) { printf "FAIL: %s = %.1f Mpix/s < floor %d\n", name, v, floor[name]; fail=1 } \
	      else { printf "ok:   %s = %.1f Mpix/s >= %d\n", name, v, floor[name] } \
	    } \
	  } \
	  END { if (fail) exit 1; else print "bench-all: all gated lines >= floor" } \
	' /tmp/bench-all.txt

# Cross-language competitive report (on-demand; needs libopenslide +
# python opentile via OPENTILE_ORACLE_PYTHON). Not run in CI.
bench-compare: ## Competitive report: opentile-go vs openslide vs python opentile
	@go build -tags openslidebench -o /tmp/bench-compare ./cmd/bench/compare/
	@OPENTILE_TESTDIR="$(OPENTILE_TESTDIR)" /tmp/bench-compare
