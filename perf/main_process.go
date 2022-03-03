package perf

import (
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"time"
)

type MpMetricsName string

const (
	MpMiningTotal         MpMetricsName = "MP_MINING_TOTAL"
	MpMiningPrepare       MpMetricsName = "MP_MINING_PREPARE"
	MpMiningOrder         MpMetricsName = "MP_MINING_ORDER"
	MpMiningCommitTx      MpMetricsName = "MP_MINING_COMMIT_TX"
	MpMiningCommitDelay   MpMetricsName = "MP_MINING_COMMIT_DELAY"
	MpMiningCommitProcess MpMetricsName = "MP_MINING_COMMIT_PROCESS"
	MpMiningFinalize      MpMetricsName = "MP_MINING_FINALIZE"
	MpMiningWrite         MpMetricsName = "MP_MINING_WRITE"

	MpImportingTotal           MpMetricsName = "MP_IMPORTING_TOTAL"
	MpImportingVerifyHeader    MpMetricsName = "MP_IMPORTING_VERIFY_HEADER"
	MpImportingVerifyState     MpMetricsName = "MP_IMPORTING_VERIFY_STATE"
	MpImportingProcess         MpMetricsName = "MP_IMPORTING_PROCESS"
	MpImportingProcessPreload  MpMetricsName = "MP_IMPORTING_PROCESS_PRELOAD"
	MpImportingProcessExecute  MpMetricsName = "MP_IMPORTING_PROCESS_EXECUTE"
	MpImportingProcessFinalize MpMetricsName = "MP_IMPORTING_PROCESS_FINALIZE"
	MpImportingCommit          MpMetricsName = "MP_IMPORTING_COMMIT"

	MpPropagationTotal         MpMetricsName = "MP_PROPAGATION_TOTAL"
	MpPropagationSend          MpMetricsName = "MP_PROPAGATION_SEND"
	MpPropagationRequestHeader MpMetricsName = "MP_PROPAGATION_REQUEST_HEADER"
	MpPropagationRequestBodies MpMetricsName = "MP_PROPAGATION_REQUEST_BODIES"

	MpBadBlock MpMetricsName = "MP_BAD_BLOCK"
)

var mpMetricsEnabled, _ = getEnvBool("METRICS_MP_METRICS_ENABLED")

var (
	//block mining related metrics
	miningTotalAllCounter = metrics.NewRegisteredCounter("mp/mining/total/all", nil)
	miningTotalTimer      = metrics.NewRegisteredTimer("mp/mining/total", nil)

	miningPrepareAllCounter = metrics.NewRegisteredCounter("mp/mining/prepare/all", nil)
	miningPrepareTimer      = metrics.NewRegisteredTimer("mp/mining/prepare", nil)

	miningOrderAllCounter = metrics.NewRegisteredCounter("mp/mining/order/all", nil)
	miningOrderTimer      = metrics.NewRegisteredTimer("mp/mining/order", nil)

	miningCommitTxAllCounter = metrics.NewRegisteredCounter("mp/mining/commit_tx/all", nil)
	miningCommitTxTimer      = metrics.NewRegisteredTimer("mp/mining/commit_tx", nil)

	miningCommitDelayAllCounter = metrics.NewRegisteredCounter("mp/mining/commit/delay/all", nil)
	miningCommitDelayTimer      = metrics.NewRegisteredTimer("mp/mining/commit/delay", nil)

	miningCommitProcessAllCounter = metrics.NewRegisteredCounter("mp/mining/commit/process/all", nil)
	miningCommitProcessTimer      = metrics.NewRegisteredTimer("mp/mining/commit/process", nil)

	miningFinalizeAllCounter = metrics.NewRegisteredCounter("mp/mining/finalize/all", nil)
	miningFinalizeTimer      = metrics.NewRegisteredTimer("mp/mining/finalize", nil)

	miningWriteAllCounter = metrics.NewRegisteredCounter("mp/mining/write/all", nil)
	miningWriteTimer      = metrics.NewRegisteredTimer("mp/mining/write", nil)

	//block importing related metrics
	importingTotalAllCounter = metrics.NewRegisteredCounter("mp/importing/total/all", nil)
	importingTotalTimer      = metrics.NewRegisteredTimer("mp/importing/total", nil)

	importingVerifyHeaderAllCounter = metrics.NewRegisteredCounter("mp/importing/verify/header/all", nil)
	importingVerifyHeaderTimer      = metrics.NewRegisteredTimer("mp/importing/verify/header", nil)

	importingVerifyStateAllCounter = metrics.NewRegisteredCounter("mp/importing/verify/state/all", nil)
	importingVerifyStateTimer      = metrics.NewRegisteredTimer("mp/importing/verify/state", nil)

	importingProcessAllCounter = metrics.NewRegisteredCounter("mp/importing/process/all", nil)
	importingProcessTimer      = metrics.NewRegisteredTimer("mp/importing/process", nil)

	importingProcessPreloadAllCounter = metrics.NewRegisteredCounter("mp/importing/process/preload/all", nil)
	importingProcessPreloadTimer      = metrics.NewRegisteredTimer("mp/importing/process/preload", nil)

	importingProcessExecuteAllCounter = metrics.NewRegisteredCounter("mp/importing/process/execute/all", nil)
	importingProcessExecuteTimer      = metrics.NewRegisteredTimer("mp/importing/process/execute", nil)

	importingProcessFinalizeAllCounter = metrics.NewRegisteredCounter("mp/importing/process/finalize/all", nil)
	importingProcessFinalizeTimer      = metrics.NewRegisteredTimer("mp/importing/process/finalize", nil)

	importingCommitAllCounter = metrics.NewRegisteredCounter("mp/importing/commit/all", nil)
	importingCommitTimer      = metrics.NewRegisteredTimer("mp/importing/commit", nil)

	//block importing, block mining, p2p overall metrics
	propagationTotalTimer = metrics.NewRegisteredTimer("mp/propagation/total", nil)
	//total is less than send for async send is used
	propagationSendTimer          = metrics.NewRegisteredTimer("mp/propagation/send", nil)
	propagationRequestHeaderTimer = metrics.NewRegisteredTimer("mp/propagation/request/header", nil)
	propagationRequestBodiesTimer = metrics.NewRegisteredTimer("mp/propagation/request/bodies", nil)

	//bad block counter
	badBlockCounter = metrics.NewRegisteredCounter("mp/bad_block", nil)
)

func RecordMPMetrics(metricsName MpMetricsName, start time.Time) {
	if !mpMetricsEnabled {
		return
	}

	switch metricsName {
	case MpMiningTotal:
		recordTimer(miningTotalTimer, start)
		increaseCounter(miningTotalAllCounter, start)
	case MpMiningPrepare:
		recordTimer(miningPrepareTimer, start)
		increaseCounter(miningPrepareAllCounter, start)
	case MpMiningOrder:
		recordTimer(miningOrderTimer, start)
		increaseCounter(miningOrderAllCounter, start)
	case MpMiningCommitTx:
		recordTimer(miningCommitTxTimer, start)
		increaseCounter(miningCommitTxAllCounter, start)
	case MpMiningCommitDelay:
		recordTimer(miningCommitDelayTimer, start)
		increaseCounter(miningCommitDelayAllCounter, start)
	case MpMiningCommitProcess:
		recordTimer(miningCommitProcessTimer, start)
		increaseCounter(miningCommitProcessAllCounter, start)
	case MpMiningFinalize:
		recordTimer(miningFinalizeTimer, start)
		increaseCounter(miningFinalizeAllCounter, start)
	case MpMiningWrite:
		recordTimer(miningWriteTimer, start)
		increaseCounter(miningWriteAllCounter, start)

	case MpImportingTotal:
		recordTimer(importingTotalTimer, start)
		increaseCounter(importingTotalAllCounter, start)
	case MpImportingVerifyHeader:
		recordTimer(importingVerifyHeaderTimer, start)
		increaseCounter(importingVerifyHeaderAllCounter, start)
	case MpImportingVerifyState:
		recordTimer(importingVerifyStateTimer, start)
		increaseCounter(importingVerifyStateAllCounter, start)
	case MpImportingProcess:
		recordTimer(importingProcessTimer, start)
		increaseCounter(importingProcessAllCounter, start)
	case MpImportingProcessPreload:
		recordTimer(importingProcessPreloadTimer, start)
		increaseCounter(importingProcessPreloadAllCounter, start)
	case MpImportingProcessExecute:
		recordTimer(importingProcessExecuteTimer, start)
		increaseCounter(importingProcessExecuteAllCounter, start)
	case MpImportingProcessFinalize:
		recordTimer(importingProcessFinalizeTimer, start)
		increaseCounter(importingProcessFinalizeAllCounter, start)
	case MpImportingCommit:
		recordTimer(importingCommitTimer, start)
		increaseCounter(importingCommitAllCounter, start)

	case MpPropagationTotal:
		recordTimer(propagationTotalTimer, start)
	case MpPropagationSend:
		recordTimer(propagationSendTimer, start)
	case MpPropagationRequestHeader:
		recordTimer(propagationRequestHeaderTimer, start)
	case MpPropagationRequestBodies:
		recordTimer(propagationRequestBodiesTimer, start)

	case MpBadBlock:
		badBlockCounter.Inc(1)
	}
}

func RecordMPLogs(logger log.Logger, msg string, ctx ...interface{}) {
	if !mpMetricsEnabled {
		return
	}

	if logger != nil {
		logger.Info(msg, ctx...)
	} else {
		log.Info(msg, ctx...)
	}

}

func recordTimer(timer metrics.Timer, start time.Time) {
	timer.Update(time.Since(start))
}

func increaseCounter(counter metrics.Counter, start time.Time) {
	counter.Inc(time.Since(start).Nanoseconds())
}
