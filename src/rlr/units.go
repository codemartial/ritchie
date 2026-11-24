// This file is the language side of a unit boundary for calling and serving services.
package rlr

import (
	"errors"
	"math"
	"os"
	"strconv"
	"time"

	"ritchie.dev/rlr/mesh"
)

// Locator names a logical unit independently of process.
type Locator = mesh.Locator

const (
	EntryBudgetEnv   = "RITCHIE_ENTRY_BUDGET_MS"
	OutboundLimitEnv = "RITCHIE_OUTBOUND_CALL_CONCURRENCY_LIMIT"
	LogLevelEnv      = "RITCHIE_LOG_LEVEL"
	PanicEnv         = mesh.PanicEnv
)

const defaultEntryBudget = 2 * time.Second

// settings are the operational limits and the reporting level that the application reads once at startup.
type settings struct {
	budget time.Duration
	limit  int
	level  int
}

// readSettings reads the two operational limits and the reporting level from the environment, where an unset variable gives its default and the error states the variable whose value is invalid.
func readSettings() (settings, error) {
	s := settings{budget: defaultEntryBudget, limit: mesh.DefaultLimit, level: terminalLevel}
	if text, set := os.LookupEnv(EntryBudgetEnv); set {
		ms, err := strconv.ParseInt(text, 10, 64)
		if err != nil || ms <= 0 || ms > math.MaxInt64/int64(time.Millisecond) {
			return s, errors.New(EntryBudgetEnv + " is not a positive integer of milliseconds")
		}
		s.budget = time.Duration(ms) * time.Millisecond
	}
	if text, set := os.LookupEnv(OutboundLimitEnv); set {
		n, err := strconv.Atoi(text)
		if err != nil || n <= 0 {
			return s, errors.New(OutboundLimitEnv + " is not a positive integer")
		}
		s.limit = n
	}
	if text, set := os.LookupEnv(LogLevelEnv); set {
		n, err := strconv.Atoi(text)
		if err != nil || n < 0 || n > recoveryLevel {
			return s, errors.New(LogLevelEnv + " is not a level from 0 to 3")
		}
		s.level = n
	}
	return s, nil
}

// Method is one service method as the generated program publishes it, where Results reads the results from their JSON form and is nil for a method without results.
type Method struct {
	Name    string
	Mutates bool
	Invoke  func(inv Invocation, args []any) ([]any, error)
	Decode  Decoder[[]any]
	Encode  func(results []any) []any
	Results Decoder[[]any]
}

// UnitSpec is one resident unit of the compiled inventory whose Route is the module component of its external routes.
type UnitSpec struct {
	Locator Locator
	Module  string
	Kind    string
	Route   string
	Methods []Method
	Calls   []Locator
}

// Application is a compiled program with resident units.
type Application struct {
	Module string
	Main   func(inv Invocation) error
	Stack  string
	Units  []UnitSpec
}

func newRuntime(app Application, host func([]mesh.Hosted) mesh.Realization) *mesh.Runtime {
	specs := make([]mesh.UnitSpec, len(app.Units))
	for i, u := range app.Units {
		specs[i] = mesh.UnitSpec{Locator: u.Locator, Handler: handler(u)}
	}
	rt := mesh.New(specs, host, Diag)
	rt.Outcome = OutcomeName
	return rt
}

// handler serves one unit by finding the method and reporting an escaping error once at the service level, where a runtime error that confirms relevant state unaltered reaches the caller as Unverified! when the invocation may have altered relevant state.
func handler(u UnitSpec) mesh.Handler {
	methods := make(map[string]Method, len(u.Methods))
	for _, m := range u.Methods {
		methods[m.Name] = m
	}
	module := u.Module
	return func(inv *mesh.Invocation, req mesh.Request) mesh.AppResponse {
		m, found := methods[req.Method]
		if !found {
			return mesh.AppResponse{Rejected: true}
		}
		results, err := m.Invoke(&Scope{inv: inv}, req.Args)
		if err != nil {
			if !stopped(inv) {
				report(serviceLevel, module, err.(RitchieError))
			}
			if inv.Altered() && CategoryOf(outcomeName(err)) == RequestFailure {
				return mesh.AppResponse{Err: NewError(Unverified.TypeID(), nil)}
			}
			return mesh.AppResponse{Err: err}
		}
		return mesh.AppResponse{Results: results}
	}
}

// stopped reports whether the cancel signal of the caller of an invocation has arrived, after which the caller has settled the invocation and the failure of its method is not reported.
func stopped(inv *mesh.Invocation) bool {
	select {
	case <-inv.Stopped():
		return true
	default:
		return false
	}
}

// failureOf is the error a caller receives for a response or nil when the response carries a result.
func failureOf(r mesh.Response, mutates bool) error {
	switch mesh.OutcomeOf(r, mutates) {
	case mesh.Unavailable:
		return NewError(Unavailable.TypeID(), nil)
	case mesh.Lapsed:
		if mutates {
			return NewError(Unmodified.TypeID(), nil)
		}
		return NewError(Cancelled.TypeID(), nil)
	case mesh.NoResponse:
		return NewError(Cancelled.TypeID(), nil)
	case mesh.Unknown:
		return NewError(Unverified.TypeID(), nil)
	}
	if r.AppResponse.Rejected {
		return NewError(Rejected.TypeID(), nil)
	}
	return r.AppResponse.Err
}

// Arrive is the error of a service call as the calling routine at loc receives it.
func Arrive(err error, module string, loc LocationID, raiser string) RitchieError {
	e := err.(RitchieError)
	if !e.raised {
		e = Raise(e, loc, raiser)
		e.arrived = true
		return e
	}
	return Cross(e, module)
}

// Leave is the error of a service call as it leaves the calling routine at loc where a system error that Arrive raised adds no step and a transported error records its propagation.
func Leave(e RitchieError, loc LocationID) RitchieError {
	if e.arrived {
		e.arrived = false
		return e
	}
	return Bubble(e, loc)
}

// Category is the outcome category of a call outcome that the caller acts on.
type Category uint8

const (
	Verdict Category = iota
	RequestFailure
	ResponseFailure
)

// CategoryOf is the outcome category of a call with a response whose error has the catalog or declared name, where the empty name is a value: a request failure did not take effect, a response failure may have, and every other outcome is a verdict.
func CategoryOf(name string) Category {
	switch name {
	case Unavailable.Name(), Conflict.Name(), Unmodified.Name(), Cancelled.Name():
		return RequestFailure
	case Unverified.Name():
		return ResponseFailure
	}
	return Verdict
}

// NoResponseError is the name of the error of a sent call that settled without a usable outcome, which follows from whether the method can mutate.
func NoResponseError(mutates bool) string {
	return OutcomeName(mesh.Response{}, mutates)
}

// OutcomeName is the name of the error that a caller of a method receives for a response, or the empty name when the response carries a result.
func OutcomeName(r mesh.Response, mutates bool) string {
	return outcomeName(failureOf(r, mutates))
}

// outcomeName is the name of the error a caller receives, or the empty name for nil.
func outcomeName(err error) string {
	if err == nil {
		return ""
	}
	return errorTypes.at(err.(RitchieError).typ).Name
}
