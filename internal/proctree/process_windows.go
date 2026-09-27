package proctree

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x4} }
func attachProcess(p *os.Process) (Control, error) {
	job, _, err := kernel.NewProc("CreateJobObjectW").Call(0, 0)
	if job == 0 {
		return Control{}, fmt.Errorf("create process job: %w", err)
	}
	closeJob := func() { _ = syscall.CloseHandle(syscall.Handle(job)) }
	// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE also collects subprocesses when the
	// main service exits or watch closes the job during a restart.
	var limits struct {
		Basic struct {
			ProcessTime, JobTime int64
			Flags                uint32
			MinWS, MaxWS         uintptr
			Active               uint32
			Affinity             uintptr
			Priority, Scheduling uint32
		}
		IO                                                         [6]uint64
		ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
	}
	limits.Basic.Flags = 0x2000
	if ok, _, err := kernel.NewProc("SetInformationJobObject").Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); ok == 0 {
		closeJob()
		return Control{}, fmt.Errorf("configure process job: %w", err)
	}
	handle, err := syscall.OpenProcess(0x0100|0x0001, false, uint32(p.Pid))
	if err != nil {
		closeJob()
		return Control{}, err
	}
	defer syscall.CloseHandle(handle)
	if ok, _, err := kernel.NewProc("AssignProcessToJobObject").Call(job, uintptr(handle)); ok == 0 {
		closeJob()
		return Control{}, fmt.Errorf("attach process job: %w", err)
	}
	// The main thread is suspended until it belongs to the job, so no child
	// can escape between CreateProcess and AssignProcessToJobObject.
	if err := resumeProcess(uint32(p.Pid)); err != nil {
		closeJob()
		return Control{}, err
	}
	kill := func() { kernel.NewProc("TerminateJobObject").Call(job, 1) }
	return Control{kill, kill, closeJob}, nil
}

func resumeProcess(pid uint32) error {
	snapshot, _, err := kernel.NewProc("CreateToolhelp32Snapshot").Call(0x4, 0)
	if snapshot == ^uintptr(0) {
		return fmt.Errorf("enumerate suspended process thread: %w", err)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	var entry struct {
		Size, Usage, ThreadID, ProcessID uint32
		BasePriority, DeltaPriority      int32
		Flags                            uint32
	}
	entry.Size = uint32(unsafe.Sizeof(entry))
	ok, _, err := kernel.NewProc("Thread32First").Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		if entry.ProcessID == pid {
			thread, _, err := kernel.NewProc("OpenThread").Call(0x2, 0, uintptr(entry.ThreadID))
			if thread == 0 {
				return fmt.Errorf("open suspended process thread: %w", err)
			}
			resumed, _, err := kernel.NewProc("ResumeThread").Call(thread)
			syscall.CloseHandle(syscall.Handle(thread))
			if resumed == 0xffffffff {
				return fmt.Errorf("resume process thread: %w", err)
			}
			return nil
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		ok, _, err = kernel.NewProc("Thread32Next").Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	return fmt.Errorf("suspended process thread not found: %w", err)
}
