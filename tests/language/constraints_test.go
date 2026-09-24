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

func TestGenericInterfaceConstraints(t *testing.T) {
	runMatchSource(t, `namespace main
interface Identifiable {func getId() int}
interface Labelled {func label() string}
interface Reader[V any] {func read(fallback []V=[]V{}) V}
type Entity interface { Identifiable; Labelled }
type Readable[V any] interface { Entity; Reader[V] }
class User implements Identifiable, Labelled, Reader[int] {
 public func getId() int {return 7}
 public func label() string {return "Ada"}
 public func read(fallback []int=[]int{}) int {return 9+len(fallback)}
}
class Repository[T Identifiable] {
 public func identify(item T) int {return item.getId()}
}
func describe[T Entity](item T) { cb:=item.label; println(item.getId(),cb()) }
func read[V any,T Readable[V]](item T) V {return item.read()}
func inline[T interface {Identifiable; Labelled}](item T) string {return item.label()}
func direct[T Reader[int]](item T) int {return item.read()}
func main() {
 user:=new User()
 println(new Repository[User]().identify(user))
 describe(user)
 println(read[int](user),direct(user),inline(user))
}
`, "7\n7 Ada\n9 9 Ada\n")
}

func TestGenericInterfaceConstraintRejections(t *testing.T) {
	for name, source := range map[string]string{
		"missing method":           `class User {};func main(){_=new Repository[User]()}`,
		"wrong result":             `class User {public func getId() string{return "bad"}};func main(){_=new Repository[User]()}`,
		"private method":           `class User {private func getId() int{return 1}};func main(){_=new Repository[User]()}`,
		"insufficient constraint":  `class Child[T any] extends Repository[T] {};func main(){}`,
		"unavailable method":       `func bad[T Identifiable](value T) string{return value.label()};func main(){}`,
		"missing composite method": `interface Labelled {func label() string};type Entity interface {Identifiable;Labelled};class User {public func getId() int{return 1}};func describe[T Entity](value T)string{return value.label()};func main(){_=describe(new User())}`,
		"wrong generic bound":      `interface Reader[T any]{func read()T};type Bound interface {Reader[int]};class User {public func read()string{return "bad"}};func read[T Bound](value T)int{return value.read()};func main(){_=read(new User())}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			prefix := "namespace main\ninterface Identifiable {func getId() int}\nclass Repository[T Identifiable] {}\n"
			if err := os.WriteFile(filepath.Join(dir, "main.ghi"), []byte(prefix+source), 0600); err != nil {
				t.Fatal(err)
			}
			if err := compiler.Check(context.Background(), compiler.Options{Dir: dir}); err == nil {
				t.Fatal("invalid constraint accepted")
			}
		})
	}
}

func TestGenericConstraintsAcrossNamespaces(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"main.ghi", "contracts/entity.ghi"} {
		source, err := os.ReadFile(filepath.Join("..", "..", "examples", "constraints", name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, source, 0600); err != nil {
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
	if strings.TrimSpace(string(out)) != "7 Ada Ada" {
		t.Fatalf("output: %s", out)
	}
}
