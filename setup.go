package main

import (
	"net"
	"os"

	"github.com/dgraph-io/badger/v4"
	"github.com/katzenpost/katzenpost/client"
	"github.com/katzenpost/katzenpost/client/config"
	"golang.org/x/crypto/argon2"
)

// checks to see if the local system has a listener on port 9050
func hasDefaultTor() bool {
	c, err := net.Dial("tcp", "127.0.0.1:9050")
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func setupClient(a *App, passphrase []byte, result chan interface{}) {
	_, err := os.Stat(*profilePath)
	if os.IsNotExist(err) {
		// create the application data directory
		err := os.MkdirAll(*profilePath, os.ModeDir|os.FileMode(0700))
		if err != nil {
			result <- err
			return
		}
	}

	// expand passphrase
	key := argon2.Key(passphrase, nil, 3, 32*1024, 4, keySize)
	db, err := badger.Open(badger.DefaultOptions(*profilePath).WithIndexCacheSize(10 << 20).WithEncryptionKey(key).WithSyncWrites(true))
	if err != nil {
		result <- err
		return
	}
	a.db = db

	// Create or update any db entries as necessary
	err = a.InitDB()
	if err != nil {
		result <- err
		return
	}

	// halt the db at app shutdown
	a.Go(func() {
		<-a.HaltCh()
		a.db.Close()
		a.db = nil
	})

	var cfg *config.Config
	if len(*clientConfigFile) != 0 {
		cfg, err = config.LoadFile(*clientConfigFile)
		if err != nil {
			result <- err
			return
		}
	} else {
		// detect running Tor and use configuration
		var useTor bool
		err := a.db.View(func(txn *badger.Txn) error {
			i, err := txn.Get([]byte("UseTor"))
			if err != nil {
				return err
			}
			return i.Value(func(val []byte) error {
				if val[0] == 0xFF {
					useTor = true
				}
				return nil
			})
		})
		// default to using Tor if Tor is available
		if err == badger.ErrKeyNotFound {
			if hasDefaultTor() {
				useTor = true
				err = a.db.Update(func(txn *badger.Txn) error {
					return txn.Set([]byte("UseTor"), []byte{0xFF})
				})
			} else {
				err = a.db.Update(func(txn *badger.Txn) error {
					return txn.Set([]byte("UseTor"), []byte{0x0})
				})
			}
			if err != nil {
				result <- err
				return
			}
		}
		if useTor {
			cfg, err = config.Load(cfgWithTor)
			if err != nil {
				result <- err
				return
			}
		} else {
			cfg, err = config.Load(cfgWithoutTor)
			if err != nil {
				result <- err
				return
			}
		}
	}

	// create a client
	c, err := client.New(cfg)
	if err != nil {
		result <- err
		return
	}

	// start connecting automatically, if enabled

	result <- c
}
