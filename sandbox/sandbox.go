package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func RunCode(code []byte) error{
	return createSanbox(code)
}

func createSanbox(code []byte) error {
	//Synchronization pipe to ensure that parent is the one that spawned the child process
	readFd, writeFd, err := os.Pipe()

	if err != nil {
		return errors.New("Error: Synchronization pipe cannot be created")
	}

	defer readFd.Close()
	defer writeFd.Close()

	//Another pipe to send code to the child
	readCode, writeCode, err := os.Pipe()

	if err != nil {
		return errors.New("Error: Code pipe cannot be created")
	}

	defer writeCode.Close()
	defer readCode.Close()

	cmd := exec.Command("/proc/self/exe", "child")
	cmd.ExtraFiles = []*os.File{readFd, readCode}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	hostUid := os.Getuid()
	hostGid := os.Getgid()

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNET | syscall.CLONE_NEWUSER,
		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: hostUid, Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: hostGid, Size: 1},
		},
	}

	if err := cmd.Start(); err != nil {
		return errors.New("Error: Cannot spawn child process")
	}

	writeCode.Write(code)

	if err := AttachToCGroup(cmd); err != nil{
		return err
	}

	writeFd.Write([]byte{0})

	return nil
}
