/*
Copyright 2019 The Knative Authors

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

package health

import (
	"io"
	"net/http"
	"sync"

	"knative.dev/serving/pkg/queue"
)

// State holds state about the current healthiness of the component.
type State struct {
	alive        bool
	shuttingDown bool
	mutex        sync.RWMutex

	drainCh        chan struct{}
	drainCompleted bool
}

// NewState returns a new State with both alive and shuttingDown set to false.
func NewState() *State {
	return &State{
		drainCh: make(chan struct{}),
	}
}

// isAlive returns whether or not the health server is in a known
// working state currently.
func (h *State) isAlive() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	return h.alive
}

// isShuttingDown returns whether or not the health server is currently
// shutting down.
func (h *State) isShuttingDown() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	return h.shuttingDown
}

// setAlive updates the state to declare the service alive.
func (h *State) setAlive() {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.alive = true
	h.shuttingDown = false
}

// shutdown updates the state to declare the service shutting down.
func (h *State) shutdown() {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.alive = false
	h.shuttingDown = true
}

// drainFinish updates that we finished draining.
func (h *State) drainFinished() {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if !h.drainCompleted {
		close(h.drainCh)
	}

	h.drainCompleted = true
}

// HandleHealthProbe handles the probe according to the current state of the
// health server.
func (h *State) HandleHealthProbe(prober func() bool, w http.ResponseWriter) {
	sendAlive := func() {
		io.WriteString(w, queue.Name)
	}

	sendNotAlive := func() {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	sendShuttingDown := func() {
		w.WriteHeader(http.StatusGone)
	}

	switch {
	case h.isShuttingDown():
		sendShuttingDown()
	case prober != nil && !prober():
		sendNotAlive()
	default:
		h.setAlive()
		sendAlive()
	}
}

// DrainHandlerFunc constructs an HTTP handler that waits until the proxy server is shut down.
func (h *State) DrainHandlerFunc() func(_ http.ResponseWriter, _ *http.Request) {
	return func(_ http.ResponseWriter, _ *http.Request) {
		<-h.drainCh
	}
}

// Shutdown marks the proxy server as not ready and begins its shutdown process. This
// results in unblocking any connections waiting for drain.
func (h *State) Shutdown(drain func()) {
	h.shutdown()

	if drain != nil {
		drain()
	}

	h.drainFinished()
}
