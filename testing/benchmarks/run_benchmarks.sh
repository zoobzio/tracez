#!/bin/bash
# Tracez Benchmark Runner
# Provides convenient commands for running different benchmark categories

set -e

BENCH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$BENCH_DIR"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default benchmark time
BENCHTIME=${BENCHTIME:-3s}

print_usage() {
    cat << EOF
Usage: $0 [COMMAND] [OPTIONS]

COMMANDS:
    all         Run all benchmarks
    memory      Run memory benchmarks
    cpu         Run CPU benchmarks  
    concurrency Run concurrency benchmarks
    scenarios   Run scenario benchmarks
    comparison  Run comparison benchmarks
    quick       Run quick performance check
    profile     Run with profiling enabled

OPTIONS:
    -t TIME     Set benchmark time (default: ${BENCHTIME})
    -v          Verbose output
    -h          Show this help

EXAMPLES:
    $0 quick                    # Quick performance check
    $0 memory -t 5s            # Memory benchmarks for 5 seconds
    $0 cpu -v                  # CPU benchmarks with verbose output
    $0 profile                 # Run with CPU and memory profiling
    $0 comparison              # Compare against OpenTelemetry

ENVIRONMENT:
    BENCHTIME   Default benchmark duration (default: 3s)
EOF
}

print_header() {
    echo -e "${BLUE}================================================${NC}"
    echo -e "${BLUE} $1${NC}"
    echo -e "${BLUE}================================================${NC}"
    echo
}

run_benchmark() {
    local pattern="$1"
    local description="$2"
    
    print_header "$description"
    
    if [ "$VERBOSE" = "1" ]; then
        go test -bench="$pattern" -benchmem -benchtime="$BENCHTIME" -v .
    else
        go test -bench="$pattern" -benchmem -benchtime="$BENCHTIME" .
    fi
    
    echo
}

run_with_profile() {
    local pattern="$1"
    local name="$2"
    
    print_header "Profiling: $name"
    
    # CPU profile
    echo -e "${YELLOW}Generating CPU profile...${NC}"
    go test -bench="$pattern" -benchtime="$BENCHTIME" -cpuprofile="cpu_${name}.prof" .
    
    # Memory profile  
    echo -e "${YELLOW}Generating memory profile...${NC}"
    go test -bench="$pattern" -benchtime="$BENCHTIME" -memprofile="mem_${name}.prof" .
    
    echo -e "${GREEN}Profiles generated:${NC}"
    echo "  CPU: cpu_${name}.prof"
    echo "  Memory: mem_${name}.prof"
    echo
    echo "View with:"
    echo "  go tool pprof cpu_${name}.prof"
    echo "  go tool pprof mem_${name}.prof"
    echo
}

run_comparison() {
    print_header "Comparison Benchmarks vs OpenTelemetry"
    
    cd comparison
    
    # Ensure dependencies are available
    if ! go mod tidy; then
        echo -e "${RED}Failed to download comparison dependencies${NC}"
        echo "Run 'go mod tidy' in testing/benchmarks/comparison/"
        exit 1
    fi
    
    echo -e "${YELLOW}Running tracez vs OpenTelemetry benchmarks...${NC}"
    
    if [ "$VERBOSE" = "1" ]; then
        go test -bench=BenchmarkTracezVsOtel -benchmem -benchtime="$BENCHTIME" -v .
    else
        go test -bench=BenchmarkTracezVsOtel -benchmem -benchtime="$BENCHTIME" .
    fi
    
    cd ..
    echo
}

# Parse arguments
VERBOSE=0
COMMAND=""

while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--benchtime)
            BENCHTIME="$2"
            shift 2
            ;;
        -v|--verbose)
            VERBOSE=1
            shift
            ;;
        -h|--help)
            print_usage
            exit 0
            ;;
        all|memory|cpu|concurrency|scenarios|comparison|quick|profile)
            COMMAND="$1"
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            print_usage
            exit 1
            ;;
    esac
done

if [ -z "$COMMAND" ]; then
    print_usage
    exit 1
fi

# Verify we're in the right directory
if [ ! -f "memory_bench_test.go" ]; then
    echo -e "${RED}Error: Must run from testing/benchmarks directory${NC}"
    exit 1
fi

echo -e "${GREEN}Tracez Benchmark Runner${NC}"
echo "Benchmark time: $BENCHTIME"
echo "Verbose: $([ "$VERBOSE" = "1" ] && echo "enabled" || echo "disabled")"
echo

case $COMMAND in
    all)
        run_benchmark "BenchmarkTracer" "Memory Benchmarks"
        run_benchmark "BenchmarkSpan" "CPU Benchmarks"  
        run_benchmark "BenchmarkConcurrent" "Concurrency Benchmarks"
        run_benchmark "BenchmarkWebServer|BenchmarkMicroservice|BenchmarkDatabase|BenchmarkWorkerPool|BenchmarkStreaming" "Scenario Benchmarks"
        run_comparison
        ;;
    memory)
        run_benchmark "BenchmarkTracer" "Memory Benchmarks"
        ;;
    cpu)
        run_benchmark "BenchmarkSpan" "CPU Benchmarks"
        ;;
    concurrency)
        run_benchmark "BenchmarkConcurrent" "Concurrency Benchmarks"
        ;;
    scenarios)
        run_benchmark "BenchmarkWebServer|BenchmarkMicroservice|BenchmarkDatabase|BenchmarkWorkerPool|BenchmarkStreaming" "Scenario Benchmarks"
        ;;
    comparison)
        run_comparison
        ;;
    quick)
        print_header "Quick Performance Check"
        echo -e "${YELLOW}Running core performance benchmarks...${NC}"
        go test -bench="BenchmarkTracerSpanCreation|BenchmarkSpanCreationRate|BenchmarkFullPipeline" -benchmem -benchtime=1s .
        echo
        ;;
    profile)
        echo "Select benchmark to profile:"
        echo "1) Span Creation"
        echo "2) Full Pipeline" 
        echo "3) Web Server Scenario"
        echo "4) Custom pattern"
        read -p "Choice (1-4): " choice
        
        case $choice in
            1)
                run_with_profile "BenchmarkTracerSpanCreation" "span_creation"
                ;;
            2)
                run_with_profile "BenchmarkFullPipeline" "full_pipeline"
                ;;
            3)
                run_with_profile "BenchmarkWebServerScenario" "web_server"
                ;;
            4)
                read -p "Enter benchmark pattern: " pattern
                read -p "Enter profile name: " name
                run_with_profile "$pattern" "$name"
                ;;
            *)
                echo -e "${RED}Invalid choice${NC}"
                exit 1
                ;;
        esac
        ;;
esac

echo -e "${GREEN}Benchmark run completed!${NC}"