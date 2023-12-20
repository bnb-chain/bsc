// Copyright 2022 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>

package aggpathdb

import "github.com/ethereum/go-ethereum/metrics"

var (
	cleanHitMeter   = metrics.NewRegisteredMeter("aggpathdb/clean/hit", nil)
	cleanMissMeter  = metrics.NewRegisteredMeter("aggpathdb/clean/miss", nil)
	cleanReadMeter  = metrics.NewRegisteredMeter("aggpathdb/clean/read", nil)
	cleanWriteMeter = metrics.NewRegisteredMeter("aggpathdb/clean/write", nil)
	cleanFalseMeter = metrics.NewRegisteredMeter("aggpathdb/clean/false", nil)

	nodeTimer           = metrics.NewRegisteredTimer("aggpathdb/node/time", nil)
	nodeBufferTimer     = metrics.NewRegisteredTimer("aggpathdb/node/buffer/time", nil)
	nodeImmuBufferTimer = metrics.NewRegisteredTimer("aggpathdb/node/immubuffer/time", nil)
	nodeCleanCacheTimer = metrics.NewRegisteredTimer("aggpathdb/node/cleancache/time", nil)
	nodeDiskTimer       = metrics.NewRegisteredTimer("aggpathdb/node/disk/time", nil)

	dirtyHitMeter         = metrics.NewRegisteredMeter("aggpathdb/dirty/hit", nil)
	dirtyMissMeter        = metrics.NewRegisteredMeter("aggpathdb/dirty/miss", nil)
	dirtyReadMeter        = metrics.NewRegisteredMeter("aggpathdb/dirty/read", nil)
	dirtyWriteMeter       = metrics.NewRegisteredMeter("aggpathdb/dirty/write", nil)
	dirtyNodeHitDepthHist = metrics.NewRegisteredHistogram("aggpathdb/dirty/depth", nil, metrics.NewExpDecaySample(1028, 0.015))

	aggNodeHitBufferMeter     = metrics.NewRegisteredMeter("aggpathdb/aggnode/hit/buffer", nil)
	aggNodeHitImmuBufferMeter = metrics.NewRegisteredMeter("aggpathdb/aggnode/hit/immubuffer", nil)
	aggNodeHitCleanCacheMeter = metrics.NewRegisteredMeter("aggpathdb/aggnode/hit/cleancache", nil)

	aggNodeMissMeter           = metrics.NewRegisteredMeter("aggpathdb/aggnode/miss", nil)
	aggNodeTimeBufferTimer     = metrics.NewRegisteredTimer("aggpathdb/aggnode/time/buffer", nil)
	aggNodeTimeImmuBufferTimer = metrics.NewRegisteredTimer("aggpathdb/aggnode/time/immubuffer", nil)
	aggNodeTimeCleanCacheTimer = metrics.NewRegisteredTimer("aggpathdb/aggnode/time/cleancache", nil)
	aggNodeTimeDiskTimer       = metrics.NewRegisteredTimer("aggpathdb/aggnode/time/disk", nil)

	dirtyFalseMeter = metrics.NewRegisteredMeter("aggpathdb/dirty/false", nil)
	diskFalseMeter  = metrics.NewRegisteredMeter("aggpathdb/disk/false", nil)

	commitTimeTimer             = metrics.NewRegisteredTimer("aggpathdb/commit/time", nil)
	commitWriteHistoryTimeTimer = metrics.NewRegisteredTimer("aggpathdb/commit/writehistory/time", nil)
	commitWriteStateIDTimeTimer = metrics.NewRegisteredTimer("aggpathdb/commit/writestateid/time", nil)
	commitCommitNodesTimeTimer  = metrics.NewRegisteredTimer("aggpathdb/commit/commitnodes/time", nil)
	commitFlushTimer            = metrics.NewRegisteredTimer("aggpathdb/commit/flush/time", nil)
	commitTruncateHistoryTimer  = metrics.NewRegisteredTimer("aggpathdb/commit/truncatehistory/time", nil)
	commitMergeAsyncMapTimer    = metrics.NewRegisteredTimer("aggpathdb/commit/mergeasync/time", nil)

	flushTimeTimer  = metrics.NewRegisteredTimer("aggpathdb/flush/time", nil)
	flushNodesMeter = metrics.NewRegisteredMeter("aggpathdb/flush/aggNodes", nil)
	flushBytesMeter = metrics.NewRegisteredMeter("aggpathdb/flush/bytes", nil)

	gcNodesMeter = metrics.NewRegisteredMeter("aggpathdb/gc/aggNodes", nil)
	gcBytesMeter = metrics.NewRegisteredMeter("aggpathdb/gc/bytes", nil)

	diffLayerBytesMeter = metrics.NewRegisteredMeter("aggpathdb/diff/bytes", nil)
	diffLayerNodesMeter = metrics.NewRegisteredMeter("aggpathdb/diff/aggNodes", nil)

	historyBuildTimeMeter  = metrics.NewRegisteredTimer("aggpathdb/history/time", nil)
	historyDataBytesMeter  = metrics.NewRegisteredMeter("aggpathdb/history/bytes/data", nil)
	historyIndexBytesMeter = metrics.NewRegisteredMeter("aggpathdb/history/bytes/index", nil)
)
