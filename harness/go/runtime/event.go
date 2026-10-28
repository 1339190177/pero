package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var nextEventID atomic.Uint64

type ListenerOptions struct {
	Prepend bool
	Global  bool
	Filter  func(context.Context, *Scope, *Instance) bool
}

type listenerEntry[L any] struct {
	id       uint64
	listener L
}

type listenerSnapshot[L any] struct {
	listeners []listenerEntry[L]
}

type listenerStore[L any] struct {
	mu      sync.Mutex
	nextID  uint64
	current atomic.Pointer[listenerSnapshot[L]]
}

func (s *listenerStore[L]) add(listener L, prepend bool) (uint64, Dispose) {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	entry := listenerEntry[L]{id: id, listener: listener}
	current := s.load()
	listeners := make([]listenerEntry[L], 0, len(current)+1)
	if prepend {
		listeners = append(listeners, entry)
		listeners = append(listeners, current...)
	} else {
		listeners = append(listeners, current...)
		listeners = append(listeners, entry)
	}
	s.current.Store(&listenerSnapshot[L]{listeners: listeners})
	s.mu.Unlock()

	var once sync.Once
	return id, func(context.Context) error {
		once.Do(func() { s.remove(id) })
		return nil
	}
}

func (s *listenerStore[L]) remove(id uint64) {
	s.mu.Lock()
	current := s.load()
	for index := range current {
		if current[index].id != id {
			continue
		}
		listeners := make([]listenerEntry[L], 0, len(current)-1)
		listeners = append(listeners, current[:index]...)
		listeners = append(listeners, current[index+1:]...)
		s.current.Store(&listenerSnapshot[L]{listeners: listeners})
		break
	}
	s.mu.Unlock()
}

func (s *listenerStore[L]) snapshot() []listenerEntry[L] {
	return s.load()
}

func (s *listenerStore[L]) load() []listenerEntry[L] {
	current := s.current.Load()
	if current == nil {
		return nil
	}
	return current.listeners
}

func listenerOption(options []ListenerOptions) ListenerOptions {
	if len(options) == 0 {
		return ListenerOptions{}
	}
	return options[0]
}

func listenerAllowed(ctx context.Context, option ListenerOptions) bool {
	if option.Global || option.Filter == nil {
		return true
	}
	scope, _ := ScopeFromContext(ctx)
	instance, _ := InstanceFromContext(ctx)
	return option.Filter(ctx, scope, instance)
}

func eventContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func validateEventRun(run *Run, runtime *Runtime, name string) error {
	if run == nil || !run.Active() {
		return ErrInactiveRun
	}
	if runtime == nil || run.Scope == nil || run.Scope.runtime != runtime {
		return fmt.Errorf("runtime: event %q belongs to another runtime", name)
	}
	return nil
}

// Signal is a typed Cordis-style event. Emit invokes listeners in registration
// order and stops on the first error; Parallel waits for every listener and
// joins all errors. Listener registrations are owned by their Run.
type Signal[T any] struct {
	id      uint64
	name    string
	runtime *Runtime
	store   listenerStore[func(context.Context, T) error]
}

func NewSignal[T any](runtime *Runtime, name string) *Signal[T] {
	if runtime == nil {
		panic("runtime: signal runtime must not be nil")
	}
	if name == "" {
		panic("runtime: signal name must not be empty")
	}
	return &Signal[T]{id: nextEventID.Add(1), name: name, runtime: runtime}
}

func (s *Signal[T]) Name() string { return s.name }

func (s *Signal[T]) On(run *Run, listener func(context.Context, T) error, options ...ListenerOptions) (Dispose, error) {
	if s == nil || listener == nil {
		return nil, errors.New("runtime: signal and listener must not be nil")
	}
	if err := validateEventRun(run, s.runtime, s.name); err != nil {
		return nil, err
	}
	option := listenerOption(options)
	wrapped := func(ctx context.Context, value T) error {
		if !listenerAllowed(ctx, option) {
			return nil
		}
		return listener(ctx, value)
	}
	_, dispose := s.store.add(wrapped, option.Prepend)
	if err := run.Defer("signal:"+s.name, dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}

func (s *Signal[T]) Once(run *Run, listener func(context.Context, T) error, options ...ListenerOptions) (Dispose, error) {
	if listener == nil {
		return nil, errors.New("runtime: signal listener must not be nil")
	}
	type onceDispose struct{ dispose Dispose }
	var holder atomic.Pointer[onceDispose]
	var fired atomic.Bool
	wrapper := func(ctx context.Context, value T) error {
		if !fired.CompareAndSwap(false, true) {
			return nil
		}
		if current := holder.Load(); current != nil {
			_ = current.dispose(context.Background())
		}
		return listener(ctx, value)
	}
	registered, err := s.On(run, wrapper, options...)
	if err != nil {
		return nil, err
	}
	holder.Store(&onceDispose{dispose: registered})
	if fired.Load() {
		_ = registered(context.Background())
	}
	return registered, nil
}

func (s *Signal[T]) Emit(ctx context.Context, value T) error {
	if s == nil {
		return errors.New("runtime: signal must not be nil")
	}
	ctx = eventContext(ctx)
	for _, entry := range s.store.snapshot() {
		if err := callSignalListener(s.name, entry.listener, ctx, value); err != nil {
			return err
		}
	}
	return nil
}

func (s *Signal[T]) Serial(ctx context.Context, value T) error {
	return s.Emit(ctx, value)
}

// EmitAll invokes listeners in registration order, contains each listener
// failure, and joins the failures after every observer has run. Lifecycle
// notifications use it when one broken observer must not starve later ones.
func (s *Signal[T]) EmitAll(ctx context.Context, value T) error {
	if s == nil {
		return errors.New("runtime: signal must not be nil")
	}
	ctx = eventContext(ctx)
	var errs []error
	for _, entry := range s.store.snapshot() {
		if err := callSignalListener(s.name, entry.listener, ctx, value); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Signal[T]) Parallel(ctx context.Context, value T) error {
	if s == nil {
		return errors.New("runtime: signal must not be nil")
	}
	ctx = eventContext(ctx)
	listeners := s.store.snapshot()
	errs := make([]error, len(listeners))
	var wait sync.WaitGroup
	wait.Add(len(listeners))
	for index, entry := range listeners {
		go func() {
			defer wait.Done()
			errs[index] = callSignalListener(s.name, entry.listener, ctx, value)
		}()
	}
	wait.Wait()
	return errors.Join(errs...)
}

func callSignalListener[T any](name string, listener func(context.Context, T) error, ctx context.Context, value T) (err error) {
	defer recoverNamedError("signal", name, &err)
	return listener(ctx, value)
}

// Query is the typed equivalent of Cordis serial/bail dispatch. A handler sets
// handled=true to stop propagation; the zero value can therefore be a valid
// answer without relying on JavaScript truthiness.
type Query[I, O any] struct {
	id      uint64
	name    string
	runtime *Runtime
	store   listenerStore[func(context.Context, I) (O, bool, error)]
}

func NewQuery[I, O any](runtime *Runtime, name string) *Query[I, O] {
	if runtime == nil {
		panic("runtime: query runtime must not be nil")
	}
	if name == "" {
		panic("runtime: query name must not be empty")
	}
	return &Query[I, O]{id: nextEventID.Add(1), name: name, runtime: runtime}
}

func (q *Query[I, O]) Name() string { return q.name }

func (q *Query[I, O]) On(run *Run, handler func(context.Context, I) (O, bool, error), options ...ListenerOptions) (Dispose, error) {
	if q == nil || handler == nil {
		return nil, errors.New("runtime: query and handler must not be nil")
	}
	if err := validateEventRun(run, q.runtime, q.name); err != nil {
		return nil, err
	}
	option := listenerOption(options)
	wrapped := func(ctx context.Context, input I) (O, bool, error) {
		if !listenerAllowed(ctx, option) {
			var zero O
			return zero, false, nil
		}
		return handler(ctx, input)
	}
	_, dispose := q.store.add(wrapped, option.Prepend)
	if err := run.Defer("query:"+q.name, dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}

func (q *Query[I, O]) Bail(ctx context.Context, input I) (O, bool, error) {
	var zero O
	if q == nil {
		return zero, false, errors.New("runtime: query must not be nil")
	}
	ctx = eventContext(ctx)
	for _, entry := range q.store.snapshot() {
		value, handled, err := callQueryHandler(q.name, entry.listener, ctx, input)
		if err != nil || handled {
			return value, handled, err
		}
	}
	return zero, false, nil
}

func (q *Query[I, O]) Serial(ctx context.Context, input I) (O, bool, error) {
	return q.Bail(ctx, input)
}

func callQueryHandler[I, O any](name string, handler func(context.Context, I) (O, bool, error), ctx context.Context, input I) (value O, handled bool, err error) {
	defer recoverNamedError("query", name, &err)
	return handler(ctx, input)
}

type Next[T any] func(T) (T, error)
type Middleware[T any] func(context.Context, T, Next[T]) (T, error)

// Pipeline is the typed equivalent of Cordis waterfall. Middleware may call
// next with a transformed value or short-circuit by returning directly.
type Pipeline[T any] struct {
	id      uint64
	name    string
	runtime *Runtime
	store   listenerStore[Middleware[T]]
}

func NewPipeline[T any](runtime *Runtime, name string) *Pipeline[T] {
	if runtime == nil {
		panic("runtime: pipeline runtime must not be nil")
	}
	if name == "" {
		panic("runtime: pipeline name must not be empty")
	}
	return &Pipeline[T]{id: nextEventID.Add(1), name: name, runtime: runtime}
}

func (p *Pipeline[T]) Name() string { return p.name }

func (p *Pipeline[T]) Use(run *Run, middleware Middleware[T], options ...ListenerOptions) (Dispose, error) {
	if p == nil || middleware == nil {
		return nil, errors.New("runtime: pipeline and middleware must not be nil")
	}
	if err := validateEventRun(run, p.runtime, p.name); err != nil {
		return nil, err
	}
	option := listenerOption(options)
	wrapped := func(ctx context.Context, input T, next Next[T]) (T, error) {
		if !listenerAllowed(ctx, option) {
			return next(input)
		}
		return middleware(ctx, input, next)
	}
	_, dispose := p.store.add(wrapped, option.Prepend)
	if err := run.Defer("pipeline:"+p.name, dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}

func (p *Pipeline[T]) Run(ctx context.Context, input T, terminal func(context.Context, T) (T, error)) (T, error) {
	if p == nil {
		return input, errors.New("runtime: pipeline must not be nil")
	}
	ctx = eventContext(ctx)
	if terminal == nil {
		terminal = func(_ context.Context, value T) (T, error) { return value, nil }
	}
	middleware := p.store.snapshot()
	var dispatch func(int, T) (T, error)
	dispatch = func(index int, value T) (result T, err error) {
		if index == len(middleware) {
			return terminal(ctx, value)
		}
		defer recoverNamedError("pipeline", p.name, &err)
		return middleware[index].listener(ctx, value, func(next T) (T, error) {
			return dispatch(index+1, next)
		})
	}
	return dispatch(0, input)
}
