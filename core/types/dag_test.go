package types

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTxDAG(t *testing.T) {
	dag := mockSimpleDAG()
	t.Log(dag.String())
}

func mockSimpleDAG() *TxDAG {
	dag := NewTxDAG(10)
	dag.TxDeps[0].TxIndexes = []int{}
	dag.TxDeps[1].TxIndexes = []int{}
	dag.TxDeps[2].TxIndexes = []int{}
	dag.TxDeps[3].TxIndexes = []int{0}
	dag.TxDeps[4].TxIndexes = []int{0}
	dag.TxDeps[5].TxIndexes = []int{1, 2}
	dag.TxDeps[6].TxIndexes = []int{2, 5}
	dag.TxDeps[7].TxIndexes = []int{6}
	dag.TxDeps[8].TxIndexes = []int{}
	dag.TxDeps[9].TxIndexes = []int{8}
	return dag
}

func TestMVStates2TxDAG(t *testing.T) {
	ms := NewMVStates(10)

	ms.rwSets[0] = mockRWSet(0, []string{"0x00"}, []string{"0x00"})
	ms.rwSets[1] = mockRWSet(1, []string{"0x01"}, []string{"0x01"})
	ms.rwSets[2] = mockRWSet(2, []string{"0x02"}, []string{"0x02"})
	ms.rwSets[3] = mockRWSet(3, []string{"0x00", "0x03"}, []string{"0x03"})
	ms.rwSets[4] = mockRWSet(4, []string{"0x00", "0x04"}, []string{"0x04"})
	ms.rwSets[5] = mockRWSet(5, []string{"0x01", "0x02", "0x05"}, []string{"0x05"})
	ms.rwSets[6] = mockRWSet(6, []string{"0x02", "0x05", "0x06"}, []string{"0x06"})
	ms.rwSets[7] = mockRWSet(7, []string{"0x06", "0x07"}, []string{"0x07"})
	ms.rwSets[8] = mockRWSet(8, []string{"0x08"}, []string{"0x08"})
	ms.rwSets[9] = mockRWSet(9, []string{"0x08", "0x09"}, []string{"0x09"})

	dag := ms.ResolveDAG()
	require.Equal(t, mockSimpleDAG(), dag)
	t.Log(dag.String())
}

func mockRWSet(index int, read []string, write []string) *RWSet {
	ver := StateVersion{
		TxIndex: index,
	}
	set := NewRWSet(ver)
	for _, k := range read {
		key := RWKey{}
		if len(k) > len(key) {
			k = k[:len(key)]
		}
		copy(key[:], k)
		set.readSet[key] = &ReadRecord{
			StateVersion: ver,
			Val:          struct{}{},
		}
	}
	for _, k := range write {
		key := RWKey{}
		if len(k) > len(key) {
			k = k[:len(key)]
		}
		copy(key[:], k)
		set.writeSet[key] = &WriteRecord{
			Val: struct{}{},
		}
	}

	return set
}
