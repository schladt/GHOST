package client

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	ps "github.com/mitchellh/go-ps"
)

// Plugin struct
type Plugin struct {
	Name             string         `yaml:"Name" json:"name"`
	Mode             string         `yaml:"Mode" json:"mode"`
	LaunchFrequency  int            `yaml:"LaunchFrequency" json:"launch_frequency"`
	UUID             string         `yaml:"UUID" json:"plugin_uuid"`
	WorkingDirectory string         `yaml:"WorkingDirectory" json:"working_directory"`
	Command          string         `yaml:"Command" json:"command"`
	Args             []string       `yaml:"Args" json:"args"`
	ResourceFiles    []ResourceFile `yaml:"ResourceFiles" json:"resource_files"`
	CPULimit         uint64         `yaml:"CPULimit" json:"cpu_limit"`
	RetryFailure     bool           `yaml:"RetryFailure" json:"retry_failure"`
	Status           string         `json:"status"`
	StatusMessage    string         `json:"status_message"`
	ProcessName      string         `json:"process_name"`
	ProcessID        int            `json:"process_id"`
	LastStart        time.Time      `json:"last_start"`
	LastExit         time.Time      `json:"last_exit"`
	CurrentManager   int            `json:"current_manager,omitempty"`
}

// ResourceFile struct
type ResourceFile struct {
	Path string `yaml:"Path" json:"path"`
	Hash string `yaml:"Hash" json:"hash"`
}

// SetError hepler method to set and report error status
func (p Plugin) SetError(client *Client, msg ...string) {
	exMsg := fmt.Sprintf("Plugin %v(%v): %v", p.Name, p.UUID, strings.Join(msg, ": "))
	client.Log.Error("%v", exMsg)
	p.Status = "error"
	p.StatusMessage = exMsg
	p.LastExit = time.Now().UTC()
	p.UpdateStatus(client)
}

// UpdateStatus Method updates plugin status with the control server
func (p Plugin) UpdateStatus(client *Client) error {
	if err := client.LocalDb.PluginInsert(p); err != nil {
		return err
	}

	// send log back to server via message Queue
	if !client.Offline {
		//make a copy and clear the current_manager so we don't send it in a pluginlog
		pluginToSend := p
		pluginToSend.CurrentManager = 0

		// marshall
		msgBytes, err := json.Marshal(pluginToSend)
		if err != nil {
			return err
		}

		if err := client.LocalDb.MessageQueueInsert(string(msgBytes), "/core/pluginlog/"); err != nil {
			return err
		}

	}
	return nil
}

// QueuePluginLog only sends a log message to the server (if online) and doesn't update the local database
func (p Plugin) QueuePluginLog(client *Client) error {
	if !client.Offline {
		//make a copy and clear the current_manager so we don't send it in a pluginlog
		pluginToSend := p
		pluginToSend.CurrentManager = 0

		// marshall
		msgBytes, err := json.Marshal(pluginToSend)
		if err != nil {
			return err
		}

		if err := client.LocalDb.MessageQueueInsert(string(msgBytes), "/core/pluginlog/"); err != nil {
			return err
		}
	}
	return nil
}

// IsRunning validates if the the current plugin is running
// The plugin struct only needs the UUID member set as this method check the key_store for other values
func (p Plugin) IsRunning(client *Client) (bool, error) {
	// get stored plugin from database
	storedPlugin, err := client.LocalDb.PluginSelectUUID(p.UUID)
	if err != nil {
		return false, err
	}

	// return false if status is not "running"
	if storedPlugin.Status != "running" {
		return false, nil
	}

	// validate plugin is actually running
	// return false if either process ID or Name returns blank
	if storedPlugin.ProcessID == 0 || storedPlugin.ProcessName == "" {
		return false, nil
	}

	// validate the process is actually running
	// return false if no process found matching the stored ID
	proc, err := ps.FindProcess(storedPlugin.ProcessID)
	if proc == nil || err != nil {
		return false, nil
	}

	// check if process name matches
	if proc.Executable() != storedPlugin.ProcessName {
		return false, nil
	}

	// made it past all checks; process is running!
	return true, nil
}

// VerifyHashes checks hashes for all resource files associated with a plugin
// Returns true if all resources files for a plugin are verify
func (p Plugin) VerifyHashes(client *Client) bool {
	// get working directory
	wd := filepath.Join(client.InstallDir, p.WorkingDirectory)

	// create the working directory if it doesn't already exist
	if err := os.MkdirAll(wd, os.ModePerm); err != nil {
		client.Log.Error("Could not create plugin working directory: %s", err)
		return false
	}

	// process each resource file
	for _, resourceFile := range p.ResourceFiles {
		resourcePath := filepath.Join(wd, resourceFile.Path)
		hash, err := client.GetSHA256(resourcePath)
		if err != nil {
			client.Log.Error("Error getting resource hash %s, %v", resourcePath, err)
			// Don't return yet. (we may be able to download new file)
		}

		// check hash
		if strings.ToLower(hash) != strings.ToLower(resourceFile.Hash) {
			// if offline, return false
			if client.Offline {
				client.Log.Error("Mismatched hashes: name: %s wanted: %s got: %s", resourcePath, resourceFile.Hash, hash)
				return false
			}

			// attempt to get correct file from server
			client.Log.Info("Resource file %s hash on disk does not match configuration. Downloading update...", resourcePath)
			fileBytes, err := client.Sender.GetResource(strings.ToLower(resourceFile.Hash))
			if err != nil {
				client.Log.Error("Unable to retrieve new resource file: %s", err)
				return false
			}

			// create any subdirectories the resource file may need
			fileDir := filepath.Dir(resourcePath)
			if err := os.MkdirAll(fileDir, os.ModePerm); err != nil {
				client.Log.Error("Could not create subdirectory %s : %s", fileDir, err)
				return false
			}

			// write new file to disk
			if err := ioutil.WriteFile(resourcePath, fileBytes, 0755); err != nil {
				client.Log.Error("Unable to write resource file to disk: %s", err)
				return false
			}

			client.Log.Info("New client resource file written to disk.")

			// check hash of newly written file
			newHash, err := client.GetSHA256(resourcePath)
			if err != nil {
				client.Log.Error("Error getting resource hash %s, %v", resourcePath, err)
				return false
			}

			if strings.ToLower(newHash) != strings.ToLower(resourceFile.Hash) {
				client.Log.Error("Mismatched hashes: name: %s wanted: %s got: %s", resourcePath, resourceFile.Hash, newHash)
				return false
			}
		}

		client.Log.Debug("Resource file hash verified: %s %s", resourceFile.Path, hash)
	}

	// made it -- all file are verified
	return true
}

// LaunchBinary method launches a plugin binary
// Should be executed from seperate goroutine to prevent blocking
// INPUT ch is an channel used to indicate when the plugin has been launched
// client is client object passed by pointer
// manager is the PID of the current plugin manager.  It's needed for plugin management resuming
func (p Plugin) LaunchBinary(ch chan int, client *Client, manager int) {
	var err error
	//defer channgel send to ensure function won't block in case of error
	defer func() { ch <- 0 }()

	// verify hashes from configuration file
	if !client.Debug {
		if !p.VerifyHashes(client) {
			p.SetError(client, "unable to verify hashes")
			return
		}
	}

	// create process
	var cmd *exec.Cmd

	// add command and args
	cmd = exec.Command(p.Command, p.Args...)

	// set working directory of command
	cmd.Dir = filepath.Join(client.InstallDir, p.WorkingDirectory)

	//Uncomment these lines to log output
	//create output pipes
	stderr, _ := cmd.StderrPipe()
	// stdout, _ := cmd.StdoutPipe()

	// start process
	err = cmd.Start()
	if err != nil {
		p.SetError(client, "unable to start plugin", err.Error())
		return
	}

	// Get process information and update status
	p.Status = "running"
	p.StatusMessage = "running"
	p.ProcessID = cmd.Process.Pid
	proc, err := ps.FindProcess(cmd.Process.Pid)
	if err != nil {
		p.SetError(client, "unable to get plugin process information", err.Error())
		return
	}
	p.ProcessName = proc.Executable()
	p.LastStart = time.Now().UTC()
	p.CurrentManager = manager
	p.UpdateStatus(client)

	//start process
	client.Log.Info("Plugin launched with command %v", cmd.Args)

	// let channel know the process has started
	ch <- 0

	// lower priority of the plugin process
	if err := LowerProcessPriority(cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		p.SetError(client, err.Error())
		return
	}

	// throttle process
	quit := make(chan int)
	if p.CPULimit > 0 {
		go MonitorCpu(quit, cmd.Process.Pid, p.CPULimit)
	}

	// Uncomment these lines to log all output from plugin
	errMsg, _ := ioutil.ReadAll(stderr)
	// client.Log.Debug("Stderr: %s", errMsg)
	// slurp, _ := ioutil.ReadAll(stdout)
	// client.Log.Debug("Stdout: %s", slurp)

	// wait for process to exit

	err = cmd.Wait()

	// stop throttling by sending message to queue
	if p.CPULimit > 0 {
		quit <- 0
	}

	// check for errors and update status
	if err != nil {
		client.Log.Error("Plugin %s(%s) exited with errors: %v : %s", p.Name, p.UUID, err, errMsg)
		p.Status = "error"
		p.StatusMessage = err.Error() + " : " + string(errMsg)
	} else {
		client.Log.Info("Plugin %s(%s) exited successfully", p.Name, p.UUID)
		p.Status = "complete"
		p.StatusMessage = "complete"
	}
	p.LastExit = time.Now().UTC()
	p.ProcessID = 0 //clear it out for the next launch to work
	p.UpdateStatus(client)
}

// ResumePlugin takes back control over a plugin that was left running when agent exited and started again
// Should be executed from seperate goroutine to prevent blocking
// INPUT ch is an channel used to indicate when the plugin has been resumed
// client is client object passed by pointer
// manager is the PID of agent's current plugin manager.  It's needed for plugin management resuming

//This function resumes monitoring the plugin process until the process exits
//However, it can't tell if the plugin was successful or not since it no longer has
//access to the exec.Command structure
func (p Plugin) ResumePlugin(ch chan int, client *Client, manager int) {
	var err error
	//defer channgel send to ensure function won't block in case of error
	defer func() { ch <- 0 }()

	// Get process information and update status
	p.Status = "running"
	p.StatusMessage = "resuming control"
	p.CurrentManager = manager

	//assume p.ProcessID is already set since IsRunning had to have returned true to get here
	proc, err := ps.FindProcess(p.ProcessID)
	if err != nil {
		p.SetError(client, "unable to get plugin process information", err.Error())
		return
	}
	p.ProcessName = proc.Executable()
	p.UpdateStatus(client)

	//Just in case previously exited just after the process was suspended by the ThrottleCPU function
	//If we don't resume it here, Monitor never will for some reason
	ResumeProcess(p.ProcessID)

	// let channel know that monitoring as been resumed
	ch <- 0

	// throttle process
	quit := make(chan int)
	if p.CPULimit > 0 {
		go MonitorCpu(quit, p.ProcessID, p.CPULimit)
	}

	// wait for process to exit
	// Need to store the current PID because IsRunning will still return true if the plugin exits and then the plugin_manager restarts it
	// (as a new PID) In between checking IsRunning
	isRunning := false
	currentPID := p.ProcessID
	for {
		//poll the database everytime in case the plugin dies and gets relaunched
		if p, err = client.LocalDb.PluginSelectUUID(p.UUID); err != nil {
			client.Log.Error("%v", err)
			break
		}
		if isRunning, err = p.IsRunning(client); err != nil {
			client.Log.Error("%v", err)
			break
		}
		if isRunning && currentPID == p.ProcessID {
			time.Sleep(time.Second * 30) //check again in 30 seconds
		} else {
			break
		}
	}

	//Once we get here, the plugin with PID 'currentPID' is no longer running and we can stop monitoring it

	// stop throttling by sending message to queue
	if p.CPULimit > 0 {
		quit <- 0
	}

	//We can't mark this as complete because we don't know the status after we do a resume

	//plus if we change the status or PID, the change wouldn't be recorded until after the plugin_manager already restarted the plugin
	//causing it to be ran twice because IsRunning will return false when agent starts back up (if we mess with p.Status or p.ProcessID)

	p.StatusMessage = "exited after monitoring resumed"
	p.LastExit = time.Now().UTC()
	client.Log.Info("Just detected exit of previously resumed plugin %v(%v) PID %v", p.Name, p.UUID, currentPID)
	p.Status = "exited after monitoring resumed"
	p.QueuePluginLog(client)
}
