package parlia

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Reproduit l'etat du snapshot Parlia juste apres le bloc d'epoch 200 sur
// une chaine Coinbosa (Bohr INACTIF, TurnLength=1, epoch=200) selon la trace
// de Snapshot.apply() :
//   - a chaque bloc, `delete(snap.Recents, number - (minerHistoryCheckLen()+1))`
//     avec 1 validateur => minerHistoryCheckLen()=0 => Recents ne contient
//     jamais plus que le bloc courant.
//   - au bloc 200 (number%200 == minerHistoryCheckLen() == 0) le set bascule.
//     Bohr etant inactif, BEP-404 (purge des Recents) NE s'applique PAS ; la
//     branche `else` ne supprime rien car newLimit(2) >= oldLimit(1).
//   - Recents = {200: V1}
func snapAfterEpochSwitch(vals []common.Address, sealerOfBlock200 common.Address) *Snapshot {
	m := make(map[common.Address]*ValidatorInfo, len(vals))
	for i, v := range vals {
		m[v] = &ValidatorInfo{Index: i + 1}
	}
	return &Snapshot{
		Number:      200,
		EpochLength: 200,
		TurnLength:  1,
		Validators:  m,
		Recents:     map[uint64]common.Address{200: sealerOfBlock200},
	}
}

func TestCoinbosaAddSecondValidatorHaltsChain(t *testing.T) {
	v1 := common.HexToAddress("0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50") // INITIAL_VALIDATOR (seule cle detenue)
	v2 := common.HexToAddress("0x0000000000000000000000000000000000009999") // ajoute, aucune cle en ligne

	// --- cas actuel de production : 1 seul validateur ---
	s1 := snapAfterEpochSwitch([]common.Address{v1}, v1)
	fmt.Printf("N=1  minerHistoryCheckLen=%d  SignRecently(V1)=%v\n",
		s1.minerHistoryCheckLen(), s1.SignRecently(v1))
	if s1.SignRecently(v1) {
		t.Fatal("N=1 : le validateur unique devrait pouvoir sceller le bloc 201")
	}

	// --- apres updateValidatorSet([V1, V2]) : la garde sealerPresent est satisfaite ---
	s2 := snapAfterEpochSwitch([]common.Address{v1, v2}, v1)
	fmt.Printf("N=2  minerHistoryCheckLen=%d  SignRecently(V1)=%v  SignRecently(V2)=%v  inturn(201)=%s\n",
		s2.minerHistoryCheckLen(), s2.SignRecently(v1), s2.SignRecently(v2),
		s2.inturnValidator().Hex())
	if !s2.SignRecently(v1) {
		t.Fatal("attendu : V1 bloque par errRecentlySigned au bloc 201")
	}
	// V2 pourrait signer... mais personne ne detient sa cle => plus aucun bloc.
	fmt.Println("=> bloc 201 : V1 refuse de sceller (Seal: \"Signed recently, must wait for others\"),")
	fmt.Println("   V2 n'a pas de noeud => arret de la chaine.")
}
