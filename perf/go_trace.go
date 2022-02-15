package perf

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
	"runtime/trace"
	"strconv"
	"sync"
	"time"
)

var traceMetricsEnabled, _ = getEnvBool("METRICS_GO_TRACE_ENABLED")

var mutex sync.Mutex
var running bool
var file *os.File

func StartGoTraceMetrics() {
	if !traceMetricsEnabled {
		return
	}
	startTrace()
	go func() {
		router := gin.Default()
		router.GET("/trace/start", func(c *gin.Context) {
			startTrace()
			c.String(http.StatusOK, "Trace running status: "+strconv.FormatBool(running))
		})
		router.GET("/trace/stop", func(c *gin.Context) {
			stopTrace()
			c.String(http.StatusOK, "Trace running status: "+strconv.FormatBool(running))
		})
		err := router.Run(":6002")
		if err != nil {
			fmt.Println("Failed to start gin server")
		}
	}()
}

func startTrace() {
	mutex.Lock()
	defer mutex.Unlock()

	if running {
		return
	}

	timeUnix := time.Now().Unix()
	fileName := "trace_" + strconv.FormatInt(timeUnix, 10) + ".out"
	fmt.Println("Trace to fileName: " + fileName)
	file, err := os.Create(fileName)
	if err != nil {
		fmt.Println("Failed to create file: " + fileName)
	}

	err = trace.Start(file)
	if err != nil {
		fmt.Println("Failed to start go trace")
	} else {
		running = true
	}
}

func StopGoTraceMetrics() {
	if !traceMetricsEnabled {
		return
	}
	stopTrace()
}

func stopTrace() {
	mutex.Lock()
	defer mutex.Unlock()

	if running {
		trace.Stop()
		file.Close()
		running = false
	}
}
