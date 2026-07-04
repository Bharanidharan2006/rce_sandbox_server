package sandbox

import (
	"errors"
	"os"
	"os/exec"
)

func CreateSandbox(code []byte) error {
	//Synchronization pipe to ensure that parent is the one that spawned the child process
	readFd, writeFd, err := os.Pipe()

	if err != nil {
		return errors.New("Error: Synchronization Pipe Cannot Be Created")
	}

	defer readFd.Close()
	defer writeFd.Close()

	cmd := exec.Command("/proc/self/exe", "child")
}
