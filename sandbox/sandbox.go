package sandbox

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

const newRoot string = "/tmp/rootfs"
const oldRoot string = "put_old"

func must(err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		log.Fatalf("Error at %s:%d - %v", file, line, err)
	}
}

func RunCode(code []byte) error{
	return createSanbox(code)
}

func createSanbox(code []byte) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
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
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

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

	if err := cmd.Wait(); err != nil {
		errLine := stderr.String()
		fmt.Println(errLine)
		return errors.New("Error: Command exited with status code 1")
	}

	result := stdout.String()
	fmt.Println(result)

	return nil
}

func RunChild() {

	syncPipe := os.NewFile(3, "sync")
	codePipe := os.NewFile(4, "code")

	if syncPipe == nil || codePipe == nil {
		os.Exit(1)
	}

	code, err := io.ReadAll(codePipe)
	must(err)
	codePipe.Close()

	buf := make([]byte, 1)
	syncPipe.Read(buf)
	syncPipe.Close() 

	must(syscall.Sethostname([]byte("container")))

	// Pivot root cannot be done on shared mount propagation so make it private recursive and then create a bind mount on the new root directory
	must(syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""))
	must(syscall.Mount(newRoot, newRoot, "", syscall.MS_BIND|syscall.MS_REC, ""))


	procDir := newRoot + "/proc"
	// Everybody can read it though only the owner can do crud
	must(os.MkdirAll(procDir, 0755))
	flags := uintptr(syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV)
	must(syscall.Mount("proc", procDir, "proc", flags, ""))

	pivotDir := newRoot + "/" + oldRoot
	// 0700 - Cause we want only the owner to read, update and write it and no other person should be able to access it
	must(os.MkdirAll(pivotDir, 0700))

	must(syscall.PivotRoot(newRoot, pivotDir))

	must(syscall.Chdir("/"))

	// Unmount the older filesystem
   must(syscall.Unmount("/"+oldRoot, syscall.MNT_DETACH))

	// Delete that unmounted old root
	must(os.Remove("/" + oldRoot))

	must(os.WriteFile("/tmp/submission.py", code, 0644))

	//Remove all the admin capabilities before running the command
	for i := 0; i < 64; i++ {
		unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(i), 0, 0, 0)
	}

	must(syscall.Exec("/usr/bin/python3", []string{"python3", "/tmp/submission.py"}, os.Environ()))
}
