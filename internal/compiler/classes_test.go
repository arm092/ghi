package compiler

import (
	"context"
	"strings"
	"testing"
)

func TestClassesPreserveDynamicReceiverAndConstructorDefaults(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"

interface Labelled { func label() string }

class Person {
    protected name string
    constructor(name string = "anonymous") { this.name = name }
    public func label() string { return this.name }
    public func greet() string { return "hello " + this.label() }
}
class User extends Person implements Labelled {
    public age int
    constructor(name string, age int = 18) {
        parent(name)
        this.age = age
    }
    public override func label() string { return parent.label() + "!" }
}
func describe(p Person) string { return p.greet() }
func viaInterface(p Labelled) string { return p.label() }
func main() {
    user := User("Arman")
    alias := user
    alias.age = 21
    fmt.Println(describe(user), viaInterface(user), user.age)
    fmt.Println(Person().label())
}
`})
	if got != "hello Arman! Arman! 21\nanonymous\n" {
		t.Fatalf("output %q", got)
	}
}

func TestClassRulesRejectInvalidPrograms(t *testing.T) {
	for name, body := range map[string]string{
		"private access":           `class Box { secret int }; func main(){ b:=Box(); println(b.secret) }`,
		"invalid override":         `class Box { public override func missing() {} }; func main(){}`,
		"missing parent arguments": `class Parent { constructor(n int) {} }; class Child extends Parent { constructor() {} }; func main(){}`,
		"interface missing method": `interface Named { func name() string }; class Item implements Named {}; func main(){}`,
		"overload":                 `class Item { func f() {}; func f(n int) {} }; func main(){}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\n" + body})})
			if err == nil {
				t.Fatal("invalid class accepted")
			}
			if strings.Contains(err.Error(), "expected declaration") {
				t.Fatalf("class syntax not implemented: %v", err)
			}
		})
	}
}

func TestCrossNamespaceInheritanceAndFieldTypes(t *testing.T) {
	got := runProgram(t, map[string]string{
		"main.ghi": `namespace main
import fmt "go:fmt"
import users "app.users"
import models "app.models"
func render(p models.Person) string { return p.greet() }
func main() { user:=users.User("Ada"); fmt.Println(render(user),user.friend().label()) }
`,
		"models/person.ghi": `namespace app.models
class Person {
 protected name string
 constructor(name string = "friend") { this.name = name }
 public func label() string { return this.name }
 public func greet() string { return this.label() }
 public func friend() Person { return Person() }
}

`,
		"users/user.ghi": `namespace app.users
import models "app.models"
class User extends models.Person {
 constructor(name string) { parent(name) }
 public override func label() string { return parent.label()+"!" }
}
`,
	})
	if got != "Ada! friend\n" {
		t.Fatalf("output %q", got)
	}
}
