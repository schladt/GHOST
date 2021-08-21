package client

import (
	"runtime"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/process"
)

// Throttle struct to store information need to throttle a process
type Throttle struct {
	TargetCpu     uint64
	Process       process.Process
	NumCPU        int
	sleepDuration time.Duration
}

// LowerProcessPriority lowers the priority of the target process
// INPUT: pid (int) process identifier
// OUTPUT: error
func LowerProcessPriority(pid int) error {
	//lower priority of the plugin process
	return syscall.Setpriority(syscall.PRIO_PROCESS, pid, 5)
}

// MonitorCpu method used to monitor CPU usage and call ThrottleCpu
func MonitorCpu(quit chan int, pid int, cpuLimit uint64) error {

	// create throttle object
	tt := Throttle{
		Process:   process.Process{Pid: int32(pid)},
		NumCPU:    runtime.NumCPU(),
		TargetCpu: cpuLimit,
	}

	// start throttle
	for {
		select {
		case <-quit:
			return nil
		default:
			tt.ThrottleCpu()
			time.Sleep(time.Millisecond * 200)
		}
	}
}

// MonitorCpu Method used to monitor CPU usage and call ThrottleCpu
func (t *Throttle) MonitorCpu(quit chan int) {

	for {
		select {
		case <-quit:
			return
		default:
			t.ThrottleCpu()
			time.Sleep(time.Millisecond * 200)
		}
	}
}

// ThrottleCpu Method used to throttle CPU
func (t *Throttle) ThrottleCpu() error {

	//set NumCPU if needed
	if t.NumCPU == 0 {
		t.NumCPU = runtime.NumCPU()
	}

	//set inital value for sleep ratio if needed
	if t.sleepDuration == time.Millisecond*0 {
		t.sleepDuration = time.Millisecond * 1
	}

	//get cpu percent
	percent, err := t.Process.Percent(time.Duration(0))
	if err != nil {
		return err
	}

	//calculate new sleep duration
	currentCpu := percent / float64(t.NumCPU)
	if currentCpu == 0.0 {
		currentCpu = 1.0
	}

	//buffer CPU
	currentCpu = 1.2 * currentCpu

	//calculate ration and sleep duration
	ratio := currentCpu / float64(t.TargetCpu)
	t.sleepDuration = time.Duration((float64(t.sleepDuration + time.Millisecond)) * ratio)

	//suspend process
	if err := t.Process.Suspend(); err != nil {
		return err
	}
	time.Sleep(t.sleepDuration)
	if err := t.Process.Resume(); err != nil {
		return err
	}

	return nil
}

// Resumes a process
// This is needed in the event agent exits leaving a plugin running, but the process suspended
func ResumeProcess(pid int) error {
	p := process.Process{Pid: int32(pid)}
	return p.Resume()
}
