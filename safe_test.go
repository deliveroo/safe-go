package safe_test

import (
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/deliveroo/assert-go"
	"github.com/deliveroo/safe-go"
)

func ExampleDo() {
	getName := func() string {
		panic("unhandled error")
	}
	err := safe.Do(func() error {
		_ = getName()
		return nil
	})

	if err, ok := err.(safe.PanicError); ok {
		fmt.Println(err)
	}
	// Output:
	// panic: unhandled error
}

func ExampleDoWithResult() {
	getName := func() string {
		panic("unhandled error")
	}
	_, err := safe.DoWithResult(func() (interface{}, error) {
		_ = getName()
		return nil, nil
	})

	if err, ok := err.(safe.PanicError); ok {
		fmt.Println(err)
	}
	// Output:
	// panic: unhandled error
}

func ExampleDoWithResult_ok() {
	getName := func() string {
		return "OK"
	}
	res, err := safe.DoWithResult(func() (interface{}, error) {
		return getName(), nil
	})

	if err, ok := err.(safe.PanicError); ok {
		fmt.Println(err)
	} else {
		fmt.Println(res.(string))
	}
	// Output:
	// OK
}

func ExampleGo() {
	safe.SetPanicHandler(func(err error) {
		fmt.Println(err)
	})
	var wg sync.WaitGroup
	wg.Add(1)
	safe.Go(func() {
		defer wg.Done()
		panic("unhandled error")
	})
	wg.Wait()
	// Output:
	// panic: unhandled error
}

func ExampleGroup() {
	var g safe.Group
	g.Go(func() error {
		panic("unhandled error")
	})
	err := g.Wait()
	fmt.Printf("(%T) %v\n", err, err)
	// Output:
	// (safe.PanicError) panic: unhandled error
}

type testError string

func (err testError) Error() string {
	return string(err)
}

func TestDo(t *testing.T) {
	t.Run("with panic", func(t *testing.T) {
		err := safe.Do(func() error {
			panic("internal error")
		})
		if err == nil {
			t.Fatal("expected error")
		}
		assert.Equal(t, err.Error(), "panic: internal error")
		panicErr, ok := err.(safe.PanicError)
		if !ok {
			t.Fatal("err not a safe.PanicError")
		}
		assert.Equal(t, panicErr.Panic(), "internal error")
	})

	t.Run("with error", func(t *testing.T) {
		err := safe.Do(func() error {
			return testError("internal error")
		})
		assert.Equal(t, err.Error(), "internal error")
		assert.Equal(t, reflect.TypeOf(err).String(), "safe_test.testError")
	})
}

func TestDoWithResult(t *testing.T) {
	t.Run("with panic", func(t *testing.T) {
		_, err := safe.DoWithResult(func() (interface{}, error) {
			panic("internal error")
		})
		if err == nil {
			t.Fatal("expected error")
		}
		assert.Equal(t, err.Error(), "panic: internal error")
		panicErr, ok := err.(safe.PanicError)
		if !ok {
			t.Fatal("err not a safe.PanicError")
		}
		assert.Equal(t, panicErr.Panic(), "internal error")
	})

	t.Run("with error", func(t *testing.T) {
		_, err := safe.DoWithResult(func() (interface{}, error) {
			return nil, testError("internal error")
		})
		assert.Equal(t, err.Error(), "internal error")
		assert.Equal(t, reflect.TypeOf(err).String(), "safe_test.testError")
	})

	t.Run("with result", func(t *testing.T) {
		res, err := safe.DoWithResult(func() (interface{}, error) {
			return "foo", nil
		})
		if err != nil {
			t.Fatal("expected no error")
		}
		assert.Equal(t, res.(string), "foo")
	})
}

func TestGroup(t *testing.T) {
	var g safe.Group
	g.Go(func() error { return nil })
	g.Go(func() error { panic("internal error") })
	err := g.Wait()
	if err == nil {
		t.Fatal("expected error")
	}
	assert.Equal(t, err.Error(), "panic: internal error")
	panicErr, ok := err.(safe.PanicError)
	if !ok {
		t.Fatal("err not a safe.PanicError")
	}
	assert.Equal(t, panicErr.Panic(), "internal error")
}

func TestGo(t *testing.T) {
	// Note: these tests necessarily mutate global state, and cannot be run in
	// parallel.
	t.Run("no handler", func(t *testing.T) {
		safe.SetPanicHandler(nil)
		var b logBuffer
		log.SetOutput(&b)
		log.SetFlags(0)
		b.Add()
		safe.Go(func() {
			panic("internal error")
		})
		b.Wait()
		assert.True(t, strings.HasPrefix(b.String(), "panic: internal error"))
		assert.Contains(t, b.String(), "safe-go_test.TestGo") // contains a stack trace
	})

	t.Run("with handler", func(t *testing.T) {
		var wg sync.WaitGroup

		// Set up a panic handler.
		wg.Add(1)
		var handledErr error
		safe.SetPanicHandler(func(err error) {
			handledErr = err
			wg.Done()
		})

		// Run a background goroutine that panics.
		wg.Add(1)
		safe.Go(func() {
			defer wg.Done()
			panic("internal error")
		})

		wg.Wait() // wait for goroutine and panic handler

		// Assert the panic was passed to the panic handler.
		panicErr, ok := handledErr.(safe.PanicError)
		if !ok {
			t.Fatal("err not a safe.PanicError")
		}
		assert.Equal(t, panicErr.Panic(), "internal error")
	})

	t.Run("panic in handler", func(t *testing.T) {
		var b logBuffer
		log.SetOutput(&b)
		log.SetFlags(0)

		// Configure a panic handler that panics.
		var wg sync.WaitGroup
		wg.Add(1)
		safe.SetPanicHandler(func(err error) {
			defer wg.Done()
			panic("worst panic handler")
		})

		// Run a background goroutine that panics.
		wg.Add(1)
		b.Add()
		safe.Go(func() {
			defer wg.Done()
			panic("internal error")
		})
		wg.Wait() // wait for goroutine and panic handler
		b.Wait()  // wait for log to be written

		assert.Contains(t, b.String(), "panic in panic handler") // prefix
		assert.Contains(t, b.String(), "worst panic handler")    // panic handler panic
		assert.Contains(t, b.String(), "internal error")         // original panic
		assert.Contains(t, b.String(), "safe-go_test.TestGo")    // stack trace
	})
}

// logBuffer wraps strings.Builder with a WaitGroup, allowing code to wait for a
// background goroutine to write a log message.
type logBuffer struct {
	sb strings.Builder
	wg sync.WaitGroup
}

// Add adds an expectation that a single log line will be written.
func (l *logBuffer) Add() {
	l.wg.Add(1)
}

// Wait waits up to 1 second for a log to be written.
func (l *logBuffer) Wait() {
	done := make(chan struct{})
	go func() {
		select {
		case <-time.After(time.Second):
			panic("wait timeout: no log written after 1 second")
		case <-done:
		}
	}()
	l.wg.Wait()
	close(done)
}

func (l *logBuffer) Write(b []byte) (int, error) {
	defer l.wg.Done()
	return l.sb.Write(b)
}

func (l *logBuffer) String() string {
	return l.sb.String()
}
