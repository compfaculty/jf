# Job Finder - Test Coverage Makefile

.PHONY: test test-unit test-integration test-performance benchmark coverage coverage-html coverage-xml clean test-deps

# Test targets
test: test-unit test-integration test-performance

test-unit:
	@echo "Running unit tests..."
	go test -v -race -timeout=30s ./internal/strutil/... ./internal/utils/...

test-integration:
	@echo "Running integration tests..."
	go test -v -race -timeout=60s ./internal/repo/... ./internal/httpx/...

test-performance:
	@echo "Running performance tests..."
	go test -v -timeout=120s ./internal/utils/... -run="TestPerformance|TestConcurrency|TestMemory"

benchmark:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem -benchtime=5s ./internal/strutil/... ./internal/utils/... ./internal/repo/... ./internal/httpx/...

# Coverage targets
coverage:
	@echo "Generating test coverage..."
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out

coverage-html:
	@echo "Generating HTML coverage report..."
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

coverage-xml:
	@echo "Generating XML coverage report..."
	go test -coverprofile=coverage.out -covermode=atomic ./...
	gocov convert coverage.out | gocov-xml > coverage.xml

# Test with different build tags
test-all:
	@echo "Running all tests with different configurations..."
	go test -v -race -timeout=30s ./...
	go test -v -race -timeout=30s -tags=integration ./...
	go test -v -race -timeout=30s -tags=performance ./...

# Stress tests
stress-test:
	@echo "Running stress tests..."
	go test -v -race -timeout=300s -run="TestConcurrency|TestPerformance" -count=10 ./internal/utils/...

# Memory tests
memtest:
	@echo "Running memory tests..."
	go test -v -timeout=60s -run="TestMemory" ./internal/utils/...

# Race condition tests
race-test:
	@echo "Running race condition tests..."
	go test -race -timeout=60s ./...

# Clean up
clean:
	@echo "Cleaning up test artifacts..."
	rm -f coverage.out coverage.html coverage.xml
	rm -f *.test
	go clean -testcache

# Test dependencies
test-deps:
	@echo "Installing test dependencies..."
	go install github.com/axw/gocov/gocov@latest
	go install github.com/AlekSi/gocov-xml@latest

# CI/CD targets
ci-test:
	@echo "Running CI test suite..."
	go test -v -race -timeout=60s -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out | tail -1

ci-benchmark:
	@echo "Running CI benchmarks..."
	go test -bench=. -benchmem -benchtime=3s ./internal/utils/...

# Development targets
test-watch:
	@echo "Watching for changes and running tests..."
	@while true; do \
		inotifywait -e modify -r . --include='.*\.go$$' 2>/dev/null && \
		echo "Changes detected, running tests..." && \
		go test -v -race -timeout=30s ./internal/strutil/... ./internal/utils/...; \
	done

# Test specific packages
test-strutil:
	go test -v -race ./internal/strutil/...

test-utils:
	go test -v -race ./internal/utils/...

test-repo:
	go test -v -race ./internal/repo/...

test-httpx:
	go test -v -race ./internal/httpx/...

# Performance profiling
profile-cpu:
	@echo "Generating CPU profile..."
	go test -cpuprofile=cpu.prof -bench=. ./internal/utils/...
	go tool pprof cpu.prof

profile-mem:
	@echo "Generating memory profile..."
	go test -memprofile=mem.prof -bench=. ./internal/utils/...
	go tool pprof mem.prof

# Test coverage by package
coverage-strutil:
	go test -coverprofile=strutil.out ./internal/strutil/...
	go tool cover -func=strutil.out

coverage-utils:
	go test -coverprofile=utils.out ./internal/utils/...
	go tool cover -func=utils.out

coverage-repo:
	go test -coverprofile=repo.out ./internal/repo/...
	go tool cover -func=repo.out

# Help target
help:
	@echo "Available targets:"
	@echo "  test              - Run all tests"
	@echo "  test-unit         - Run unit tests only"
	@echo "  test-integration  - Run integration tests only"
	@echo "  test-performance  - Run performance tests only"
	@echo "  benchmark         - Run benchmarks"
	@echo "  coverage          - Generate coverage report"
	@echo "  coverage-html     - Generate HTML coverage report"
	@echo "  coverage-xml      - Generate XML coverage report"
	@echo "  stress-test       - Run stress tests"
	@echo "  memtest           - Run memory tests"
	@echo "  race-test         - Run race condition tests"
	@echo "  clean             - Clean up test artifacts"
	@echo "  test-deps         - Install test dependencies"
	@echo "  ci-test           - Run CI test suite"
	@echo "  ci-benchmark      - Run CI benchmarks"
	@echo "  test-watch        - Watch for changes and run tests"
	@echo "  profile-cpu       - Generate CPU profile"
	@echo "  profile-mem       - Generate memory profile"