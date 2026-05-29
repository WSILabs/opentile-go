.PHONY: test cover parity vet bench bench-ndpi

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
MIN_NDPI_MPIXS ?= 130

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
