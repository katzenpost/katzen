package main

import (
	"net"
	"os"

	"gioui.org/app"
	"github.com/katzenpost/katzenpost/client"
	"github.com/katzenpost/katzenpost/client/config"
	"path/filepath"
)

// checks to see if the local system has a listener on port 9050
func hasTor() bool {
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

	var cfg *config.Config
	if len(*clientConfigFile) != 0 {
		cfg, err = config.LoadFile(*clientConfigFile)
		if err != nil {
			result <- err
			return
		}
	} else {
		// detect running Tor and use configuration
		if _, ok := a.Settings["UseTor"]; ok {
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

	result <- c
}
