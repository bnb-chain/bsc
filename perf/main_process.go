package perf

import (
	"github.com/ethereum/go-ethereum/metrics"
	"time"
)

type MpMetricsName string

const (
	MpMiningTotal         MpMetricsName = "MP_MINING_TOTAL"
	MpMiningPrepare       MpMetricsName = "MP_MINING_PREPARE"
	MpMiningOrder         MpMetricsName = "MP_MINING_ORDER"
	MpMiningCommit        MpMetricsName = "MP_MINING_COMMIT"
	MpMiningCommitDelay   MpMetricsName = "MP_MINING_COMMIT_DELAY"
	MpMiningCommitProcess MpMetricsName = "MP_MINING_COMMIT_PROCESS"
	MpMiningFinalize      MpMetricsName = "MP_MINING_FINALIZE"
	MpMiningWrite         MpMetricsName = "MP_MINING_WRITE"

	MpImportingTotal        MpMetricsName = "MP_IMPORTING_TOTAL"
	MpImportingVerifyHeader MpMetricsName = "MP_IMPORTING_VERIFY_HEADER"
	MpImportingVerifyState  MpMetricsName = "MP_IMPORTING_VERIFY_STATE"
	MpImportingProcess      MpMetricsName = "MP_IMPORTING_PROCESS"
	MpImportingCommit       MpMetricsName = "MP_IMPORTING_COMMIT"

	MpPropagationTotal MpMetricsName = "MP_PROPAGATION_TOTAL"
	MpPropagationSend  MpMetricsName = "MP_PROPAGATION_SEND"
)

var mpMetricsEnabled, _ = getEnvBool("METRICS_MP_METRICS_ENABLED")

var (
	//block mining related metrics
	miningTotalTimer         = metrics.NewRegisteredTimer("mp/mining/total", nil)
	miningPrepareTimer       = metrics.NewRegisteredTimer("mp/mining/prepare", nil)
	miningOrderTimer         = metrics.NewRegisteredTimer("mp/mining/order", nil)
	miningCommitTimer        = metrics.NewRegisteredTimer("mp/mining/commit", nil)
	miningCommitDelayTimer   = metrics.NewRegisteredTimer("mp/mining/commit/delay", nil)
	miningCommitProcessTimer = metrics.NewRegisteredTimer("mp/mining/commit/process", nil)
	miningFinalizeTimer      = metrics.NewRegisteredTimer("mp/mining/finalize", nil)
	miningWriteTimer         = metrics.NewRegisteredTimer("mp/mining/write", nil)

	//block importing related metrics
	importingTotalTimer        = metrics.NewRegisteredTimer("mp/importing/total", nil)
	importingVerifyHeaderTimer = metrics.NewRegisteredTimer("mp/importing/verify/header", nil)
	importingVerifyStateTimer  = metrics.NewRegisteredTimer("mp/importing/verify/state", nil)
	importingProcessTimer      = metrics.NewRegisteredTimer("mp/importing/process", nil)
	importingCommitTimer       = metrics.NewRegisteredTimer("mp/importing/commit", nil)

	//block importing, block mining, p2p overall metrics
	propagationTotalTimer = metrics.NewRegisteredTimer("mp/propagation/total", nil)
	//total is less than send for async send is used
	propagationSendTimer = metrics.NewRegisteredTimer("mp/propagation/send", nil)
)

func RecordMPMetrics(metricsName MpMetricsName, start time.Time) {
	if !mpMetricsEnabled {
		return
	}

	switch metricsName {
	case MpMiningTotal:
		recordTimer(miningTotalTimer, start)
	case MpMiningPrepare:
		recordTimer(miningPrepareTimer, start)
	case MpMiningOrder:
		recordTimer(miningOrderTimer, start)
	case MpMiningCommit:
		recordTimer(miningCommitTimer, start)
	case MpMiningCommitDelay:
		recordTimer(miningCommitDelayTimer, start)
	case MpMiningCommitProcess:
		recordTimer(miningCommitProcessTimer, start)
	case MpMiningFinalize:
		recordTimer(miningFinalizeTimer, start)
	case MpMiningWrite:
		recordTimer(miningWriteTimer, start)

	case MpImportingTotal:
		recordTimer(importingTotalTimer, start)
	case MpImportingVerifyHeader:
		recordTimer(importingVerifyHeaderTimer, start)
	case MpImportingVerifyState:
		recordTimer(importingVerifyStateTimer, start)
	case MpImportingProcess:
		recordTimer(importingProcessTimer, start)
	case MpImportingCommit:
		recordTimer(importingCommitTimer, start)

	case MpPropagationTotal:
		recordTimer(propagationTotalTimer, start)
	case MpPropagationSend:
		recordTimer(propagationSendTimer, start)
	}
}

func recordTimer(timer metrics.Timer, start time.Time) {
	timer.Update(time.Since(start))
}
