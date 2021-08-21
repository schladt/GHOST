/* Contains selected Win32 APIs not found in other repos*/

package w32ex

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

const (
	ABOVE_NORMAL_PRIORITY_CLASS   = 0x00008000
	BELOW_NORMAL_PRIORITY_CLASS   = 0x00004000
	HIGH_PRIORITY_CLASS           = 0x00000080
	IDLE_PRIORITY_CLASS           = 0x00000040
	NORMAL_PRIORITY_CLASS         = 0x00000020
	PROCESS_MODE_BACKGROUND_BEGIN = 0x00100000
	PROCESS_MODE_BACKGROUND_END   = 0x00200000
	REALTIME_PRIORITY_CLASS       = 0x00000100
)

type FILETIME syscall.Filetime
type SYSTEMTIME syscall.Systemtime
type OSVERSIONINFO struct {
	DwOSVersionInfoSize int32
	DwMajorVersion      int32
	DwMinorVersion      int32
	DwBuildNumber       int32
	DwPlatformId        int32
	SzCSDVersion        [128]byte
}

// BOOL WINAPI SystemTimeToFileTime
//   _In_  const SYSTEMTIME *lpSystemTime,
//   _Out_       LPFILETIME lpFileTime
func SystemTimeToFileTime(inputSystemTime SYSTEMTIME) (FILETIME, error) {

	var outputFileTime FILETIME
	var err error

	libkernel32, _ := syscall.LoadLibrary("kernel32.dll")
	sytemTimeToFileTime, _ := syscall.GetProcAddress(libkernel32, "SystemTimeToFileTime")
	ret, _, _ := syscall.Syscall(sytemTimeToFileTime, 2,
		uintptr(unsafe.Pointer(&inputSystemTime)),
		uintptr(unsafe.Pointer(&outputFileTime)),
		0)

	if ret == 0 {
		err = errors.New("Unable to call SystemTimeToFileTime")
	}
	return outputFileTime, err
}

// BOOL WINAPI FileTimeToSystemTime
//   _In_  const FILETIME     *lpFileTime,
//   _Out_       LPSYSTEMTIME lpSystemTime
func FileTimeToSystemTime(inputFileTime FILETIME) (SYSTEMTIME, error) {

	var outputSystemTime SYSTEMTIME
	var err error

	libkernel32, _ := syscall.LoadLibrary("kernel32.dll")
	fileTimeToSystemTime, _ := syscall.GetProcAddress(libkernel32, "FileTimeToSystemTime")
	ret, _, _ := syscall.Syscall(fileTimeToSystemTime, 2,
		uintptr(unsafe.Pointer(&inputFileTime)),
		uintptr(unsafe.Pointer(&outputSystemTime)),
		0)

	if ret == 0 {
		err = errors.New("Unable to call FileTimeToSystemTime")
	}
	return outputSystemTime, err
}

// LONG WINAPI CompareFileTime
//   _In_ const FILETIME *lpFileTime1,
//   _In_ const FILETIME *lpFileTime2
func CompareFileTime(t1 FILETIME, t2 FILETIME) int {
	libkernel32, _ := syscall.LoadLibrary("kernel32.dll")
	fileTimeToSystemTime, _ := syscall.GetProcAddress(libkernel32, "CompareFileTime")
	ret, _, _ := syscall.Syscall(fileTimeToSystemTime, 2,
		uintptr(unsafe.Pointer(&t1)),
		uintptr(unsafe.Pointer(&t2)),
		0)

	return int(int16(ret))
}

// void WINAPI GetSystemTime
//   _Out_ LPSYSTEMTIME lpSystemTime
func GetSystemTime() SYSTEMTIME {
	var systemTime SYSTEMTIME
	libkernel32, _ := syscall.LoadLibrary("kernel32.dll")
	getSystemTime, _ := syscall.GetProcAddress(libkernel32, "GetSystemTime")
	_, _, _ = syscall.Syscall(getSystemTime, 1,
		uintptr(unsafe.Pointer(&systemTime)),
		0,
		0)

	return systemTime
}

// BOOL WINAPI GetVersionEx(
//   _Inout_ LPOSVERSIONINFO lpVersionInfo
func GetVersionEx(osversion *OSVERSIONINFO) bool {
	osversion.DwOSVersionInfoSize = int32(unsafe.Sizeof(*osversion))
	libkernel32, _ := syscall.LoadLibrary("kernel32.dll")
	getVersionExA, _ := syscall.GetProcAddress(libkernel32, "GetVersionExA")
	rt, _, _ := syscall.Syscall(getVersionExA, 1, uintptr(unsafe.Pointer(osversion)), 0, 0)
	return rt == 1
}

// BOOL WINAPI SetPriorityClass
//   _In_ HANDLE hProcess,
//   _In_ DWORD  dwPriorityClass
func SetPriorityClass(hProcess syscall.Handle, dwPriorityClass uint32) bool {
	libkernel32, _ := syscall.LoadLibrary("kernel32.dll")
	setPriorityClass, _ := syscall.GetProcAddress(libkernel32, "SetPriorityClass")
	ret, _, _ := syscall.Syscall(setPriorityClass, 2,
		uintptr(hProcess),
		uintptr(dwPriorityClass),
		0)
	return ret == 1
}

//Helper function convert a Go time to System time
func GoTimeToSystemTime(goTime time.Time) SYSTEMTIME {
	var systemTime SYSTEMTIME
	systemTime.Day = uint16(goTime.Day())
	systemTime.Hour = uint16(goTime.Hour())
	systemTime.DayOfWeek = uint16(goTime.Weekday())
	systemTime.Milliseconds = uint16(goTime.Nanosecond() / 1000 / 1000)
	systemTime.Minute = uint16(goTime.Minute())
	systemTime.Month = uint16(goTime.Month())
	systemTime.Second = uint16(goTime.Second())
	systemTime.Year = uint16(goTime.Year())

	return systemTime
}
