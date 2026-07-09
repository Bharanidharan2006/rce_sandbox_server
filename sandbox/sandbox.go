package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"
)

const oldRoot string = "put_old"

type socketMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type outputPayload struct {
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

func must(err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		log.Fatalf("Error at %s:%d - %v", file, line, err)
	}
}

func emit(conn *websocket.Conn, event string, data interface{}) {
	dataBytes, _ := json.Marshal(data)
	msg := socketMessage{
		Event: event,
		Data:  dataBytes,
	}

	conn.WriteJSON(msg)
}

func RunCode(conn *websocket.Conn, code []byte, inputChan <-chan string) error {
	//Synchronization pipe to ensure that parent is the one that spawned the child process
	readFd, writeFd, err := os.Pipe()

	if err != nil {
		return errors.New("Error: Synchronization pipe cannot be created")
	}

	//Another pipe to send code to the child
	readCode, writeCode, err := os.Pipe()

	if err != nil {
		return errors.New("Error: Code pipe cannot be created")
	}

	cmd := exec.Command("/proc/self/exe", "child")
	cmd.ExtraFiles = []*os.File{readFd, readCode}
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	stdin, _ := cmd.StdinPipe()

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
		writeFd.Close()
		return fmt.Errorf("Error: Cannot spawn child process: %w", err)
	}
	readFd.Close()
	readCode.Close()

	// Ensure writeFd is always closed so the child never hangs
	// waiting on the sync pipe if the parent returns early.
	defer writeFd.Close()

	writeCode.Write(code)
	writeCode.Close()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				emit(conn, "output", outputPayload{
					Stream: "stdout",
					Text:   string(buf[:n]),
				})
			}
			if err != nil {
				break
			}
		}
	}()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				emit(conn, "output", outputPayload{
					Stream: "stderr",
					Text:   string(buf[:n]),
				})
			}
			if err != nil {
				break
			}
		}
	}()

	done := make(chan struct{})

	go func() {
		defer stdin.Close()

		for {
			select {
			case text := <-inputChan:
				_, err := stdin.Write([]byte(text + "\n"))
				if err != nil {
					continue
				}
			case <-done:
				return
			}
		}
	}()

	cgPath, err := attachToCGroup(cmd.Process.Pid)
	if err != nil {
		log.Printf("cgroup setup failed: %v – killing child", err)
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("cgroup setup failed: %w", err)
	}
	defer os.Remove(cgPath)

	// Signal the child that the parent is ready
	writeFd.Write([]byte{0})

	if err := cmd.Wait(); err != nil {
		return err
	}

	close(done)
	emit(conn, "finished", nil)

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
	n, err := syncPipe.Read(buf)
	if err != nil || n == 0 {
		log.Fatal("The child is not spawned by the parent")
	}
	syncPipe.Close()

	must(syscall.Sethostname([]byte("container")))

	// Pivot root cannot be done on shared mount propagation so make it private recursive and then create a bind mount on the new root directory
	must(syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""))

	requestID := uuid.New().String()
	mergedDir, err := prepareFileSystem(requestID)
	if err != nil {
		log.Fatalf("Failed to prepare filesystem (overlay mount): %v", err)
	}

	procDir := mergedDir + "/proc"
	// Everybody can read it though only the owner can do crud
	must(os.MkdirAll(procDir, 0755))
	flags := uintptr(syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV)
	must(syscall.Mount("proc", procDir, "proc", flags, ""))

	pivotDir := mergedDir + "/" + oldRoot
	// 0700 - Cause we want only the owner to read, update and write it and no other person should be able to access it
	must(os.MkdirAll(pivotDir, 0700))

	must(syscall.PivotRoot(mergedDir, pivotDir))

	defer func() {
		must(syscall.Unmount(mergedDir, 0))
		must(os.RemoveAll(fmt.Sprintf("/tmp/sandbox/%s", requestID)))
	}()

	must(syscall.Chdir("/"))

	// Unmount the older filesystem
	must(syscall.Unmount("/"+oldRoot, syscall.MNT_DETACH))

	// Delete that unmounted old root
	must(os.Remove("/" + oldRoot))

	must(os.WriteFile("/tmp/submission.py", code, 0644))

	//Remove all the admin capabilities before running the command
	for i := 0; i < 64; i++ {
		err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(i), 0, 0, 0)
		if err != nil && !errors.Is(err, syscall.EINVAL) {
			fmt.Printf("Failed to drop the capability %d: %v", i, err)
		}
	}

	must(unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0))

	env := []string{
		"PATH=/usr/bin:/bin",
		"LANG=en_US.UTF-8",
		"PYTHONUNBUFFERED=1",
	}

	if err := setUpSeccomp(); err != nil {
		log.Fatalf("seccomp filter cannot be setup: %v", err)
	}

	must(syscall.Exec("/usr/bin/python3", []string{"python3", "/tmp/submission.py"}, env))
}
