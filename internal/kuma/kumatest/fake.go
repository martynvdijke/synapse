// Package kumatest provides a scriptable in-memory kuma.API for tests in
// other packages. It records calls and can return per-method errors or
// custom results.
package kumatest

import (
	"context"
	"sync"

	"synapse/internal/kuma"
)

// Call is one recorded invocation.
type Call struct {
	Method    string
	MonitorID int
	Spec      kuma.MonitorSpec
}

// Fake implements kuma.API. Set the Fn field for a method to control its
// result; otherwise the Err field (if set) is returned, and the zero value is
// otherwise returned.
type Fake struct {
	mu    sync.Mutex
	calls []Call

	MonitorsFn      func(ctx context.Context) ([]kuma.KumaMonitor, error)
	DockerHostsFn   func(ctx context.Context) ([]kuma.DockerHost, error)
	AddMonitorFn    func(ctx context.Context, spec kuma.MonitorSpec) (int, error)
	EditMonitorFn   func(ctx context.Context, id int, spec kuma.MonitorSpec) error
	DeleteMonitorFn func(ctx context.Context, id int) error
	PauseMonitorFn  func(ctx context.Context, id int) error
	ResumeMonitorFn func(ctx context.Context, id int) error
	PingFn          func(ctx context.Context) error

	MonitorsErr      error
	DockerHostsErr   error
	AddMonitorErr    error
	EditMonitorErr   error
	DeleteMonitorErr error
	PauseMonitorErr  error
	ResumeMonitorErr error
	PingErr          error
}

var _ kuma.API = (*Fake)(nil)

// New returns an empty Fake.
func New() *Fake { return &Fake{} }

func (f *Fake) record(c Call) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
}

// Calls returns a copy of the recorded calls, in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	copy(out, f.calls)
	return out
}

// Reset clears recorded calls (configuration is preserved).
func (f *Fake) Reset() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

// LastCall returns the most recent call for method, if any.
func (f *Fake) LastCall(method string) (Call, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Method == method {
			return f.calls[i], true
		}
	}
	return Call{}, false
}

func (f *Fake) Monitors(ctx context.Context) ([]kuma.KumaMonitor, error) {
	f.record(Call{Method: "Monitors"})
	if f.MonitorsFn != nil {
		return f.MonitorsFn(ctx)
	}
	return nil, f.MonitorsErr
}

func (f *Fake) DockerHosts(ctx context.Context) ([]kuma.DockerHost, error) {
	f.record(Call{Method: "DockerHosts"})
	if f.DockerHostsFn != nil {
		return f.DockerHostsFn(ctx)
	}
	return nil, f.DockerHostsErr
}

func (f *Fake) AddMonitor(ctx context.Context, spec kuma.MonitorSpec) (int, error) {
	f.record(Call{Method: "AddMonitor", Spec: spec})
	if f.AddMonitorFn != nil {
		return f.AddMonitorFn(ctx, spec)
	}
	return 0, f.AddMonitorErr
}

func (f *Fake) EditMonitor(ctx context.Context, id int, spec kuma.MonitorSpec) error {
	f.record(Call{Method: "EditMonitor", MonitorID: id, Spec: spec})
	if f.EditMonitorFn != nil {
		return f.EditMonitorFn(ctx, id, spec)
	}
	return f.EditMonitorErr
}

func (f *Fake) DeleteMonitor(ctx context.Context, id int) error {
	f.record(Call{Method: "DeleteMonitor", MonitorID: id})
	if f.DeleteMonitorFn != nil {
		return f.DeleteMonitorFn(ctx, id)
	}
	return f.DeleteMonitorErr
}

func (f *Fake) PauseMonitor(ctx context.Context, id int) error {
	f.record(Call{Method: "PauseMonitor", MonitorID: id})
	if f.PauseMonitorFn != nil {
		return f.PauseMonitorFn(ctx, id)
	}
	return f.PauseMonitorErr
}

func (f *Fake) ResumeMonitor(ctx context.Context, id int) error {
	f.record(Call{Method: "ResumeMonitor", MonitorID: id})
	if f.ResumeMonitorFn != nil {
		return f.ResumeMonitorFn(ctx, id)
	}
	return f.ResumeMonitorErr
}

func (f *Fake) Ping(ctx context.Context) error {
	f.record(Call{Method: "Ping"})
	if f.PingFn != nil {
		return f.PingFn(ctx)
	}
	return f.PingErr
}
