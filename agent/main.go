// Mike Schladt 2021

package main

import (
	"ghost/agent/client"
	"ghost/agent/logger"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jessevdk/go-flags"
	"gopkg.in/yaml.v2"
)

var version string

func main() {
	var err error

	// Parse commandline options
	var opts struct {
		Debug   bool `short:"d" long:"debug" description:"Debug mode (no file hash verification & offline mode)"`
		Offline bool `short:"o" long:"offline" description:"Run offline while still verifying file hashes"`
		Args    struct {
			ConfigFile string `description:"YAML formatted configuration file"`
		} `positional-args:"yes" required:"yes"`
	}

	if _, err := flags.Parse(&opts); err != nil {
		os.Exit(1)
	}

	// create client
	client := client.Client{}
	client.InstallName = filepath.Base(os.Args[0])
	client.InstallDir, err = filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatalf("Unable to create client object from configuration file: %v", err)
	}

	// set version passed in from compiler flags
	client.Version = version

	// Set offline and debug modes
	client.Debug = opts.Debug
	client.Offline = opts.Offline || opts.Debug

	// load config from file
	client.ConfigPath = opts.Args.ConfigFile
	rawConfig, err := ioutil.ReadFile(client.ConfigPath)
	if err != nil {
		panic(err.Error())
	}

	if err := yaml.Unmarshal(rawConfig, &client.Config); err != nil {
		panic(err.Error())
	}

	// create logger
	client.Log = logger.Logger{Filename: filepath.Join(client.InstallDir, "ghost.log")} //TODO: config file name

	// start heartbeat
	if !client.Debug && !opts.Offline {
		go client.Heartbeat()
	}

	// load client values
	client.Bootstrap()

	// check for binary updates
	if !client.Debug {
		if !client.VerifyBinary() {
			client.Log.Fatal("No suitable client binary found... want %s: have: %s", client.Config.BinaryHash, client.BinaryHash)
		}
	}

	// start client checkin and message managers
	if !client.Offline {
		go CheckinManager(&client)
		go MessageQueueManager(&client)
	}

	// start plugin manager
	go PluginManager(&client)

	// loop forever
	for {
		time.Sleep(time.Minute * 1)
	}
}
