package sandbox

import (
	"os"
	"os/exec"
	"strconv"
)

const cGroupBasePath string = "/sys/fs/cgroup/sandbox/container_"

func AttachToCGroup(cmd *exec.Cmd) error {
	cgPath := cGroupBasePath + strconv.Itoa(cmd.Process.Pid)

	err := os.MkdirAll(cgPath, 0755)

	if err != nil{
		return err
	}

	err = os.WriteFile(cgPath + "/memory.max", []byte("50000000"), 0700)

	if err != nil{
		return err
	}

	err = os.WriteFile(cgPath + "/pids.max", []byte("10"), 0700)

	if err != nil{
		return err
	}

	pidStr := strconv.Itoa(cmd.Process.Pid)

	err = os.WriteFile(cgPath + "/cgroup.procs", []byte(pidStr), 0700)

	if err != nil{
		return err
	}

	err = cmd.Wait()

	if err != nil{
		return err
	}

	err = os.Remove(cgPath)

	return err

}