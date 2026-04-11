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

package orchestrator

import "time"

// EventType classifies bootstrap events.
type EventType int

const (
	EventPhaseChange     EventType = iota // Orchestrator phase transition
	EventSuccess                          // Successful step completion
	EventInfo                             // Informational message
	EventWarning                          // Non-fatal warning
	EventError                            // Error (may be retried)
	EventDebug                            // Debug-level detail
	EventBootstrapStatus                  // Full CRD status snapshot from watchBootstrap
	EventComplete                         // Bootstrap finished successfully
	EventFailed                           // Bootstrap failed
	EventKINDReady                        // KIND cluster is up, kubeconfig available
)

// Event is a single bootstrap lifecycle event emitted by the orchestrator.
type Event struct {
	Type           EventType
	Timestamp      time.Time
	Message        string
	Phase          string              // Current orchestrator phase name
	Status         *BootstrapSnapshot  // Non-nil for EventBootstrapStatus
	Creds          *ClusterCredentials // Non-nil for EventComplete
	Error          error               // Non-nil for EventFailed
	KINDKubeconfig string              // Non-empty for EventKINDReady: path to KIND kubeconfig file
}

// BootstrapSnapshot captures the full ClusterBootstrap CRD status at a point in time.
type BootstrapSnapshot struct {
	Phase           string
	Machines        []MachineStatus
	AddonsInstalled map[string]bool
	FailureReason   string
	FailureMessage  string
	Endpoint        string
	ConsoleURL      string
}

// MachineStatus tracks a single machine's provisioning state.
type MachineStatus struct {
	Name            string
	Role            string
	Phase           string
	IPAddress       string
	TalosConfigured bool
	Ready           bool
}

// ClusterCredentials holds the kubeconfig and talosconfig for a bootstrapped cluster.
type ClusterCredentials struct {
	Kubeconfig      []byte
	Talosconfig     []byte
	ControlPlaneIPs []string
	ConsoleURL      string
}

// EventSink receives events from the orchestrator. Implementations must be
// safe for concurrent use.
type EventSink interface {
	Send(Event)
}

// ChannelSink is an EventSink backed by a buffered channel. Events are
// dropped (with debug logging) if the channel is full.
type ChannelSink struct {
	ch     chan<- Event
	debugf func(string, ...any) // Optional debug logger for dropped events
}

// NewChannelSink creates a ChannelSink that sends to the provided channel.
// debugf is called when an event is dropped; pass nil to silently drop.
func NewChannelSink(ch chan<- Event, debugf func(string, ...any)) *ChannelSink {
	return &ChannelSink{ch: ch, debugf: debugf}
}

// Send emits an event. If the channel is full, the event is dropped and
// logged at debug level.
func (s *ChannelSink) Send(e Event) {
	e.Timestamp = time.Now()
	select {
	case s.ch <- e:
	default:
		if s.debugf != nil {
			s.debugf("dropped event (channel full)", "type", e.Type, "message", e.Message)
		}
	}
}
