#!/bin/bash

# Reliability Test Runner for tracez
# Implements CI-Safe, Stress-Capable pattern following hookz reliability framework

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default configuration
RELIABILITY_LEVEL="${TRACEZ_RELIABILITY_LEVEL:-}"
DURATION="${TRACEZ_RELIABILITY_DURATION:-30s}"
MAX_GOROUTINES="${TRACEZ_RELIABILITY_MAX_GOROUTINES:-100}"
MAX_MEMORY_MB="${TRACEZ_RELIABILITY_MAX_MEMORY_MB:-512}"
FAILURE_THRESHOLD="${TRACEZ_RELIABILITY_FAILURE_THRESHOLD:-0.05}"
VERBOSE="${TRACEZ_RELIABILITY_VERBOSE:-false}"
OUTPUT_DIR="${TRACEZ_RELIABILITY_OUTPUT_DIR:-./reliability-results}"

# Usage information
usage() {
    echo "Usage: $0 [basic|stress] [options]"
    echo ""
    echo "Arguments:"
    echo "  basic    Run CI-safe reliability tests (default in CI)"
    echo "  stress   Run comprehensive stress tests (manual/scheduled)"
    echo ""
    echo "Options:"
    echo "  -d, --duration DURATION     Test duration for stress tests (default: 30s)"
    echo "  -g, --goroutines MAX        Maximum goroutines (default: 100)"
    echo "  -m, --memory MB             Memory limit in MB (default: 512)"
    echo "  -t, --threshold RATE        Failure rate threshold 0.0-1.0 (default: 0.05)"
    echo "  -v, --verbose               Verbose output"
    echo "  -o, --output DIR            Output directory (default: ./reliability-results)"
    echo "  -h, --help                  Show this help"
    echo ""
    echo "Environment Variables:"
    echo "  TRACEZ_RELIABILITY_LEVEL           Test level (basic|stress)"
    echo "  TRACEZ_RELIABILITY_DURATION        Test duration"
    echo "  TRACEZ_RELIABILITY_MAX_GOROUTINES  Max goroutines"
    echo "  TRACEZ_RELIABILITY_MAX_MEMORY_MB   Memory limit"
    echo "  TRACEZ_RELIABILITY_FAILURE_THRESHOLD Failure threshold"
    echo "  TRACEZ_RELIABILITY_VERBOSE         Verbose output"
    echo "  TRACEZ_RELIABILITY_OUTPUT_DIR      Output directory"
    echo ""
    echo "Examples:"
    echo "  $0 basic                    # Quick CI validation"
    echo "  $0 stress -d 5m -g 500      # 5-minute stress test"
    echo "  $0 stress -v -o ./results   # Verbose stress test"
}

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_verbose() {
    if [ "$VERBOSE" = "true" ]; then
        echo -e "${BLUE}[VERBOSE]${NC} $1"
    fi
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            basic|stress)
                RELIABILITY_LEVEL="$1"
                shift
                ;;
            -d|--duration)
                DURATION="$2"
                shift 2
                ;;
            -g|--goroutines)
                MAX_GOROUTINES="$2"
                shift 2
                ;;
            -m|--memory)
                MAX_MEMORY_MB="$2"
                shift 2
                ;;
            -t|--threshold)
                FAILURE_THRESHOLD="$2"
                shift 2
                ;;
            -v|--verbose)
                VERBOSE="true"
                shift
                ;;
            -o|--output)
                OUTPUT_DIR="$2"
                shift 2
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                log_error "Unknown argument: $1"
                usage
                exit 1
                ;;
        esac
    done
}

# Detect environment and set defaults
detect_environment() {
    # CI environment detection
    if [ "${CI:-false}" = "true" ] || [ "${GITHUB_ACTIONS:-false}" = "true" ] || [ "${GITLAB_CI:-false}" = "true" ]; then
        log_info "CI environment detected"
        if [ -z "$RELIABILITY_LEVEL" ]; then
            RELIABILITY_LEVEL="basic"
            log_info "Setting reliability level to 'basic' for CI"
        fi
        
        # Reduce resource usage in CI
        if [ "$MAX_GOROUTINES" -gt 50 ]; then
            MAX_GOROUTINES=50
            log_verbose "Reducing max goroutines to $MAX_GOROUTINES for CI"
        fi
        
        if [ "$MAX_MEMORY_MB" -gt 256 ]; then
            MAX_MEMORY_MB=256
            log_verbose "Reducing memory limit to ${MAX_MEMORY_MB}MB for CI"
        fi
    fi
    
    # Default to basic if not specified
    if [ -z "$RELIABILITY_LEVEL" ]; then
        log_warning "No reliability level specified, skipping tests"
        log_info "Use '$0 basic' for CI-safe tests or '$0 stress' for comprehensive testing"
        exit 0
    fi
}

# Validate configuration
validate_config() {
    log_verbose "Validating configuration..."
    
    # Validate reliability level
    if [ "$RELIABILITY_LEVEL" != "basic" ] && [ "$RELIABILITY_LEVEL" != "stress" ]; then
        log_error "Invalid reliability level: $RELIABILITY_LEVEL (must be 'basic' or 'stress')"
        exit 1
    fi
    
    # Validate numeric parameters
    if ! [[ "$MAX_GOROUTINES" =~ ^[0-9]+$ ]] || [ "$MAX_GOROUTINES" -lt 1 ]; then
        log_error "Invalid max goroutines: $MAX_GOROUTINES (must be positive integer)"
        exit 1
    fi
    
    if ! [[ "$MAX_MEMORY_MB" =~ ^[0-9]+$ ]] || [ "$MAX_MEMORY_MB" -lt 64 ]; then
        log_error "Invalid memory limit: $MAX_MEMORY_MB (must be >= 64MB)"
        exit 1
    fi
    
    # Validate failure threshold
    if ! python3 -c "
import sys
try:
    threshold = float('$FAILURE_THRESHOLD')
    if threshold < 0.0 or threshold > 1.0:
        sys.exit(1)
except:
    sys.exit(1)
" 2>/dev/null; then
        log_error "Invalid failure threshold: $FAILURE_THRESHOLD (must be 0.0-1.0)"
        exit 1
    fi
    
    log_verbose "Configuration validation passed"
}

# Setup test environment
setup_environment() {
    log_info "Setting up test environment..."
    
    # Create output directory
    mkdir -p "$OUTPUT_DIR"
    
    # Export configuration as environment variables
    export TRACEZ_RELIABILITY_LEVEL="$RELIABILITY_LEVEL"
    export TRACEZ_RELIABILITY_DURATION="$DURATION"
    export TRACEZ_RELIABILITY_MAX_GOROUTINES="$MAX_GOROUTINES"
    export TRACEZ_RELIABILITY_MAX_MEMORY_MB="$MAX_MEMORY_MB"
    export TRACEZ_RELIABILITY_FAILURE_THRESHOLD="$FAILURE_THRESHOLD"
    export TRACEZ_RELIABILITY_VERBOSE="$VERBOSE"
    export TRACEZ_RELIABILITY_OUTPUT_DIR="$OUTPUT_DIR"
    
    log_verbose "Environment variables exported"
}

# Display configuration
show_config() {
    log_info "Reliability Test Configuration:"
    echo "  Level:              $RELIABILITY_LEVEL"
    echo "  Duration:           $DURATION"
    echo "  Max Goroutines:     $MAX_GOROUTINES"
    echo "  Memory Limit:       ${MAX_MEMORY_MB}MB"
    echo "  Failure Threshold:  $FAILURE_THRESHOLD"
    echo "  Verbose:            $VERBOSE"
    echo "  Output Directory:   $OUTPUT_DIR"
    echo ""
}

# Run reliability tests
run_tests() {
    log_info "Running $RELIABILITY_LEVEL reliability tests..."
    
    local test_args="-v"
    if [ "$VERBOSE" = "true" ]; then
        test_args="$test_args -test.v"
    fi
    
    local output_file="$OUTPUT_DIR/reliability-test-results.txt"
    local json_file="$OUTPUT_DIR/reliability-test-results.json"
    
    # Run tests with detailed output
    if go test $test_args -json . 2>&1 | tee "$output_file" | jq -r 'select(.Action == "output") | .Output' > "$json_file" 2>/dev/null; then
        log_success "All reliability tests passed"
        return 0
    else
        # Extract test results for analysis
        local failed_tests=$(grep "FAIL" "$output_file" | wc -l)
        local total_tests=$(grep -E "(PASS|FAIL)" "$output_file" | wc -l)
        
        if [ "$total_tests" -gt 0 ]; then
            local failure_rate=$(python3 -c "print(f'{$failed_tests / $total_tests:.3f}')")
            local threshold_check=$(python3 -c "print('PASS' if $failure_rate <= $FAILURE_THRESHOLD else 'FAIL')")
            
            log_warning "Test failures detected: $failed_tests/$total_tests (rate: $failure_rate)"
            
            if [ "$threshold_check" = "PASS" ]; then
                log_success "Failure rate within acceptable threshold ($FAILURE_THRESHOLD)"
                return 0
            else
                log_error "Failure rate exceeds threshold ($FAILURE_THRESHOLD)"
                return 1
            fi
        else
            log_error "No test results found"
            return 1
        fi
    fi
}

# Generate test report
generate_report() {
    log_info "Generating reliability test report..."
    
    local report_file="$OUTPUT_DIR/reliability-report.md"
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    
    cat > "$report_file" << EOF
# Tracez Reliability Test Report

**Generated:** $timestamp  
**Level:** $RELIABILITY_LEVEL  
**Duration:** $DURATION  
**Max Goroutines:** $MAX_GOROUTINES  
**Memory Limit:** ${MAX_MEMORY_MB}MB  
**Failure Threshold:** $FAILURE_THRESHOLD  

## Test Configuration

The reliability tests were executed using the CI-Safe, Stress-Capable pattern:

### Test Categories

1. **Collector Saturation** - Verify collector remains stable under extreme span ingestion
2. **Span Memory Pressure** - Test span operations under memory constraints  
3. **Tracer Lifecycle** - Verify tracer initialization, operation, and cleanup
4. **Context Cascade** - Test trace context propagation under extreme conditions
5. **Span Hierarchy Corruption** - Verify parent-child relationships under stress

### Test Levels

- **Basic ($RELIABILITY_LEVEL)**: CI-safe validation tests
- **Stress**: Production-level stress testing

## Results

EOF

    # Append test results if available
    if [ -f "$OUTPUT_DIR/reliability-test-results.txt" ]; then
        echo "### Test Output" >> "$report_file"
        echo "" >> "$report_file"
        echo '```' >> "$report_file"
        tail -50 "$OUTPUT_DIR/reliability-test-results.txt" >> "$report_file"
        echo '```' >> "$report_file"
    fi
    
    log_success "Report generated: $report_file"
}

# Cleanup function
cleanup() {
    log_verbose "Cleaning up test environment..."
    # Add any necessary cleanup here
}

# Main execution
main() {
    # Set trap for cleanup
    trap cleanup EXIT
    
    # Parse arguments
    parse_args "$@"
    
    # Environment detection and validation
    detect_environment
    validate_config
    setup_environment
    
    # Show configuration
    show_config
    
    # Execute tests
    if run_tests; then
        log_success "Reliability tests completed successfully"
        generate_report
        exit 0
    else
        log_error "Reliability tests failed"
        generate_report
        exit 1
    fi
}

# Execute main function with all arguments
main "$@"