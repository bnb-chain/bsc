package parlia

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// SUJET C — que se passe-t-il si pobsTime est franchi AVANT que le contrat
// d'enjeu ne soit deploye, ou avant qu'il ne soit amorce ?
//
// Cas 1 : AUCUN CODE a l'adresse cible.
//   eth_call sur un compte sans code renvoie 0 octet, SANS erreur. C'est le
//   decodage ABI qui echoue. On mesure ici le message exact.
func TestPobsPrematureForkNoContractCode(t *testing.T) {
	vABI, err := abi.JSON(strings.NewReader(validatorSetABI))
	if err != nil {
		t.Fatal(err)
	}
	var valSet []common.Address
	var voteAddrSet []types.BLSPublicKey
	// retour d'un eth_call vers une adresse sans code : donnee vide
	err = vABI.UnpackIntoInterface(&[]interface{}{&valSet, &voteAddrSet}, "getMiningValidators", []byte{})
	if err == nil {
		t.Fatal("attendu : le decodage d'un retour vide ECHOUE")
	}
	t.Logf("contrat absent -> getCurrentValidators renvoie l'erreur : %q", err.Error())
	t.Log("  chemin production   : prepareValidators -> SetExtraData -> Prepare echoue -> AUCUN bloc d'epoch produit")
	t.Log("  chemin verification : verifyValidators -> Finalize echoue -> bloc d'epoch REJETE")
}

// Cas 2 : contrat deploye mais ENSEMBLE VIDE (jamais amorce).
//   Le decodage reussit, aucune erreur nulle part. C'est l'entete du bloc
//   d'epoch qui devient invalide : num == 0 -> getValidatorBytesFromHeader
//   renvoie nil -> verifyHeader renvoie errInvalidSpanValidators.
func TestPobsPrematureForkEmptyValidatorSet(t *testing.T) {
	vABI, err := abi.JSON(strings.NewReader(validatorSetABI))
	if err != nil {
		t.Fatal(err)
	}
	m := vABI.Methods["getMiningValidators"]
	raw, err := m.Outputs.Pack([]common.Address{}, [][]byte{})
	if err != nil {
		t.Fatal(err)
	}
	var valSet []common.Address
	var voteAddrSet []types.BLSPublicKey
	if err := vABI.UnpackIntoInterface(&[]interface{}{&valSet, &voteAddrSet}, "getMiningValidators", raw); err != nil {
		t.Fatalf("attendu : un ensemble vide se decode SANS erreur, obtenu %v", err)
	}
	t.Logf("ensemble vide : decodage SANS erreur, len(valSet)=%d — rien ne signale le probleme ici", len(valSet))

	// L'entete d'epoch construite a partir de cet ensemble vide.
	cfg := &params.ChainConfig{
		ChainID:     big.NewInt(26262),
		LondonBlock: big.NewInt(0),
		LubanBlock:  big.NewInt(0),
		Parlia:      &params.ParliaConfig{},
	}
	extra := make([]byte, 0, extraVanity+1+extraSeal)
	extra = append(extra, make([]byte, extraVanity)...) // vanity
	extra = append(extra, byte(0))                      // compteur de validateurs = 0
	extra = append(extra, make([]byte, extraSeal)...)   // sceau
	h := &types.Header{Number: big.NewInt(200), Time: 1800000000, Extra: extra}

	got := getValidatorBytesFromHeader(h, cfg, 200)
	if got != nil {
		t.Fatalf("attendu nil pour un compteur de validateurs nul, obtenu %d octets", len(got))
	}
	t.Log("getValidatorBytesFromHeader(compteur=0) -> nil")
	t.Log("  verifyHeader (parlia.go:622) : isEpoch && len(signersBytes)==0 -> errInvalidSpanValidators")
	t.Logf("  soit exactement : %v", errInvalidSpanValidators)
	t.Log("  => le bloc d'epoch est refuse par TOUS les noeuds, y compris son propre producteur.")
}

// Cas 3 : rappel chiffre du piege 1 -> N, avec la valeur exacte du quorum
// exige par Parlia pour chaque taille d'ensemble.
func TestPobsQuorumTableForEachSetSize(t *testing.T) {
	for n := 1; n <= 12; n++ {
		vals := make([]common.Address, n)
		for i := range vals {
			vals[i] = common.BigToAddress(big.NewInt(int64(i + 1)))
		}
		s := snapAfterEpochSwitch(vals, vals[0])
		besoin := s.minerHistoryCheckLen() + 1
		t.Logf("N=%2d  minerHistoryCheckLen=%d  scelleurs DISTINCTS et EN LIGNE exiges=%d", n, s.minerHistoryCheckLen(), besoin)
		if besoin != uint64(n/2+1) {
			t.Fatalf("N=%d : quorum mesure %d, attendu %d", n, besoin, n/2+1)
		}
	}
}
