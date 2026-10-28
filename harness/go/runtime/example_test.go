package runtime_test

import (
	"context"
	"fmt"

	lifecycle "github.com/phodal/pero/harness/go/runtime"
)

type greeter interface {
	Greet(string) string
}

type greeterService struct{}

func (greeterService) Greet(name string) string { return "hello, " + name }

func Example() {
	greeterKey := lifecycle.NewKey[greeter]("example.greeter")
	runtime := lifecycle.New()

	provider := lifecycle.Define(
		lifecycle.Descriptor{ID: "example/greeter", Provides: []lifecycle.AnyKey{greeterKey.Any()}},
		lifecycle.DecodeJSON[struct{}],
		func(run *lifecycle.Run, _ struct{}) (lifecycle.Dispose, error) {
			_, err := lifecycle.Provide[greeter](run, greeterKey, greeterService{})
			return nil, err
		},
	)
	consumer := lifecycle.Define(
		lifecycle.Descriptor{ID: "example/app", Requires: []lifecycle.Requirement{lifecycle.Require(greeterKey)}},
		lifecycle.DecodeJSON[struct{}],
		func(run *lifecycle.Run, _ struct{}) (lifecycle.Dispose, error) {
			service, err := lifecycle.Resolve(run, greeterKey)
			if err != nil {
				return nil, err
			}
			fmt.Println(service.Greet("Point"))
			return nil, nil
		},
	)

	app, _ := runtime.Mount(nil, "app", consumer, nil)
	greeterInstance, _ := runtime.Mount(nil, "greeter", provider, nil)
	_ = greeterInstance.WaitStable(context.Background())
	_ = app.WaitStable(context.Background())
	_ = runtime.Dispose(context.Background())

	// Output: hello, Point
}
