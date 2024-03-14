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

package node

import (
	"k8s.io/apimachinery/pkg/util/json"
	"net/http"
	"strings"
	"sync"
)

// Node is the state of a node.
type Node struct {
	Name  string    `json:"name"`
	State NodeState `json:"state"`
}

type NodeState string

// Node states.
var (
	NodeStateUnknown      NodeState = "unknown"
	NodeStateShuttingDown NodeState = "shutting-down"
)

type Server struct {
	nodes map[string]NodeState

	*sync.RWMutex
}

func NewServer() *Server {
	return &Server{
		nodes: map[string]NodeState{},
	}
}

// SetNodeState sets the state of a node.
func (s *Server) SetNodeState(name string, state NodeState) {
	s.Lock()
	defer s.Unlock()
	s.nodes[name] = state
}

// GetNodeState gets the state of a node.
func (s *Server) GetNodeState(name string) NodeState {
	s.RLock()
	defer s.RUnlock()
	if node, ok := s.nodes[name]; ok {
		return node
	}
	return NodeStateUnknown
}

// DeleteNodeState deletes the state of a node from the map.
func (s *Server) DeleteNodeState(name string) {
	s.Lock()
	defer s.Unlock()
	delete(s.nodes, name)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	nodeName := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/nodes/"), "/")
	if nodeName == "" {
		http.Error(w, "node query parameter is missing", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		n := Node{
			Name:  nodeName,
			State: s.GetNodeState(nodeName),
		}
		if err := json.NewEncoder(w).Encode(n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
	return
}
