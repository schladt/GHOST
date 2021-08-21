//Manages checkin process
package main

import (
	"encoding/json"
	"fmt"
	"ghost/agent/client"
	"io/ioutil"
	"math/rand"
	"os"
	"runtime/debug"
	"strings"
	"time"
)

// CheckinManager checks for updates from the client
func CheckinManager(client *client.Client) {

	for {
		// house keeping first
		// clean up and set polltime jitter
		debug.FreeOSMemory()
		rand.Seed(time.Now().UnixNano())
		client.PollTime = (time.Second * time.Duration(client.Config.PollTime)) + (time.Millisecond * time.Duration(rand.Intn(1000)))

		// send basic get request
		resp, err := client.Sender.Get(fmt.Sprintf("/core/hello/%s/", client.UUID))
		if err != nil {
			client.Log.Error("Error sending check-in message (1): %s", err)
			// attempt different controller & proxy combinations
			client.Sender.UpdateConnection(client.Config.ProxyList, client.Config.ControllerList)
			time.Sleep(client.PollTime)
			continue
		}

		// log return message (debug only)
		client.Log.Debug("Check-in reply from server %v: %v", client.Version, resp)

		// parse response
		var respMap map[string]string
		err = json.Unmarshal([]byte(resp), &respMap)
		if err != nil {
			client.Log.Error("Unable to parse JSON from controller: %s", err)
			time.Sleep(client.PollTime)
			continue
		}

		// Check for new configuration file
		if reqConfig, ok := respMap["required_config"]; ok {
			if !strings.EqualFold(client.ConfigHash, reqConfig) {
				client.Log.Info("New client configuration required. Have: %s -> Need: %s", client.ConfigHash, reqConfig)

				// Get new configuration file
				configBytes, err := client.Sender.GetResource(reqConfig)
				if err != nil {
					client.Log.Error("Unable to get new configuration file: %s", err)
					time.Sleep(client.PollTime)
					continue
				}

				// Overwrite configuration file on disk
				if err := ioutil.WriteFile(client.ConfigPath, configBytes, 0644); err != nil {
					client.Log.Error("Unable to write new configuration file to disk: %s", err)
					time.Sleep(client.PollTime)
					continue
				}

				// Exit client
				// Nanny is responsible for restarting client
				client.Log.Info("Client configuration has been updated. Going for shut down...")
				os.Exit(0)
			}
		}

		//sleep
		time.Sleep(client.PollTime)
	}
}
