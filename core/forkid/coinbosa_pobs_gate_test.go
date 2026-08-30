package forkid

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// Charge la config REELLE du genesis de production Coinbosa.
func coinbosaConfig(t *testing.T) *params.ChainConfig {
	t.Helper()
	raw, err := os.ReadFile("../../coinbosa/genesis/genesis-coinbosa.json")
	if err != nil {
		t.Fatalf("genesis illisible: %v", err)
	}
	var g struct {
		Config *params.ChainConfig `json:"config"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("genesis non parsable: %v", err)
	}
	return g.Config
}

// 1) La config actuelle passe CheckConfigForkOrder.
// 2) Ajouter une porte temporelle DANS la liste ordonnee la fait ECHOUER,
//    parce que toutes les portes intermediaires (feynman..bohr) sont nil.
//    PascalTime sert ici de doublure exacte de ce que serait un pobsTime
//    insere dans cette liste : meme type (*uint64), meme traitement.
func TestCoinbosaForkOrderRefusesNewTimestampGate(t *testing.T) {
	cfg := coinbosaConfig(t)
	if cfg.Parlia == nil {
		t.Fatal("parlia absent : CheckConfigForkOrder serait ignore")
	}
	if err := cfg.CheckConfigForkOrder(); err != nil {
		t.Fatalf("config actuelle refusee : %v", err)
	}
	t.Log("config actuelle : CheckConfigForkOrder OK")

	future := uint64(1800000000)
	cfg.PascalTime = &future
	err := cfg.CheckConfigForkOrder()
	if err == nil {
		t.Fatal("attendu : une porte temporelle ajoutee dans la liste ordonnee est REFUSEE")
	}
	t.Logf("porte ajoutee dans la liste ordonnee -> le noeud REFUSE de demarrer : %v", err)
}

// La porte pobsTime change le forkid. On mesure ce que devient l'appairage
// p2p entre un noeud ANCIEN (sans la porte) et un noeud NOUVEAU (avec).
func TestCoinbosaPobsGateP2PCompat(t *testing.T) {
	old := coinbosaConfig(t)
	nouveau := coinbosaConfig(t)
	T := uint64(1800000000)
	nouveau.PascalTime = &T // doublure de pobsTime

	gen := types.NewBlockWithHeader(&types.Header{
		Number:     big.NewInt(0),
		Time:       0,
		Difficulty: big.NewInt(1),
	})

	avant := uint64(1700000000) // avant la porte
	apres := uint64(1800000500) // apres la porte

	head := func(ts *uint64) func() (uint64, uint64) {
		return func() (uint64, uint64) { return 400000, *ts }
	}

	idOld := func(ts uint64) ID { return NewID(old, gen, 400000, ts) }
	idNew := func(ts uint64) ID { return NewID(nouveau, gen, 400000, ts) }

	t.Logf("forkid ANCIEN  avant=%x next=%d", idOld(avant).Hash, idOld(avant).Next)
	t.Logf("forkid NOUVEAU avant=%x next=%d", idNew(avant).Hash, idNew(avant).Next)
	t.Logf("forkid ANCIEN  apres=%x next=%d", idOld(apres).Hash, idOld(apres).Next)
	t.Logf("forkid NOUVEAU apres=%x next=%d", idNew(apres).Hash, idNew(apres).Next)

	tsA, tsB := avant, avant
	filtreOldAvant := newFilter(old, gen, head(&tsA))
	filtreNewAvant := newFilter(nouveau, gen, head(&tsB))

	if err := filtreOldAvant(idNew(avant)); err != nil {
		t.Fatalf("AVANT la porte : l'ancien REJETTE le nouveau : %v", err)
	}
	if err := filtreNewAvant(idOld(avant)); err != nil {
		t.Fatalf("AVANT la porte : le nouveau REJETTE l'ancien : %v", err)
	}
	t.Log("AVANT la porte : ancien <-> nouveau s'appairent (fenetre de deploiement sure)")

	tsC, tsD := apres, apres
	filtreOldApres := newFilter(old, gen, head(&tsC))
	filtreNewApres := newFilter(nouveau, gen, head(&tsD))

	errON := filtreOldApres(idNew(apres))
	errNO := filtreNewApres(idOld(apres))
	t.Logf("APRES la porte : ancien juge le nouveau -> %v", errON)
	t.Logf("APRES la porte : nouveau juge l'ancien -> %v", errNO)
	if errON == nil && errNO == nil {
		t.Fatal("attendu : au moins un sens de l'appairage est refuse apres la porte")
	}
}
