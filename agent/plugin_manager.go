// Manages execution of plugins
package main

import (
	"ghost/agent/client"
	"os"
	"time"

	ps "github.com/mitchellh/go-ps"
)

// PluginManager enforces plugin execution policy
func PluginManager(client *client.Client) {
	//this will help us determine if an already running plugin is currently managed, or was managed by a previously running instance
	currentManager := os.Getpid()

	// loop forever checking on plugins
	for {
		// process each plugin in the configuration
		for _, plugin := range client.Config.Plugins {

			// get stored plugin history from database
			p, err := client.LocalDb.PluginSelectUUID(plugin.UUID)
			if err != nil {
				client.Log.Error("error retreiving plugin information from local database: %v", err)
				continue
			}
			//set the plugin's pid up so IsRunning can work in case we are resuming
			plugin.ProcessID = p.ProcessID

			// flag for launching plugin
			launchPlugin := false

			//flag for resuming plugin management
			resumeManaging := false

			// process oneshot plugins
			if plugin.Mode == "oneshot" {

				// check plugin status
				if p.Status == "" {
					// no indicates the plugin has never been launched
					launchPlugin = true

				} else if p.Status == "error" {
					// if errored, check for retry flag
					if plugin.RetryFailure {
						launchPlugin = true
					} else {
						continue
					}
				}
			}

			// process persistent plugins
			if plugin.Mode == "persistent" {
				if isRunning, err := plugin.IsRunning(client); err != nil {
					client.Log.Error("%v", err)
					continue
				} else if !isRunning {
					launchPlugin = true
				} else if p.CurrentManager != currentManager { //the plugin is running but is not managed by this instance
					resumeManaging = true
				}
			}

			// process periodic plugins
			if plugin.Mode == "periodic" {

				// check if pocess is running
				if isRunning, err := plugin.IsRunning(client); err != nil {
					client.Log.Error("%v", err)
					continue
				} else if !isRunning {
					// check if enough time has elasped since last exit
					if time.Now().UTC().After(p.LastExit.Add(time.Second * time.Duration(plugin.LaunchFrequency))) {
						launchPlugin = true
					}

				} else if p.CurrentManager != currentManager { //the plugin is running but is not managed by this instance
					resumeManaging = true
				}
			}

			// launch plugin if needed
			if launchPlugin {
				//launch plugin in new goroutine
				client.Log.Info("Launching plugin %v(%v)", plugin.Name, plugin.UUID)
				ch := make(chan int, 1)
				go plugin.LaunchBinary(ch, client, currentManager)
				<-ch // block until process has been launched
			} else if resumeManaging {
				//new go routine will find plugin PID and resume throttling it
				client.Log.Info("Resuming plugin throttling for %v(%v)", plugin.Name, plugin.UUID)
				ch := make(chan int, 1)
				go plugin.ResumePlugin(ch, client, currentManager)
				<-ch // block until process has been properly resumed
			}

			// Remove running plugins not found in current config
			// Get all running plugins from local database
			runningPlugins, err := client.LocalDb.PluginSelectStatus("running")
			if err != nil {
				client.Log.Error("unable to read keystore: %v", err)
				continue
			}

			// Check if UUIDs are present in current configuration
			for _, runningPlugin := range runningPlugins {
				found := false
				for _, plugin := range client.Config.Plugins {
					if plugin.UUID == runningPlugin.UUID {
						found = true
					}
				}

				// remove unfound plugins
				if !found {
					// Kill any running processes
					if runningPlugin.ProcessID != 0 {
						// get process
						proc, _ := ps.FindProcess(p.ProcessID)
						// if a process was returned ... kill it
						if proc != nil {
							// but only if process name matches the one stored in the database
							if proc.Executable() == p.ProcessName {
								// get real process using the os package
								if process, err := os.FindProcess(proc.Pid()); err == nil {
									// kill it -- errors are ignored
									process.Kill()
								}
							}
						}
					}

					// Update status
					runningPlugin.Status = "complete"
					runningPlugin.StatusMessage = "removed from configuration"
					client.LocalDb.PluginInsert(runningPlugin)
					continue
				}
			}
		}

		// sleep
		time.Sleep(time.Second * 3)
	}
}
