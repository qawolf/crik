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
	"path/filepath"
	"sigs.k8s.io/yaml"
	"strconv"
	"syscall"
	"time"

	"github.com/checkpoint-restore/go-criu/v7"
	"github.com/checkpoint-restore/go-criu/v7/rpc"
	"google.golang.org/protobuf/proto"
)

type Actions struct {
	pid           int
	configuration Configuration
}

// PreDump is called when criu is about to dump the process.
func (a Actions) PreDump() error {
	// Temp hack to resolve crash during dump.
	for _, p := range a.configuration.InotifyIncompatiblePaths {
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("failed to remove %s: %w", p, err)
		}
	}
	conf := &configurationOnDisk{
		Configuration: a.configuration,
	}
	conf.UnixFileDescriptorTrio = make([]string, 3)
	fdDir := filepath.Join("/proc", strconv.Itoa(a.pid), "fd")
	for i := 0; i < 3; i++ {
		fdPath := filepath.Join(fdDir, strconv.Itoa(i))
		link, err := os.Readlink(fdPath)
		if err != nil {
			return fmt.Errorf("failed to read link of %s: %w", fdPath, err)
		}
		conf.UnixFileDescriptorTrio[i] = link
	}
	confYAML, err := yaml.Marshal(conf)
	if err != nil {
		return fmt.Errorf("failed to marshal fds: %w", err)
	}
	if err := os.WriteFile(filepath.Join(a.configuration.ImageDir, ConfigurationFileName), confYAML, 0o600); err != nil {
		return fmt.Errorf("failed to write stdio-fds.json: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(a.configuration.ImageDir, "extraFiles"), 0755); err != nil {
		return fmt.Errorf("failed to create extra path: %w", err)
	}
	for _, p := range a.configuration.AdditionalPaths {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		if err := CopyDir(p, filepath.Join(a.configuration.ImageDir, "extraFiles", p)); err != nil {
			return fmt.Errorf("failed to copy %s: %w", p, err)
		}
	}
	return nil
}

// PostDump does nothing.
func (a Actions) PostDump() error {
	return nil
}

// PreRestore does nothing.
func (a Actions) PreRestore() error {
	return nil
}

// PostRestore does nothing.
func (a Actions) PostRestore(pid int32) error {
	return nil
}

// NetworkLock does nothing.
func (a Actions) NetworkLock() error {
	return nil
}

// NetworkUnlock does nothing.
func (a Actions) NetworkUnlock() error {
	return nil
}

// SetupNamespaces does nothing.
func (a Actions) SetupNamespaces(_ int32) error {
	return nil
}

// PostSetupNamespaces does nothing.
func (a Actions) PostSetupNamespaces() error {
	return nil
}

// PostResume does nothing.
func (a Actions) PostResume() error {
	return nil
}

func TakeCheckpoint(c *criu.Criu, pid int, configuration Configuration) (time.Duration, error) {
	start := time.Now()
	fd, err := syscall.Open(configuration.ImageDir, syscall.O_DIRECTORY, 755)
	if err != nil {
		return time.Since(start), fmt.Errorf("failed to open directory %s: %w", configuration.ImageDir, err)
	}
	cgMode := rpc.CriuCgMode_IGNORE
	opts := &rpc.CriuOpts{
		TcpEstablished:    proto.Bool(true),
		ShellJob:          proto.Bool(false),
		FileLocks:         proto.Bool(false),
		LogFile:           proto.String("dump.log"),
		AutoDedup:         proto.Bool(false),
		Pid:               proto.Int32(int32(pid)),
		ImagesDirFd:       proto.Int32(int32(fd)), // To make it use ImagesDir.
		OrphanPtsMaster:   proto.Bool(true),
		NotifyScripts:     proto.Bool(true),
		LeaveRunning:      proto.Bool(false),
		LeaveStopped:      proto.Bool(false),
		LogLevel:          proto.Int32(4),
		LazyPages:         proto.Bool(false),
		GhostLimit:        proto.Uint32(500 * 1048576), // 500MB
		Root:              proto.String("/"),
		TcpClose:          proto.Bool(true),
		ManageCgroupsMode: &cgMode,
		External:          GetExternalDirectoriesForCheckpoint(),
	}
	actions := Actions{
		pid:           pid,
		configuration: configuration,
	}
	if err := c.Dump(opts, actions); err != nil {
		return time.Since(start), fmt.Errorf("failed to dump: %w", err)
	}
	return time.Since(start), nil
}
