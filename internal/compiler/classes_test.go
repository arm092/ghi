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
    user := new User("Arman")
    alias := user
    alias.age = 21
    fmt.Println(describe(user), viaInterface(user), user.age)
    fmt.Println(new Person().label())
}
`})
	if got != "hello Arman! Arman! 21\nanonymous\n" {
		t.Fatalf("output %q", got)
	}
}

func TestClassRulesRejectInvalidPrograms(t *testing.T) {
	for name, body := range map[string]string{
		"private access":           `class Box { secret int }; func main(){ b:=new Box(); println(b.secret) }`,
		"invalid override":         `class Box { public override func missing() {} }; func main(){}`,
		"missing parent arguments": `class Parent { constructor(n int) {} }; class Child extends Parent { constructor() {} }; func main(){}`,
		"interface missing method": `interface Named { func name() string }; class Item implements Named {}; func main(){}`,
		"overload":                 `class Item { func f() {}; func f(n int) {} }; func main(){}`,
		"private interface":        `interface Named { private func name() string }; func main(){}`,
		"protected interface":      `interface Named { protected func name() string }; func main(){}`,
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

func TestConstructionSupportsOptionalNew(t *testing.T) {
	for name, fixture := range map[string]struct{ source, diagnostic string }{
		"function target":  {`func create()int{return 1};func main(){_=new create()}`, "new requires a Ghi class"},
		"primitive target": {`func main(){_=new int()}`, "new requires a Ghi class"},
		"interface target": {`interface Named {func name()string};func main(){_=new Named()}`, "cannot instantiate interface"},
	} {
		t.Run(name, func(t *testing.T) {
			err := Check(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\n" + fixture.source})})
			if err == nil || !strings.Contains(err.Error(), fixture.diagnostic) {
				t.Fatalf("wanted %q, got %v", fixture.diagnostic, err)
			}
		})
	}
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User {public name string;constructor(name string="default"){this.name=name}}
func main(){
 value:=new(int)
 *value=7
 fmt.Println(new User().name,User().name,new User("Ada").name,User("Ada").name,*value,"new User()")
}
`})
	if got != "default default Ada Ada 7 new User()\n" {
		t.Fatalf("output %q", got)
	}
}

func TestOverrideResultNamesAreNotPartOfSignature(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
interface Named { func name() (label string) }
class Parent { public func name() (result string) { return "parent" } }
class Child extends Parent implements Named {
 public override func name() string { return "child" }
}
func main() { var p Parent = new Child(); fmt.Println(p.name()) }
`})
	if got != "child\n" {
		t.Fatalf("output %q", got)
	}
}

func TestNamespaceIdentityCannotCollide(t *testing.T) {
	_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{
		"main.ghi": `namespace main
import a.b as one
import a_b as two
func main() { var wrong one.Item = new two.Item(); _ = wrong }
`,
		"one/item.ghi": "namespace a.b\nclass Item {}",
		"two/item.ghi": "namespace a_b\nclass Item {}",
	})})
	if err == nil {
		t.Fatal("unrelated nominal classes accepted as the same type")
	}
}

func TestCrossNamespaceInheritanceAndFieldTypes(t *testing.T) {
	got := runProgram(t, map[string]string{
		"main.ghi": `namespace main
import fmt "go:fmt"
import app.users
import app.models
func render(p models.Person) string { return p.greet() }
func main() { user:=new users.User("Ada"); fmt.Println(render(user),user.friend().label()) }
`,
		"models/person.ghi": `namespace app.models
class Person {
 protected name string
 constructor(name string = "friend") { this.name = name }
 public func label() string { return this.name }
 public func greet() string { return this.label() }
 public func friend() Person { return new Person() }
}

`,
		"users/user.ghi": `namespace app.users
import app.models
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
