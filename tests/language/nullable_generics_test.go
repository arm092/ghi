package language_test

import (
	"bytes"
	"context"
	"ghi/internal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNullableVariadicBoxing(t *testing.T) {
	runMatchSource(t, `namespace main
class User {public name string;constructor(name string){this.name=name}}
class Admin extends User {constructor(name string){parent(name)}}
func count(prefix int,users ...?User) int{return prefix+len(users)}
func generic[T any](users ...?T) int{return len(users)}
func main(){
 users:=[]?User{}
 users=append(users,new User("Ada"),new Admin("Arman"),nil)
 first:=users[0];if first!=nil{println(first.name)}
 println(count(10,new User("A"),new Admin("B"),nil),generic[User](new User("C"),nil))
 println(count(0,users...),len(append([]?User{},users...)))
 a,b:=1,2;ptrs:=[]*int{};ptrs=append(ptrs,&a,&b);println(ptrs[0]==&a,ptrs[1]==&b)
}
`, "Ada\n13 2\n3 3\ntrue true\n")
}

func TestNullableVariadicSpreadInvariance(t *testing.T) {
	for name, body := range map[string]string{
		"append": `users:=[]User{new User()};_=append([]?User{},users...)`,
		"call":   `users:=[]User{new User()};accept(users...)`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			source := "namespace main\nclass User {}\nfunc accept(users ...?User) {}\nfunc main(){" + body + "}\n"
			if err := os.WriteFile(filepath.Join(dir, "main.ghi"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if err := compiler.Check(context.Background(), compiler.Options{Dir: dir}); err == nil {
				t.Fatal("invariant spread slice accepted")
			}
		})
	}
}

func TestGenericNullableEquality(t *testing.T) {
	source := `namespace main
class User {}
func equal[T any](a ?T,b ?T) bool{return a==b}
func different[T any](a ?T,b ?T) bool{return a!=b}
func native[T any](a *T,b *T) bool{return a==b}
func wrapped[T any](value T) ?T{return value}
func returned[T any](value T) bool{return wrapped[T](value)==wrapped[T](value)}
func mapped[T any](value T) bool {values:=map[string]T{"a":value};return values["a"]==values["a"]}
func nativeBound[T User](a *T,b *T) bool{return a==b}
func nativeConcrete(a *User,b *User)bool{return a==b}
func closures[T any](value T)bool{nullable:=func() ?T{return value};native:=func() *T{var pointer ?T=value;return pointer};return nullable()==nullable() && native()!=native()}
class Compare[T any] {public func equal(a ?T,b ?T)bool{return a==b};public func native(a *T,b *T)bool{return a==b}}
class Pair[T any] {public a ?T;public b ?T;constructor(a ?T,b ?T){this.a=a;this.b=b};public func same()bool{return this.a==this.b}}
class Child[T any] extends Pair[T] {constructor(a ?T,b ?T){parent(a,b)}}
class Callback[T any] {public get func()?T;constructor(get func()?T){this.get=get};public func same()bool{return this.get()==this.get()}}
func main(){
 u:=new User();var a ?User=u;var b ?User=u;var empty ?User
 println(a==b,equal[User](a,b),equal[User](u,u),different[User](a,b))
 println(equal[User](a,empty),equal[User](empty,empty),different[User](a,empty))
 println(native[User](a,b),native[User](a,a),new Compare[User]().equal(a,b),new Compare[User]().native(a,b))
 println(returned[User](u),mapped[User](u),nativeBound[User](a,b),nativeConcrete(a,b))
 println(new Pair[User](u,u).same(),new Child[User](u,u).same())
 println(new Callback[User](func()?User{return u}).same())
 callback:=func(a ?User,b ?User)bool{return a==b}; nativeCallback:=func(a *User,b *User)bool{return a==b}
 println(callback(a,b),nativeCallback(a,b))
 println(closures[User](u))
 x,y:=7,7;println(equal[int](&x,&y),equal[int](&x,&x),native[int](&x,&y))
 xs,ys:=[]int{1},[]int{1};println(equal[[]int](&xs,&ys),equal[[]int](&xs,&xs))
}
`
	want := "true true true false\nfalse true true\nfalse true true false\ntrue true false false\ntrue true\ntrue\ntrue false\ntrue\nfalse true false\nfalse true\n"
	t.Run("normal", func(t *testing.T) { runMatchSource(t, source, want) })
	for name, variant := range map[string]struct {
		source string
		debug  bool
	}{"debug": {source, true}, "BOM_CRLF": {"\ufeff" + strings.ReplaceAll(source, "\n", "\r\n"), false}} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.ghi"), []byte(variant.source), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := compiler.Build(context.Background(), compiler.Options{Dir: dir, Debug: variant.debug})
			if err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(result.Executable).CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if got := strings.ReplaceAll(string(out), "\r\n", "\n"); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
	t.Run("coverage", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "tests"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "origins"), 0700); err != nil {
			t.Fatal(err)
		}
		library := strings.Replace(source, "namespace main\n", "namespace origins\n", 1)
		library = strings.NewReplacer("equal[", "Equal[", "different[", "Different[", "native[", "Native[", "wrapped[", "Wrapped[", "returned[", "Returned[", "mapped[", "Mapped[", "nativeBound[", "NativeBound[", "closures[", "Closures[").Replace(library)
		library = "\ufeff" + strings.ReplaceAll(library, "\n", "\r\n")
		testSource := `namespace tests
import testing "go:testing"
import origins
func TestNullable(t *testing.T){u:=new origins.User();var a ?origins.User=u;var b ?origins.User=u;if !origins.Equal[origins.User](u,u) || origins.Native[origins.User](a,b) || !new origins.Child[origins.User](u,u).same() || !origins.Closures[origins.User](u){t.Fatal("nullable origin changed under coverage")}}
`
		files := map[string]string{"main.ghi": "namespace main\nfunc main(){}\n", "origins/main.ghi": library, "tests/nullable_test.ghi": testSource}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		var log bytes.Buffer
		profile := filepath.Join(dir, "coverage.out")
		if err := compiler.Test(context.Background(), compiler.TestOptions{Dir: dir, Cover: true, CoverProfile: profile, Log: &log}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(profile)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(log.String(), "no executable statements") || len(strings.Split(strings.TrimSpace(string(data)), "\n")) < 2 {
			t.Fatalf("coverage fixture did not instrument production source: %s\n%s", log.String(), data)
		}
	})
}
