package client

import (
	"errors"
	"ghost/agent/w32ex"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// struct to store various time trackers
type Throttle struct {
	TargetCpu     float64
	sleepDuration time.Duration
	prevProcTime  float64
	prevTickCount uint32
}

// MonitorCpu method used to monitor CPU usage and call ThrottleCpu
func MonitorCpu(quit chan int, pid int, cpuLimit uint64) error {
	// get process handle
	handle, err := syscall.OpenProcess(uint32(0x1F0FFF), false, uint32(pid))
	if err != nil {
		return err
	}

	// create throttle object
	tt := Throttle{TargetCpu: float64(cpuLimit), sleepDuration: (time.Millisecond * 10)}

	// start throttle
	for {
		select {
		case <-quit:
			return nil
		default:
			ThrottleCpu(handle, &tt)
			time.Sleep(time.Millisecond * 200)
		}
	}
}

// Method used to throttle CPU while talking the directory
func ThrottleCpu(handle syscall.Handle, tt *Throttle) error {

	//get process times
	var u syscall.Rusage
	err := syscall.GetProcessTimes(handle, &u.CreationTime, &u.ExitTime, &u.KernelTime, &u.UserTime)
	if err != nil {
		return err
	}

	//convert from filetime to system time
	var kernelTime syscall.Systemtime
	var userTime syscall.Systemtime

	libkernel32, _ := syscall.LoadLibrary("kernel32.dll")
	fileTimeToSystemTime, _ := syscall.GetProcAddress(libkernel32, "FileTimeToSystemTime")
	ret, _, _ := syscall.Syscall(fileTimeToSystemTime, 2,
		uintptr(unsafe.Pointer(&u.UserTime)),
		uintptr(unsafe.Pointer(&userTime)),
		0)

	if ret == 0 {
		return errors.New("unable to call FileTimeToSystemTime (1)")
	}

	ret, _, _ = syscall.Syscall(fileTimeToSystemTime, 2,
		uintptr(unsafe.Pointer(&u.KernelTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		0)

	if ret == 0 {
		return errors.New("unable to call FileTimeToSystemTime (2)")
	}

	//add kernel and user times
	currProcTime := float64((float64(kernelTime.Hour) * 3600.0 * 1000.0) +
		(float64(kernelTime.Minute) * 60.0 * 1000.0) +
		(float64(kernelTime.Second) * 1000.0) +
		float64(kernelTime.Milliseconds) +
		(float64(userTime.Hour) * 3600.0 * 1000.0) +
		(float64(userTime.Minute) * 60.0 * 1000.0) +
		(float64(userTime.Second) * 1000.0) +
		float64(userTime.Milliseconds))

	//get the tick count
	user32 := syscall.MustLoadDLL("kernel32.dll")
	getTickCount := user32.MustFindProc("GetTickCount")
	t, _, _ := getTickCount.Call()
	currTickCount := uint32(t)

	var cpuPercent float64
	if tt.prevProcTime != 0 {
		//calculate CPU usage
		cpuPercent = ((currProcTime - tt.prevProcTime) / float64(currTickCount-tt.prevTickCount) * 100) / float64(runtime.NumCPU())
		//buffer CPU
		cpuPercent = 1.2 * cpuPercent

		//calculate new sleep times
		ratio := (cpuPercent) / tt.TargetCpu
		tt.sleepDuration = time.Duration((float64(tt.sleepDuration + time.Millisecond)) * ratio)
	}

	//update procTime and TickCount
	tt.prevProcTime = currProcTime
	tt.prevTickCount = currTickCount

	//suspend process
	w32ex.NtSuspendProcess(handle)
	time.Sleep(tt.sleepDuration)
	w32ex.NtResumeProcess(handle)

	return nil
}

// LowerProcessPriorty lowers process priority
// INPUT: pid (int) process identifier
// OUTPUT: error
func LowerProcessPriority(pid int) error {
	// get process handle
	hProcess, err := syscall.OpenProcess(uint32(0x1F0FFF), false, uint32(pid))
	if err != nil {
		return err
	}

	// lower Plugin Priority
	libkernel32, _ := syscall.LoadLibrary("kernel32.dll")
	setPriorityClass, _ := syscall.GetProcAddress(libkernel32, "SetPriorityClass")
	ret, _, _ := syscall.Syscall(setPriorityClass, 2, uintptr(hProcess), uintptr(0x00004000), 0)
	if ret != 1 {
		return errors.New("unable to lower plugin process priority")
	}

	return nil
}

// Resumes a process
// This is needed in the event agent exits leaving a plugin running, but the process suspended
func ResumeProcess(pid int) error {
	hProcess, err := syscall.OpenProcess(uint32(0x1F0FFF), false, uint32(pid))
	if err != nil {
		return err
	}
	w32ex.NtResumeProcess(hProcess)
	return nil
}
