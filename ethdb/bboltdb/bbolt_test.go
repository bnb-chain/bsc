package bboltdb

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/dbtest"
	"go.etcd.io/bbolt"
)

func TestBoltDB(t *testing.T) {
	t.Run("DatabaseSuite", func(t *testing.T) {
		dbtest.TestDatabaseSuite(t, func() ethdb.KeyValueStore {
			options := &bbolt.Options{Timeout: 0}
			db1, err := bbolt.Open("bbolt.db", 0600, options)
			if err != nil {
				t.Fatalf("failed to open bbolt database: %v", err)
			}

			// Create the default bucket if it does not exist
			err = db1.Update(func(tx *bbolt.Tx) error {
				_, err := tx.CreateBucketIfNotExists([]byte("ethdb"))
				return err
			})
			if err != nil {
				db1.Close()
				panic(fmt.Errorf("failed to create default bucket: %v", err))
			}

			return &Database{
				db: db1,
			}
		})
	})
}
