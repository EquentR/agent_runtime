package logics

type ExampleLogic struct{}

func (e *ExampleLogic) SayHello(name string) string {
	return "Hello " + name
}
