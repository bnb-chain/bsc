// Copyright 2026 The go-ethereum Authors
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
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types/bal"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// systemTxStateDB collects the BALs finalized by Parlia without changing the
// consensus engine interface. Pending protocol mutations share the next system
// transaction's scope, so their combined net changes are recorded together.
type systemTxStateDB struct {
	vm.StateDB
	accessList *bal.ConstructionBlockAccessList
}

func (s *systemTxStateDB) Finalise(rules params.Rules) *bal.ConstructionBlockAccessList {
	list := s.StateDB.Finalise(rules)
	s.accessList.Merge(list)
	return list
}

// finaliseProtocolChanges records only pending mutations. Prepare runs after
// the protocol operations so their read-only accesses, including any omitted
// system transaction attempt, do not enter the BAL.
func finaliseProtocolChanges(statedb vm.StateDB, rules params.Rules, index uint32) *bal.ConstructionBlockAccessList {
	statedb.Prepare(rules, common.Address{}, common.Address{}, nil, nil, nil)
	statedb.SetTxContext(common.Hash{}, max(0, int(index)-1), index)
	return statedb.Finalise(rules)
}
