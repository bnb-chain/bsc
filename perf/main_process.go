package perf

import (
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"time"
)

type MpMetricsName string

const (
	MpMiningTotalAll      MpMetricsName = "MP_MINING_TOTAL_ALL"
	MpMiningTotal         MpMetricsName = "MP_MINING_TOTAL"
	MpMiningPrepare       MpMetricsName = "MP_MINING_PREPARE"
	MpMiningOrder         MpMetricsName = "MP_MINING_ORDER"
	MpMiningCommitTx      MpMetricsName = "MP_MINING_COMMIT_TX"
	MpMiningCommitDelay   MpMetricsName = "MP_MINING_COMMIT_DELAY"
	MpMiningCommitProcess MpMetricsName = "MP_MINING_COMMIT_PROCESS"
	MpMiningFinalize      MpMetricsName = "MP_MINING_FINALIZE"
	MpMiningWrite         MpMetricsName = "MP_MINING_WRITE"

	MpImportingTotalAll            MpMetricsName = "MP_IMPORTING_TOTAL_ALL"
	MpImportingTotal               MpMetricsName = "MP_IMPORTING_TOTAL"
	MpImportingVerifyHeader        MpMetricsName = "MP_IMPORTING_VERIFY_HEADER"
	MpImportingVerifyState         MpMetricsName = "MP_IMPORTING_VERIFY_STATE"
	MpImportingProcess             MpMetricsName = "MP_IMPORTING_PROCESS"
	MpImportingProcessPreload      MpMetricsName = "MP_IMPORTING_PROCESS_PRELOAD"
	MpImportingProcessExecute      MpMetricsName = "MP_IMPORTING_PROCESS_EXECUTE"
	MpImportingProcessExecuteApply MpMetricsName = "MP_IMPORTING_PROCESS_EXECUTE_APPLY"
	MpImportingProcessFinalize     MpMetricsName = "MP_IMPORTING_PROCESS_FINALIZE"
	MpImportingCommit              MpMetricsName = "MP_IMPORTING_COMMIT"

	MpPropagationTotal         MpMetricsName = "MP_PROPAGATION_TOTAL"
	MpPropagationSend          MpMetricsName = "MP_PROPAGATION_SEND"
	MpPropagationRequestHeader MpMetricsName = "MP_PROPAGATION_REQUEST_HEADER"
	MpPropagationRequestBodies MpMetricsName = "MP_PROPAGATION_REQUEST_BODIES"

	MpBadBlock MpMetricsName = "MP_BAD_BLOCK"
)

var mpMetricsEnabled, _ = getEnvBool("METRICS_MP_METRICS_ENABLED")

var (
	//block mining related metrics
	miningTotalAllCounter    = metrics.NewRegisteredCounter("mp/mining/total/all", nil)
	miningTotalTimer         = metrics.NewRegisteredTimer("mp/mining/total", nil)
	miningPrepareTimer       = metrics.NewRegisteredTimer("mp/mining/prepare", nil)
	miningOrderTimer         = metrics.NewRegisteredTimer("mp/mining/order", nil)
	miningCommitTxTimer      = metrics.NewRegisteredTimer("mp/mining/commit_tx", nil)
	miningCommitDelayTimer   = metrics.NewRegisteredTimer("mp/mining/commit/delay", nil)
	miningCommitProcessTimer = metrics.NewRegisteredTimer("mp/mining/commit/process", nil)
	miningFinalizeTimer      = metrics.NewRegisteredTimer("mp/mining/finalize", nil)
	miningWriteTimer         = metrics.NewRegisteredTimer("mp/mining/write", nil)

	//block importing related metrics
	importingTotalAllCounter          = metrics.NewRegisteredCounter("mp/importing/total/all", nil)
	importingTotalTimer               = metrics.NewRegisteredTimer("mp/importing/total", nil)
	importingVerifyHeaderTimer        = metrics.NewRegisteredTimer("mp/importing/verify/header", nil)
	importingVerifyStateTimer         = metrics.NewRegisteredTimer("mp/importing/verify/state", nil)
	importingProcessTimer             = metrics.NewRegisteredTimer("mp/importing/process", nil)
	importingProcessPreloadTimer      = metrics.NewRegisteredTimer("mp/importing/process/preload", nil)
	importingProcessExecuteTimer      = metrics.NewRegisteredTimer("mp/importing/process/execute", nil)
	importingProcessExecuteApplyTimer = metrics.NewRegisteredTimer("mp/importing/process/execute/apply", nil)
	importingProcessFinalizeTimer     = metrics.NewRegisteredTimer("mp/importing/process/finalize", nil)
	importingCommitTimer              = metrics.NewRegisteredTimer("mp/importing/commit", nil)

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
	case MpMiningTotalAll:
		miningTotalAllCounter.Inc(time.Since(start).Nanoseconds())
	case MpMiningTotal:
		recordTimer(miningTotalTimer, start)
	case MpMiningPrepare:
		recordTimer(miningPrepareTimer, start)
	case MpMiningOrder:
		recordTimer(miningOrderTimer, start)
	case MpMiningCommitTx:
		recordTimer(miningCommitTxTimer, start)
	case MpMiningCommitDelay:
		recordTimer(miningCommitDelayTimer, start)
	case MpMiningCommitProcess:
		recordTimer(miningCommitProcessTimer, start)
	case MpMiningFinalize:
		recordTimer(miningFinalizeTimer, start)
	case MpMiningWrite:
		recordTimer(miningWriteTimer, start)

	case MpImportingTotalAll:
		importingTotalAllCounter.Inc(time.Since(start).Nanoseconds())
	case MpImportingTotal:
		recordTimer(importingTotalTimer, start)
	case MpImportingVerifyHeader:
		recordTimer(importingVerifyHeaderTimer, start)
	case MpImportingVerifyState:
		recordTimer(importingVerifyStateTimer, start)
	case MpImportingProcess:
		recordTimer(importingProcessTimer, start)
	case MpImportingProcessPreload:
		recordTimer(importingProcessPreloadTimer, start)
	case MpImportingProcessExecute:
		recordTimer(importingProcessExecuteTimer, start)
	case MpImportingProcessExecuteApply:
		recordTimer(importingProcessExecuteApplyTimer, start)
	case MpImportingProcessFinalize:
		recordTimer(importingProcessFinalizeTimer, start)
	case MpImportingCommit:
		recordTimer(importingCommitTimer, start)

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
