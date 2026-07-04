package sandbox

import (
	"fmt"

	seccomp "github.com/seccomp/libseccomp-golang"
)

var dockerDenyList = []string{
	// 1. Filesystem & Mount Escapes (They already have pivot_root, don't let them change it)
	"mount", "umount2", "pivot_root", "chroot",

	// 2. Namespace Escapes (Don't let them break out of our isolation)
	"unshare", "setns",

	// 3. Process & Memory Snooping (No injecting code into other processes)
	"ptrace", "process_vm_readv", "process_vm_writev",

	// 4. Kernel Modification (No messing with the host OS)
	"bpf", "kexec_load", "kexec_file_load", "reboot", "syslog", "swapon", "swapoff",

	// 5. Kernel Modules (No loading malicious drivers)
	"init_module", "finit_module", "delete_module",

	// 6. Keyring Management (No stealing encryption keys)
	"add_key", "request_key", "keyctl",

	// 7. System Administration
	"vhangup", "acct", "quotactl", "sethostname", "setdomainname",
}

func setUpSeccomp() error {
	filter, err := seccomp.NewFilter(seccomp.ActAllow)

	if err != nil {
		return fmt.Errorf("seccomp filter cannot be created: %v", err)
	}

	defer filter.Release()

	for _, call := range dockerDenyList {
		seccompSyscall, err := seccomp.GetSyscallFromName(call)

		if err != nil {
			continue // the syscall does not exist in this system
		}

		if err := filter.AddRule(seccompSyscall, seccomp.ActKillProcess); err != nil {
			return fmt.Errorf("syscall: %v cannot be allowed: %v", seccompSyscall, err)
		}
	}

	if err := filter.Load(); err != nil {
		return fmt.Errorf("seccomp filter cannot be loaded into the kernel: %v", err)
	}

	return nil
}
