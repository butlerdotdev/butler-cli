/*
Copyright 2026 The Butler Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

//go:build windows

package cluster

import "syscall"

// detachSysProcAttr starts the child in a new process group, detached from the
// parent's console, so it survives the parent process exiting (the kubeconfig
// command returns; the port-forward keeps running for the follow-up kubectl
// invocation).
func detachSysProcAttr() *syscall.SysProcAttr {
	const (
		createNewProcessGroup = 0x00000200 // CREATE_NEW_PROCESS_GROUP
		detachedProcess       = 0x00000008 // DETACHED_PROCESS
	)
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
