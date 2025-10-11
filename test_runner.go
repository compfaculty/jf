package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type TestConfig struct {
	Unit        bool
	Integration bool
	Performance bool
	Benchmark   bool
	Coverage    bool
	Race        bool
	Verbose     bool
	Timeout     time.Duration
	Packages    []string
}

func main() {
	config := parseFlags()

	fmt.Println("🧪 Job Finder Test Runner")
	fmt.Println("==========================")

	if config.Coverage {
		runCoverageTests(config)
	} else {
		runTests(config)
	}
}

func parseFlags() *TestConfig {
	config := &TestConfig{}

	flag.BoolVar(&config.Unit, "unit", false, "Run unit tests")
	flag.BoolVar(&config.Integration, "integration", false, "Run integration tests")
	flag.BoolVar(&config.Performance, "performance", false, "Run performance tests")
	flag.BoolVar(&config.Benchmark, "benchmark", false, "Run benchmarks")
	flag.BoolVar(&config.Coverage, "coverage", false, "Generate coverage report")
	flag.BoolVar(&config.Race, "race", true, "Enable race detection")
	flag.BoolVar(&config.Verbose, "v", false, "Verbose output")
	flag.DurationVar(&config.Timeout, "timeout", 30*time.Second, "Test timeout")

	flag.Parse()

	// If no specific test type is specified, run all
	if !config.Unit && !config.Integration && !config.Performance && !config.Benchmark && !config.Coverage {
		config.Unit = true
		config.Integration = true
		config.Performance = true
		config.Benchmark = true
	}

	// Get packages from remaining arguments
	config.Packages = flag.Args()
	if len(config.Packages) == 0 {
		config.Packages = []string{"./..."}
	}

	return config
}

func runTests(config *TestConfig) {
	var totalTests int
	var passedTests int

	if config.Unit {
		fmt.Println("\n📋 Running Unit Tests...")
		if runTestSuite("Unit", config, "./internal/strutil/...", "./internal/utils/...") {
			passedTests++
		}
		totalTests++
	}

	if config.Integration {
		fmt.Println("\n🔗 Running Integration Tests...")
		if runTestSuite("Integration", config, "./internal/repo/...", "./internal/httpx/...") {
			passedTests++
		}
		totalTests++
	}

	if config.Performance {
		fmt.Println("\n⚡ Running Performance Tests...")
		if runTestSuite("Performance", config, "./internal/utils/...", "-run=TestPerformance|TestConcurrency|TestMemory") {
			passedTests++
		}
		totalTests++
	}

	if config.Benchmark {
		fmt.Println("\n📊 Running Benchmarks...")
		if runBenchmarks(config) {
			passedTests++
		}
		totalTests++
	}

	fmt.Printf("\n🎯 Test Results: %d/%d test suites passed\n", passedTests, totalTests)

	if passedTests == totalTests {
		fmt.Println("✅ All tests passed!")
		os.Exit(0)
	} else {
		fmt.Println("❌ Some tests failed!")
		os.Exit(1)
	}
}

func runTestSuite(name string, config *TestConfig, packages ...string) bool {
	args := []string{"test"}

	if config.Verbose {
		args = append(args, "-v")
	}

	if config.Race {
		args = append(args, "-race")
	}

	args = append(args, "-timeout="+config.Timeout.String())
	args = append(args, packages...)

	fmt.Printf("Running: go %s\n", strings.Join(args, " "))

	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	if err != nil {
		fmt.Printf("❌ %s tests failed after %v\n", name, duration)
		return false
	}

	fmt.Printf("✅ %s tests passed in %v\n", name, duration)
	return true
}

func runBenchmarks(config *TestConfig) bool {
	args := []string{"test", "-bench=.", "-benchmem", "-benchtime=3s"}

	if config.Verbose {
		args = append(args, "-v")
	}

	args = append(args, "./internal/strutil/...", "./internal/utils/...", "./internal/repo/...", "./internal/httpx/...")

	fmt.Printf("Running: go %s\n", strings.Join(args, " "))

	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	if err != nil {
		fmt.Printf("❌ Benchmarks failed after %v\n", duration)
		return false
	}

	fmt.Printf("✅ Benchmarks completed in %v\n", duration)
	return true
}

func runCoverageTests(config *TestConfig) {
	fmt.Println("\n📊 Generating Coverage Report...")

	// Generate coverage profile
	args := []string{"test", "-coverprofile=coverage.out", "-covermode=atomic"}

	if config.Verbose {
		args = append(args, "-v")
	}

	if config.Race {
		args = append(args, "-race")
	}

	args = append(args, "./...")

	fmt.Printf("Running: go %s\n", strings.Join(args, " "))

	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		log.Fatalf("Failed to generate coverage: %v", err)
	}

	// Generate coverage report
	fmt.Println("\n📈 Coverage Summary:")
	cmd = exec.Command("go", "tool", "cover", "-func=coverage.out")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		log.Fatalf("Failed to generate coverage summary: %v", err)
	}

	// Generate HTML report
	fmt.Println("\n🌐 Generating HTML coverage report...")
	cmd = exec.Command("go", "tool", "cover", "-html=coverage.out", "-o", "coverage.html")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		log.Fatalf("Failed to generate HTML coverage report: %v", err)
	}

	fmt.Println("✅ Coverage report generated: coverage.html")

	// Clean up
	os.Remove("coverage.out")
}

func findTestFiles() []string {
	var testFiles []string

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.HasSuffix(path, "_test.go") {
			testFiles = append(testFiles, path)
		}

		return nil
	})

	if err != nil {
		log.Fatalf("Failed to find test files: %v", err)
	}

	return testFiles
}

func printTestSummary() {
	testFiles := findTestFiles()

	fmt.Printf("\n📁 Found %d test files:\n", len(testFiles))
	for _, file := range testFiles {
		fmt.Printf("  - %s\n", file)
	}
}
