/*
Copyright 2024 QA Wolf Inc.

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

package exec

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sigs.k8s.io/yaml"
	"strings"
	"syscall"
)

func RestoreWithCmd(imageDir string) error {
	if err := os.MkdirAll("/tmp/.X11-unix", 0755); err != nil {
		return fmt.Errorf("failed to mkdir /tmp/.X11-unix: %w", err)
	}
	if err := CopyDir(filepath.Join(imageDir, "extraFiles"), "/"); err != nil {
		return fmt.Errorf("failed to copy extra files: %w", err)
	}
	args := []string{"restore",
		"--images-dir", imageDir,
		"--tcp-established",
		"--file-locks",
		"--evasive-devices",
		"--tcp-close",
		"--manage-cgroups=ignore",
		"-v4",
		"--log-file", "restore.log",
	}
	configYAML, err := os.ReadFile(filepath.Join(imageDir, ConfigurationFileName))
	if err != nil {
		return fmt.Errorf("failed to read stdio file descriptors: %w", err)
	}
	conf := &configurationOnDisk{}
	if err := yaml.Unmarshal(configYAML, conf); err != nil {
		return fmt.Errorf("failed to unmarshal stdio file descriptors: %w", err)
	}
	for _, d := range GetExternalDirectoriesForRestore() {
		args = append(args, "--external", d)
	}
	inheritedFds := conf.UnixFileDescriptorTrio

	// When cgroup v2 is used, the path to resource usage files contain pod and container IDs which are changed
	// in the new pod. We find and replace them with the new files.
	kubePodFiles, err := GetKubePodFilePaths(imageDir)
	if err != nil {
		return fmt.Errorf("failed to get kubepods.slice files: %w", err)
	}
	var extraFiles []*os.File
	if len(kubePodFiles) > 0 {
		// All processes within container are in the same cgroup, so getting the folder of self is enough.
		str, err := os.ReadFile("/proc/self/cgroup")
		if err != nil {
			return fmt.Errorf("failed to read /proc/self/cgroup: %w", err)
		}
		basePath := filepath.Join("/sys/fs/cgroup", strings.Split(strings.Split(string(str), "\n")[0], ":")[2])
		for k, v := range kubePodFiles {
			path := filepath.Join(basePath, k)
			f, err := os.OpenFile(path, syscall.O_RDONLY, 0)
			if err != nil {
				return fmt.Errorf("failed to open %s: %w", k, err)
			}
			// The index of file descriptor in extraFiles must match the index+3 in inheritedFds because
			// the first 3 file descriptors are reserved for stdin, stdout, and stderr.
			inheritedFds = append(inheritedFds, strings.TrimPrefix(v, "/"))
			extraFiles = append(extraFiles, f)
		}
	}
	for i, fdStr := range inheritedFds {
		args = append(args, "--inherit-fd", fmt.Sprintf("fd[%d]:%s", i, fdStr))
	}
	cmd := exec.Command("criu", args...)
	cmd.ExtraFiles = extraFiles
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
