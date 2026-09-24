package language_test

import (
	"context"
	"ghi/internal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericInheritanceDispatch(t *testing.T) {
	runMatchSource(t, `namespace main
class Base[T any] {
 protected value T
 constructor(value T) { this.value = value }
 public func get() T { return this.value }
 public func set(value T) { this.value = value }
 public func read() T { return this.get() }
}

class Middle[U any] extends Base[[]U] {
 constructor(values []U) { parent(values) }
}
class Numbers extends Middle[int] {
 constructor() { parent([]int{7}) }
 public override func get() []int { return append(parent.get(), 8) }
}
class Plain { public func name() string {return "plain"} }
class Generic[T any] extends Plain {}
func main() {
 n := new Numbers()
 var b Base[[]int] = n
 println(b.read()[1])
 b.set([]int{4})
 println(n.get()[0])
 println(new Generic[int]().name())
}
`, "8\n4\nplain\n")
}

func TestGenericInheritanceRejectsInvalidTypes(t *testing.T) {
	for _, source := range []string{
		`class Base[T any]{};class Child extends Base{};func main(){}`,
		`class Base{};class Child extends Base[int]{};func main(){}`,
		`class Base[T comparable]{};class Child[U any] extends Base[U]{};func main(){}`,
		`class Base[T any]{public func get()T{panic("unused")}};class Child extends Base[int]{public override func get()string{return "wrong"}};func main(){}`,
		`class Base[T any]{};class Child extends Base[int]{};func main(){var b Base[string]=new Child();_=b}`,
		`class Base[T any]{protected value T;constructor(value T){this.value=value}};class Child extends Base[int]{constructor(){parent("wrong")}};func main(){}`,
	} {
		t.Run(source, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.ghi"), []byte("namespace main\n"+source), 0600); err != nil {
				t.Fatal(err)
			}
			if err := compiler.Check(context.Background(), compiler.Options{Dir: dir}); err == nil {
				t.Fatal("invalid inheritance accepted")
			}
		})
	}
}

func TestGenericInheritanceNamespacesAndDefaults(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"model/item.ghi": "namespace model\ntype Item int\n",
		"factory/repo.ghi": `namespace factory
import library.Base
import model
class Concrete extends Base[model.Item] {}
func Create() Concrete {return new Concrete()}
`,
		"library/base.ghi": `namespace library
type Count int
class Base[T any] {
 protected items []T
 constructor() { this.items=[]T{} }
 public func add(value T, copies int=2) { for i:=0;i<copies;i++ { this.items=append(this.items,value) } }
 public func count() Count { return Count(len(this.items)) }
	public func get() []T { return this.items }
	public func empty(values []T=[]T{}, count Count=Count(1)) int { return len(values)+int(count) }
	public func captured(cb func() int=func() int { T := func() int {return 9}; return T() }) int { return cb() }
}
class Empty[T any] { constructor(values []T=[]T{}){} }
`,
		"app/child.ghi": `namespace app
import library.Base
import library.Empty
class User {public name string;constructor(name string){this.name=name}}
class Users extends Base[User] {
 public override func get() []User {return parent.get()}
}
class Defaults extends Empty[int] {}
`,
		"main.ghi": `namespace main
import app.Users
import app.User
import app.Defaults
import factory
func main(){r:=new Users();r.add(new User("Ada"));println(r.count(),r.get()[1].name,r.empty(),r.captured());_=new Defaults();println(factory.Create().count(),factory.Create().captured())}
`,
	}
	for name, source := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := compiler.Build(context.Background(), compiler.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "2 Ada 1 9\n0 9" {
		t.Fatalf("got %s", out)
	}
}
