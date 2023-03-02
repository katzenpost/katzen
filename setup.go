package main

import (
	"net"
	"os"
	"path/filepath"

	"gioui.org/app"
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
	// obtain the default data location
	dir, err := app.DataDir()
	if err != nil {
		result <- err
		return
	}

	// dir does not appear to point to ~/.config/katzen but rather ~/.config on linux?
	// create directory for application data
	datadir := filepath.Join(dir, dataDirName)
	_, err = os.Stat(datadir)
	if os.IsNotExist(err) {
		// create the application data directory
		err := os.Mkdir(datadir, os.ModeDir|os.FileMode(0700))
		if err != nil {
			result <- err
			return
		}
	}

	// expand passphrase
	key := argon2.Key(passphrase, nil, 3, 32*1024, 4, keySize)
	db, err := badger.Open(badger.DefaultOptions(datadir).WithIndexCacheSize(10 << 20).WithEncryptionKey(key).WithSyncWrites(true))
	if err != nil {
		result <- err
		return
	}
	a.db = db

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
