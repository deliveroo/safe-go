// Package safe provides helpers for gracefully handling panics in background
// goroutines.
package safe

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

// pkgError represents an error returned from pkg/errors containing a stack
// trace.
type pkgError interface {
	error
	fmt.Formatter
	StackTrace() errors.StackTrace
}

// PanicError is an error that wraps a panic value. It also embeds a pkg/errors
// error to ensure the stack trace is properly captured and rendered to any
// error reporters.
type PanicError struct {
	pkgError             // embedded pkg/errors error with stack trace
	val      interface{} // panic value
}

// Panic returns the underlying value passed to panic().
func (p PanicError) Panic() interface{} {
	return p.val
}

// panicError creates a new PanicError for the given panic value.
func panicError(val interface{}) error {
	// Generate a pkg/errors error to capture the stack trace.
	err := errors.Errorf("panic: %v", val).(pkgError)
	return PanicError{err, val}
}

// Do executes fn. If a panic occurs, it will be recovered and returned as a
// safe.PanicError.
func Do(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError(r)
		}
	}()
	return fn()
}

// DoWithResult executes fn. If a panic occurs, it will be recovered and
// returned as a safe.PanicError.
func DoWithResult(fn func() (interface{}, error)) (res interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError(r)
		}
	}()
	return fn()
}

// Go executes fn in a background goroutine. If a panic occurs, it will be
// recovered and passed to the global panic handler.
func Go(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				handlePanic(r)
			}
		}()
		fn()
	}()
}

// A Group is a drop-in replacement for errgroup.Group, a collection of
// goroutines working on subtasks that are part of the same overall task. If any
// panics occur, they will be recovered and returned as a safe.PanicError.
//
// A zero Group is valid and does not cancel on error.
type Group struct {
	g    *errgroup.Group
	once sync.Once
}

// GroupWithContext returns a new Group and an associated Context derived from
// ctx.
//
// The derived Context is canceled the first time a function passed to Go
// returns a non-nil error or the first time Wait returns, whichever occurs
// first.
func GroupWithContext(ctx context.Context) (*Group, context.Context) {
	g, ctx := errgroup.WithContext(ctx)
	return &Group{g: g}, ctx
}

func (g *Group) init() {
	g.once.Do(func() {
		if g.g == nil {
			g.g = &errgroup.Group{}
		}
	})
}

// Go calls the given function in a new goroutine.
//
// The first call to panic or return a non-nil error cancels the group; its
// error will be returned by Wait.
func (g *Group) Go(fn func() error) {
	g.init()
	g.g.Go(func() error {
		return Do(fn)
	})
}

// Wait blocks until all function calls from the Go method have returned, then
// returns the first non-nil error (if any) from them.
func (g *Group) Wait() error {
	g.init()
	return g.g.Wait()
}

var panicHandler atomic.Value // global panic handler

// SetPanicHandler configures a global handler for any panics that occur in
// background goroutines spawned by safe.Go. If unset, they'll instead be
// written directly to the log.
func SetPanicHandler(fn func(err error)) {
	panicHandler.Store(fn)
}

func handlePanic(val interface{}) {
	err := panicError(val)
	fn, _ := panicHandler.Load().(func(err error))
	if fn == nil {
		log.Printf("%+v\n", err)
		return
	}

	// Catch panics in the panic handler.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in panic handler: %+v\noriginal: %+v\n", panicError(r), err)
		}
	}()
	fn(err)
}
