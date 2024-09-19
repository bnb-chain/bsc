package fakebeacon

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

//
//func TestFetchBlockNumberByTime(t *testing.T) {
//	blockNum, err := fetchBlockNumberByTime(context.Background(), 1724052941, client)
//	assert.Nil(t, err)
//	assert.Equal(t, uint64(41493946), blockNum)
//
//	blockNum, err = fetchBlockNumberByTime(context.Background(), 1734052941, client)
//	assert.Equal(t, err, errors.New("time too large"))
//
//	blockNum, err = fetchBlockNumberByTime(context.Background(), 1600153618, client)
//	assert.Nil(t, err)
//	assert.Equal(t, uint64(493946), blockNum)
//}
//
//func TestBeaconBlobSidecars(t *testing.T) {
//	indexBlobHash := []IndexedBlobHash{
//		{Hash: common.HexToHash("0x01231952ecbaede62f8d0398b656072c072db36982c9ef106fbbc39ce14f983c"), Index: 0},
//		{Hash: common.HexToHash("0x012c21a8284d2d707bb5318e874d2e1b97a53d028e96abb702b284a2cbb0f79c"), Index: 1},
//		{Hash: common.HexToHash("0x011196c8d02536ede0382aa6e9fdba6c460169c0711b5f97fcd701bd8997aee3"), Index: 2},
//		{Hash: common.HexToHash("0x019c86b46b27401fb978fd175d1eb7dadf4976d6919501b0c5280d13a5bab57b"), Index: 3},
//		{Hash: common.HexToHash("0x01e00db7ee99176b3fd50aab45b4fae953292334bbf013707aac58c455d98596"), Index: 4},
//		{Hash: common.HexToHash("0x0117d23b68123d578a98b3e1aa029661e0abda821a98444c21992eb1e5b7208f"), Index: 5},
//		//{Hash: common.HexToHash("0x01e00db7ee99176b3fd50aab45b4fae953292334bbf013707aac58c455d98596"), Index: 1},
//	}
//
//	resp, err := beaconBlobSidecars(context.Background(), 1724055046, []int{0, 1, 2, 3, 4, 5}) // block: 41494647
//	assert.Nil(t, err)
//	assert.NotNil(t, resp)
//	assert.NotEmpty(t, resp.Data)
//	for i, sideCar := range resp.Data {
//		assert.Equal(t, indexBlobHash[i].Index, sideCar.Index)
//		assert.Equal(t, indexBlobHash[i].Hash, kZGToVersionedHash(sideCar.KZGCommitment))
//	}
//
//	apiscs := make([]*BlobSidecar, 0, len(indexBlobHash))
//	// filter and order by hashes
//	for _, h := range indexBlobHash {
//		for _, apisc := range resp.Data {
//			if h.Index == int(apisc.Index) {
//				apiscs = append(apiscs, apisc)
//				break
//			}
//		}
//	}
//
//	assert.Equal(t, len(apiscs), len(resp.Data))
//	assert.Equal(t, len(apiscs), len(indexBlobHash))
//}

type TimeToSlotFn func(timestamp uint64) (uint64, error)

// GetTimeToSlotFn returns a function that converts a timestamp to a slot number.
func GetTimeToSlotFn(ctx context.Context) (TimeToSlotFn, error) {
	genesis := beaconGenesis()
	config := configSpec()

	genesisTime, _ := strconv.ParseUint(genesis.Data.GenesisTime, 10, 64)
	secondsPerSlot, _ := strconv.ParseUint(config.SecondsPerSlot, 10, 64)
	if secondsPerSlot == 0 {
		return nil, fmt.Errorf("got bad value for seconds per slot: %v", config.SecondsPerSlot)
	}
	timeToSlotFn := func(timestamp uint64) (uint64, error) {
		if timestamp < genesisTime {
			return 0, fmt.Errorf("provided timestamp (%v) precedes genesis time (%v)", timestamp, genesisTime)
		}
		return (timestamp - genesisTime) / secondsPerSlot, nil
	}
	return timeToSlotFn, nil
}

func TestAPI(t *testing.T) {
	slotFn, err := GetTimeToSlotFn(context.Background())
	assert.Nil(t, err)

	expTx := uint64(123151345)
	gotTx, err := slotFn(expTx)
	assert.Nil(t, err)
	assert.Equal(t, expTx, gotTx)
}
