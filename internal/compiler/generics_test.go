package compiler

import (
	"context"
	"testing"
)

func TestGenericClassesFunctionsAndNamespaces(t *testing.T) {
	got := runProgram(t, map[string]string{
		"main.ghi": `namespace main
import fmt "go:fmt"
import containers "app.containers"
func first[T any](value T, count int = 2) T {return value}
func read[T containers.Reader[string]](value T) string {return value.get()}
type Values[T any] []T
type Number interface { ~int | ~int64 }
func twice[T Number](value T) T {return value+value}
func triple[T interface { ~int | ~int64 }](value T) T {return value+value+value}
func main(){
 box:=containers.Box[string]("hello")
 box.set("world")
 var reader containers.Reader[string]=box
 values:=Values[int]{1,2}
 fmt.Println(reader.get(),first[int](values[0]),read(box),twice(2),triple(int64(2)))
}
`,
		"containers/box.ghi": `namespace app.containers
interface Reader[T any] {func get() T}
class Box[T any] implements Reader[T] {
 private value T
 constructor(value T){this.value=value}
 public func get() T{return this.value}
 public func set(value T){this.value=value}
}
`,
	})
	if got != "world 1 world 4 6\n" {
		t.Fatalf("output %q", got)
	}
}

func TestGenericsRejectInvalidTypes(t *testing.T) {
	for name, source := range map[string]string{
		"constraint":                  `class Box[T comparable] {constructor(value T){}};func main(){_=Box[[]int]([]int{})}`,
		"wrong argument":              `class Box[T any] {constructor(value T){}};func main(){_=Box[int]("wrong")}`,
		"wrong result":                `func bad[T any](value T) T{return "wrong"};func main(){_=bad[int](1)}`,
		"uninitialized generic field": `class Box[T any] {public value T};func main(){_=Box[User]()}`,
		"uninitialized generic local": `func zero[T any]() T{var value T;return value};func main(){_=zero[User]()}`,
		"wrong interface argument":    `interface Reader[T any]{func get()T};class Box[T any] implements Reader[int]{public func get()T{panic("unused")}};func main(){_=Box[string]()}`,
		"phantom invariance":          `class Box[T any]{};func main(){var box Box[int]=Box[string]();_=box}`,
		"generic parent":              `class Base[T any]{};class Child extends Base[int]{};func main(){_=Child()}`,
		"nongeneric indexed parent":   `class Base{};class Child extends Base[int]{};func main(){_=Child()}`,
		"generic aggregate field":     `type Wrapped[T any] struct{value T};class Holder {public wrapped Wrapped[User]};func main(){_=Holder()}`,
		"generic nil default":         `func bad[T any](value T=nil){};func main(){}`,
	} {
		t.Run(name, func(t *testing.T) {
			err := Check(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\nclass User {}\n" + source})})
			if err == nil {
				t.Fatal("invalid generic program accepted")
			}
		})
	}
}

func TestGenericRepositoryPreservesAbsentValues(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User {public name string;constructor(name string){this.name=name}}
class Repository[K comparable,V any] {
 private values map[K]V
 constructor(){this.values=make(map[K]V)}
 public func put(key K,value V){this.values[key]=value}
 public func find(key K) ?V {return this.values[key]}
}
func main(){
 repository:=Repository[string,User]()
 repository.put("ada",User("Ada"))
 found:=repository.find("ada")
 if found!=nil{fmt.Println(found.name)}
 fmt.Println(repository.find("missing")==nil)
 numbers:=Repository[string,int]()
 numbers.put("zero",0)
 number:=numbers.find("zero")
 if number!=nil{fmt.Println(*number)}
 fmt.Println(numbers.find("missing")==nil)
}
`})
	if got != "Ada\ntrue\n0\ntrue\n" {
		t.Fatalf("output %q", got)
	}
}
