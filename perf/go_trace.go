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

	go func() {
		router := gin.Default()
		router.GET("/trace/start", func(c *gin.Context) {
			durationStr := c.Query("duration")
			duration, err := strconv.Atoi(durationStr)
			if err != nil {
				c.String(http.StatusBadRequest, "Please pass duration in seconds, e.g., ?duration=300")
				return
			}
			startTrace(duration)
			c.String(http.StatusOK, "Trace running status: "+strconv.FormatBool(running))
		})

		err := router.Run(":6002")
		if err != nil {
			fmt.Println("Failed to start gin server")
		}
	}()
}

func startTrace(duration int) {
	mutex.Lock()
	defer mutex.Unlock()

	if running {
		return
	}

	timeUnix := time.Now().Unix()
	fileName := "trace_" + strconv.FormatInt(timeUnix, 10) + ".out"
	fmt.Println("Tracing to fileName: " + fileName)
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

	go func(d int) {
		timer := time.NewTimer(time.Duration(float64(d) * float64(time.Second)))
		<-timer.C
		stopTrace()
	}(duration)
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
		fmt.Println("Trace stopped")
		trace.Stop()
		file.Close()
		running = false
	}
}
