package perf

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/prque"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

var addrStatsEnabled, _ = getEnvBool("METRICS_TOP_ADDRESS_ENABLED")

type AddrTxStats struct {
	Map   map[string]int64 `json:"map"`
	Start time.Time        `json:"start"`
	End   time.Time        `json:"end"`
}

var eoaMap = make(map[string]int64)
var contractMap = make(map[string]int64)

var start = time.Now()

func UpdateTopEOAStats(address common.Address) {
	if !addrStatsEnabled {
		return
	}
	inc(eoaMap, address.Hex())
}

func UpdateTopContractStats(address common.Address) {
	if !addrStatsEnabled {
		return
	}
	inc(contractMap, address.Hex())
}

func inc(m map[string]int64, address string) {
	if val, ok := m[address]; ok {
		m[address] = val + 1
	} else {
		m[address] = 1
	}
}

func getTopEOA(topN int) AddrTxStats {
	topM := top(eoaMap, topN)
	return AddrTxStats{
		Map:   topM,
		Start: start,
		End:   time.Now(),
	}
}

func getTopContract(topN int) AddrTxStats {
	topM := top(contractMap, topN)
	return AddrTxStats{
		Map:   topM,
		Start: start,
		End:   time.Now(),
	}
}

func top(m map[string]int64, topN int) (topM map[string]int64) {
	q := prque.New(nil)
	for k, v := range m {
		q.Push(k, v)
	}
	counter := 0
	topM = make(map[string]int64)
	for ; counter < topN && !q.Empty(); {
		addr, count := q.Pop()
		if addr != nil {
			topM[addr.(string)] = count
			counter++
		}
	}
	return topM
}

type getTop func(int) AddrTxStats

func serve(f getTop, c *gin.Context) {
	param := c.Param("topN")
	topN, err := strconv.Atoi(param)
	if err != nil {
		c.JSON(http.StatusBadRequest, nil)
		return
	}
	result := f(topN)
	c.JSON(http.StatusOK, result)
}

func StartTopAddrStats() {
	if !addrStatsEnabled {
		return
	}
	go func() {
		router := gin.Default()
		router.GET("/topEOA/:topN", func(c *gin.Context) {
			serve(getTopEOA, c)
		})
		router.GET("/topContract/:topN", func(c *gin.Context) {
			serve(getTopContract, c)
		})
		err := router.Run(":6001")
		if err != nil {
			fmt.Println("Failed to start gin server")
		}
	}()
}
