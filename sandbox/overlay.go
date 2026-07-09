package sandbox

import (
	"fmt"
	"os"
	"syscall"
)

func prepareFileSystem(requestID string) (string, error) {
	baseFS := "/home/azureuser/containerfs"
	upperDir := fmt.Sprintf("/tmp/sandbox/%s/upper", requestID)
	workDir := fmt.Sprintf("/tmp/sandbox/%s/work", requestID)
	mergedDir := fmt.Sprintf("/tmp/sandbox/%s/merged", requestID)

	must(os.MkdirAll(fmt.Sprintf("/tmp/sandbox/%s", requestID), 0755))
	must(os.MkdirAll(upperDir, 0755))
	must(os.MkdirAll(workDir, 0755))
	must(os.MkdirAll(mergedDir, 0755))

	data := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", baseFS, upperDir, workDir)
	err := syscall.Mount("overlay", mergedDir, "overlay", 0, data)

	return mergedDir, err
}
