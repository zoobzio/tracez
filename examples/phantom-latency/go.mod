module github.com/zoobzio/tracez/examples/phantom-latency

go 1.23

toolchain go1.24.5

require (
	github.com/zoobzio/clockz v0.1.0
	github.com/zoobzio/tracez v0.1.0
)

replace github.com/zoobzio/tracez => ../..

replace github.com/zoobzio/clockz => ../../../clockz
