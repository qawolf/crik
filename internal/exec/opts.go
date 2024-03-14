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
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/checkpoint-restore/go-criu/v7/crit"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const (
	ConfigurationFileName = "configuration.yaml"
)

func ReadConfiguration(path string) (Configuration, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Configuration{}, fmt.Errorf("failed to read configuration file: %w", err)
	}
	var c Configuration
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Configuration{}, fmt.Errorf("failed to unmarshal configuration: %w", err)
	}
	return c, nil
}

// Configuration lets crik know about quirks of the processes whose checkpoint is being taken. For example, the files
// that need to be part of the checkpoint but are not part of the container's image need to be specified here.
type Configuration struct {
	// ImageDir is the directory where the checkpoint is stored. It is expected to be available in the new container as
	// well.
	ImageDir string `json:"imageDir"`

	// NodeStateServerURL is the URL of the node state server. If given, crik will first check if the node is in shutting
	// down state and only then take checkpoint.
	// If not given, crik will always take checkpoint when it receives SIGTERM.
	NodeStateServerURL string `json:"nodeStateServerURL"`

	// AdditionalPaths is the list of paths that are not part of the container's image but were opened by one of the
	// processes in the tree. We need to make sure that these paths are available in the new container as well.
	// The paths are relative to the root of the container's filesystem.
	// Entries can be path to a file or a directory.
	AdditionalPaths []string `json:"additionalPaths,omitempty"`

	// InotifyIncompatiblePaths is the list of paths that are known to cause issues with inotify. We delete those paths
	// before taking the checkpoint.
	InotifyIncompatiblePaths []string `json:"inotifyIncompatiblePaths,omitempty"`
}

// configurationOnDisk contains additional metadata information about the checkpoint that is used during restore.
type configurationOnDisk struct {
	Configuration

	// UnixFileDescriptors is the list of file descriptors that are opened by all UNIX processes by default.
	// They map to 0 -> stdin, 1 -> stdout, 2 -> stderr.
	// In containers, these are connected to either /dev/null or pipes. We need to make sure that when we restore, the
	// pipes are connected to criu's stdin, stdout, and stderr which is what's connected to the new container's stdin,
	// stdout, and stderr.
	// This list has only 3 elements in all cases.
	UnixFileDescriptorTrio []string `json:"unixFileDescriptorTrio,omitempty"`
}

var (
	// DirectoryMounts is the list of directories that are mounted by the container runtime and need to be marked as
	// such during checkpoint and restore so that the underlying files can change without breaking the restore process.
	DirectoryMounts = []DirectoryMount{
		{
			Name:             "zoneinfo",
			PathInCheckpoint: "/usr/share/zoneinfo",
			PathInRestore:    "/usr/share/zoneinfo",
		},
		{
			Name:             "null",
			PathInCheckpoint: "/dev/null",
			PathInRestore:    "/dev/null",
		},
		{
			Name:             "random",
			PathInCheckpoint: "/dev/random",
			PathInRestore:    "/dev/random",
		},
		{
			Name:             "urandom",
			PathInCheckpoint: "/dev/urandom",
			PathInRestore:    "/dev/urandom",
		},
		{
			Name:             "tty",
			PathInCheckpoint: "/dev/tty",
			PathInRestore:    "/dev/tty",
		},
		{
			Name:             "zero",
			PathInCheckpoint: "/dev/zero",
			PathInRestore:    "/dev/zero",
		},
		{
			Name:             "full",
			PathInCheckpoint: "/dev/full",
			PathInRestore:    "/dev/full",
		},
	}
)

type DirectoryMount struct {
	Name             string `json:"name"`
	PathInCheckpoint string `json:"pathInCheckpoint"`
	PathInRestore    string `json:"pathInRestore"`
}

func GetExternalDirectoriesForCheckpoint() []string {
	result := make([]string, len(DirectoryMounts))
	for i, d := range DirectoryMounts {
		result[i] = fmt.Sprintf("mnt[%s]:%s", d.PathInCheckpoint, d.Name)
	}
	return result
}

func GetExternalDirectoriesForRestore() []string {
	result := make([]string, len(DirectoryMounts))
	for i, d := range DirectoryMounts {
		result[i] = fmt.Sprintf("mnt[%s]:%s", d.Name, d.PathInRestore)
	}
	return result
}

func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(srcPath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, srcPath)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(dstPath, d.Type().Perm())
		}
		// TODO(muvaf): This changes the perms of folder if the dir wasn't walked before.
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}
		src, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		defer src.Close()

		dst, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dst.Close()

		if _, err := io.Copy(dst, src); err != nil {
			return err
		}

		// Get the source file mode to apply to the destination file
		srcInfo, err := src.Stat()
		if err != nil {
			return err
		}
		return os.Chmod(dstPath, srcInfo.Mode())
	})
}

func GetKubePodFilePaths(imageDir string) (map[string]string, error) {
	c := crit.New(nil, nil, imageDir, false, false)
	fds, err := c.ExploreFds()
	if err != nil {
		return nil, fmt.Errorf("failed to explore fds: %w", err)
	}
	result := map[string]string{}
	for _, fd := range fds {
		for _, file := range fd.Files {
			if !strings.HasPrefix(file.Path, "/sys/fs/cgroup/kubepods.slice") ||
				file.Type != "REG" {
				continue
			}
			result[filepath.Base(file.Path)] = file.Path
		}
	}
	return result, nil
}
