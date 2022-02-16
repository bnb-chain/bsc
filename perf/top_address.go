package perf

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/common/prque"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var addrStatsEnabled, _ = getEnvBool("METRICS_TOP_ADDRESS_ENABLED")

type AddrTx struct {
	Address string `json:"address"`
	TxCount int64  `json:"tx_count"`
}

type AddrTxStats struct {
	AddrTxs []AddrTx  `json:"addr_txs"`
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
}

var eoaMap = make(map[string]int64)
var eoaLock sync.Mutex
var contractMap = make(map[string]int64)
var contractLock sync.Mutex

var ticker = time.NewTicker(15 * time.Minute) //use ticker to clean map to avoid OOM
const capOfMap = 500000

var httpRequesting = int32(0) //indicate whether there are http request in progress

var start = time.Now()

func UpdateTopEOAStats(address common.Address) {
	if !addrStatsEnabled {
		return
	}
	inc(eoaMap, &eoaLock, address.Hex())
}

func UpdateTopContractStats(address common.Address) {
	if !addrStatsEnabled {
		return
	}
	inc(contractMap, &contractLock, address.Hex())
}

func inc(m map[string]int64, lock *sync.Mutex, address string) {
	if atomic.LoadInt32(&httpRequesting) == 1 {
		//if there are http requests in progress, will not try to lock, otherwise requests cannot get lock for response
		return
	}
	lock.Lock()
	defer lock.Unlock()
	if val, ok := m[address]; ok {
		m[address] = val + 1
	} else {
		m[address] = 1
	}
}

func getTopEOA(topN int) AddrTxStats {
	result := top(eoaMap, &eoaLock, topN)
	return AddrTxStats{
		AddrTxs: result,
		Start:   start,
		End:     time.Now(),
	}
}

func getTopContract(topN int) AddrTxStats {
	result := top(contractMap, &contractLock, topN)
	return AddrTxStats{
		AddrTxs: result,
		Start:   start,
		End:     time.Now(),
	}
}

func top(m map[string]int64, lock *sync.Mutex, topN int) []AddrTx {
	lock.Lock()
	q := prque.New(nil)
	for k, v := range m {
		q.Push(k, v)
	}
	lock.Unlock()
	counter := 0
	var result []AddrTx
	for ; counter < topN && !q.Empty(); {
		addr, count := q.Pop()
		if addr != nil {
			result = append(result, AddrTx{
				Address: addr.(string),
				TxCount: count,
			})
			counter++
		}
	}
	return result
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
			atomic.StoreInt32(&httpRequesting, 1)
			time.Sleep(50 * time.Millisecond)
			defer func() { atomic.StoreInt32(&httpRequesting, 0) }()
			serve(getTopEOA, c)
		})
		router.GET("/topContract/:topN", func(c *gin.Context) {
			atomic.StoreInt32(&httpRequesting, 1)
			defer func() { atomic.StoreInt32(&httpRequesting, 0) }()
			time.Sleep(50 * time.Millisecond)
			serve(getTopContract, c)
		})
		err := router.Run(":6001")
		if err != nil {
			fmt.Println("Failed to start gin server")
		}
	}()
	go func() {
		for range ticker.C {
			if atomic.LoadInt32(&httpRequesting) == 1 {
				return
			}
			fmt.Println("Cleaning top address maps...")
			clean(eoaMap, &eoaLock)
			clean(contractMap, &contractLock)
			fmt.Println("Cleaned top address maps...")
		}
	}()
}

func clean(m map[string]int64, lock *sync.Mutex) {
	lock.Lock()
	defer lock.Unlock()

	valueToDel := int64(1)
	for ; len(m) > capOfMap; {
		fmt.Println("Value to delete: " + strconv.FormatInt(valueToDel, 10))
		minValue := int64(math.MaxInt64)
		for k, v := range m {
			if v < minValue {
				minValue = v
			}
			if v == valueToDel {
				delete(m, k)
			}
		}
		valueToDel = minValue
	}
}
