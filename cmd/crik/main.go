//go:build linux
// +build linux

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

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/alecthomas/kong"
	"github.com/checkpoint-restore/go-criu/v7"

	"github.com/qawolf/crik/internal/controller/node"
	cexec "github.com/qawolf/crik/internal/exec"
)

var signalChan = make(chan os.Signal, 1)

var cli struct {
	Debug bool `help:"Enable debug mode."`

	Run Run `cmd:"" help:"Run given command wrapped by crik."`
}

func main() {
	ctx := kong.Parse(&cli)
	if err := ctx.Run(); err != nil {
		fmt.Printf("failed to run the command: %s", err.Error())
		os.Exit(1)
	}
}

type Run struct {
	Args []string `arg:"" optional:"" passthrough:"" name:"command" help:"Command and its arguments to run. Required if --image-dir is not given or empty."`

	ConfigPath string `type:"path" default:"/etc/crik/config.yaml" help:"Path to the configuration file."`
}

func (r *Run) Run() error {
	cfg, err := cexec.ReadConfiguration(r.ConfigPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read configuration: %w", err)
	}
	willRestore, err := shouldRestore(cfg)
	if err != nil {
		return fmt.Errorf("failed to check if restore is needed: %w", err)
	}
	if willRestore {
		fmt.Printf("A checkpoint has been found in %s. Restoring.\n", cfg.ImageDir)
		if err := cexec.RestoreWithCmd(cfg.ImageDir); err != nil {
			return fmt.Errorf("failed to restore: %w", err)
		}
		return nil
	}
	if len(r.Args) == 0 {
		return fmt.Errorf("command is required when there is no checkpoint to restore, i.e. --image-dir is not given or empty")
	}
	// Make sure the PID is a high number so that it's not taken up during restore.
	lastPidPath := "/proc/sys/kernel/ns_last_pid"
	if err := os.WriteFile(lastPidPath, []byte("9000"), 0644); err != nil {
		return fmt.Errorf("failed to write to %s: %w", lastPidPath, err)
	}

	cmd := exec.Command(r.Args[0], r.Args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:       true,
		Unshareflags: syscall.CLONE_NEWIPC,
	}
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}
	fmt.Printf("Command started with PID %d\n", cmd.Process.Pid)
	if cfg.ImageDir != "" {
		fmt.Printf("Setting up SIGTERM handler to take checkpoint in %s\n", cfg.ImageDir)
		signal.Notify(signalChan, syscall.SIGTERM)
		sig := <-signalChan
		switch sig {
		case syscall.SIGTERM:
			fmt.Println("Received SIGTERM.")
			// Take checkpoint only if the node is in shutting down state or the node state server is not given.
			if cfg.NodeStateServerURL != "" {
				nodeName := os.Getenv("KUBERNETES_NODE_NAME")
				resp, err := http.Get(fmt.Sprintf("%s/nodes/%s", cfg.NodeStateServerURL, nodeName))
				if err != nil {
					return fmt.Errorf("failed to get node state: %w", err)
				}
				defer resp.Body.Close()
				var response node.Node
				if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
					return fmt.Errorf("failed to decode node state: %w", err)
				}
				if response.State != node.NodeStateShuttingDown {
					fmt.Println("Node is not in shutting down state. Not taking checkpoint.")
					if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
						return fmt.Errorf("failed to send SIGTERM to the process: %w", err)
					}
					return cmd.Wait()
				}
			}
			duration, err := cexec.TakeCheckpoint(criu.MakeCriu(), cmd.Process.Pid, cfg)
			if err != nil {
				return fmt.Errorf("failed to take checkpoint: %w", err)
			}
			fmt.Printf("Checkpoint taken in %s\n", duration)
		}
	}
	return cmd.Wait()
}

func shouldRestore(cfg cexec.Configuration) (bool, error) {
	if cfg.ImageDir == "" {
		return false, nil
	}
	entries, err := os.ReadDir(cfg.ImageDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".img") {
			return true, nil
		}
	}
	return false, nil
}
