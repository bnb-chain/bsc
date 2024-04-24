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

package pathdb

import "github.com/ethereum/go-ethereum/metrics"

var (
	cleanHitMeter   = metrics.NewRegisteredMeter("pathdb/clean/hit", nil)
	cleanMissMeter  = metrics.NewRegisteredMeter("pathdb/clean/miss", nil)
	cleanReadMeter  = metrics.NewRegisteredMeter("pathdb/clean/read", nil)
	cleanWriteMeter = metrics.NewRegisteredMeter("pathdb/clean/write", nil)

	trieCleanStorageHitMeter  = metrics.NewRegisteredMeter("pathdb/clean/storage/hit", nil)
	trieCleanStorageMissMeter = metrics.NewRegisteredMeter("path/clean/storage/miss", nil)
	trieDirtyStorageHitMeter  = metrics.NewRegisteredMeter("pathdb/dirty/storage/hit", nil)
	trieDirtyStorageMissMeter = metrics.NewRegisteredMeter("path/dirty/storage/miss", nil)

	trieCleanAccountHitMeter  = metrics.NewRegisteredMeter("pathdb/clean/account/hit", nil)
	trieCleanAccountMissMeter = metrics.NewRegisteredMeter("path/clean/account/miss", nil)
	trieDirtyAccountHitMeter  = metrics.NewRegisteredMeter("pathdb/dirty/account/hit", nil)
	trieDirtyAccountMissMeter = metrics.NewRegisteredMeter("path/dirty/account/miss", nil)

	trieDiffLayerStorageHitMeter  = metrics.NewRegisteredMeter("pathdb/difflayer/storage/hit", nil)
	trieDiffLayerStorageMissMeter = metrics.NewRegisteredMeter("pathdb/difflayer/storage/miss", nil)
	trieDiffLayerAccountHitMeter  = metrics.NewRegisteredMeter("pathdb/difflayer/account/hit", nil)
	trieDiffLayerAccountMissMeter = metrics.NewRegisteredMeter("pathdb/difflayer/account/miss", nil)

	trieDiskLayerStorageHitMeter  = metrics.NewRegisteredMeter("pathdb/disklayer/storage/hit", nil)
	trieDiskLayerStorageMissMeter = metrics.NewRegisteredMeter("pathdb/disklayer/storage/miss", nil)
	trieDiskLayerAccountHitMeter  = metrics.NewRegisteredMeter("pathdb/disklayer/account/hit", nil)
	trieDiskLayerAccountMissMeter = metrics.NewRegisteredMeter("pathdb/disklayer/account/miss", nil)

	dirtyHitMeter         = metrics.NewRegisteredMeter("pathdb/dirty/hit", nil)
	dirtyMissMeter        = metrics.NewRegisteredMeter("pathdb/dirty/miss", nil)
	dirtyReadMeter        = metrics.NewRegisteredMeter("pathdb/dirty/read", nil)
	dirtyWriteMeter       = metrics.NewRegisteredMeter("pathdb/dirty/write", nil)
	dirtyNodeHitDepthHist = metrics.NewRegisteredHistogram("pathdb/dirty/depth", nil, metrics.NewExpDecaySample(1028, 0.015))

	diffNodeTimer             = metrics.NewRegisteredTimer("pathdb/diff/node/timer", nil)
	diskBufferNodeTimer       = metrics.NewRegisteredTimer("pathdb/disk/buffer/node/timer", nil)
	diskCleanNodeTimer        = metrics.NewRegisteredTimer("pathdb/disk/clean/node/timer", nil)
	diskDBNodeTimer           = metrics.NewRegisteredTimer("pathdb/disk/db/node/timer", nil)
	diskReadAccountTimer      = metrics.NewRegisteredTimer("pathdb/disk/readtrie/account/timer", nil)
	diskReadStorageTimer      = metrics.NewRegisteredTimer("pathdb/disk/readtrie/storage/timer", nil)
	readAndDecodeAccountTimer = metrics.NewRegisteredTimer("pathdb/disk/readtrie/account/totaltime", nil)
	readAndDecodeStorageTimer = metrics.NewRegisteredTimer("pathdb/disk/readtrie/storage/totaltime", nil)

	diskTotalAccountCounter     = metrics.NewRegisteredCounter("pathdb/disk/readtrie/account/counter", nil)
	diskTotalStorageCounter     = metrics.NewRegisteredCounter("pathdb/disk/readtrie/storage/counter", nil)
	diskTotalMissAccoutCounter  = metrics.NewRegisteredCounter("pathdb/disk/readtrie/account/misscounter", nil)
	diskTotalMissStorageCounter = metrics.NewRegisteredCounter("pathdb/disk/readtrie/storage/misscounter", nil)

	cleanFalseMeter = metrics.NewRegisteredMeter("pathdb/clean/false", nil)
	dirtyFalseMeter = metrics.NewRegisteredMeter("pathdb/dirty/false", nil)
	diskFalseMeter  = metrics.NewRegisteredMeter("pathdb/disk/false", nil)

	commitTimeTimer  = metrics.NewRegisteredTimer("pathdb/commit/time", nil)
	commitNodesMeter = metrics.NewRegisteredMeter("pathdb/commit/nodes", nil)
	commitBytesMeter = metrics.NewRegisteredMeter("pathdb/commit/bytes", nil)

	gcNodesMeter = metrics.NewRegisteredMeter("pathdb/gc/nodes", nil)
	gcBytesMeter = metrics.NewRegisteredMeter("pathdb/gc/bytes", nil)

	diffLayerBytesMeter = metrics.NewRegisteredMeter("pathdb/diff/bytes", nil)
	diffLayerNodesMeter = metrics.NewRegisteredMeter("pathdb/diff/nodes", nil)

	historyBuildTimeMeter  = metrics.NewRegisteredTimer("pathdb/history/time", nil)
	historyDataBytesMeter  = metrics.NewRegisteredMeter("pathdb/history/bytes/data", nil)
	historyIndexBytesMeter = metrics.NewRegisteredMeter("pathdb/history/bytes/index", nil)

	bloomIndexTimer    = metrics.NewRegisteredResettingTimer("pathdb/bloom/index", nil)
	bloomErrorGauge    = metrics.NewRegisteredGaugeFloat64("pathdb/bloom/error", nil)
	capBloomIndexTimer = metrics.NewRegisteredResettingTimer("pathdb/cap/bloom/index", nil)

	bloomAccountTrueHitMeter  = metrics.NewRegisteredMeter("pathdb/bloom/account/truehit", nil)
	bloomAccountFalseHitMeter = metrics.NewRegisteredMeter("pathdb/bloom/account/falsehit", nil)
	bloomAccountMissMeter     = metrics.NewRegisteredMeter("pathdb/bloom/account/miss", nil)

	bloomStorageTrueHitMeter  = metrics.NewRegisteredMeter("pathdb/bloom/storage/truehit", nil)
	bloomStorageFalseHitMeter = metrics.NewRegisteredMeter("pathdb/bloom/storage/falsehit", nil)
	bloomStorageMissMeter     = metrics.NewRegisteredMeter("pathdb/bloom/storage/miss", nil)
)
