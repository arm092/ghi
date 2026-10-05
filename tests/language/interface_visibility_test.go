package language_test

import (
	"context"
	"ghi/internal/compiler"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructuralInterfacesRequirePublicMethods(t *testing.T) {
	contexts := map[string]string{
		"declaration":                `func main(){var r Reader=new Secret();_=r}`,
		"assignment":                 `class Public {public func read()int{return 1}};func main(){var r Reader=new Public();r=new Secret();_=r}`,
		"argument":                   `func use(r Reader){};func main(){use(new Secret())}`,
		"variadic":                   `func use(rs ...Reader){};func main(){use(new Secret(),new Secret())}`,
		"return":                     `func make()Reader{return new Secret()};func main(){_=make()}`,
		"closure return":             `func main(){f:=func()Reader{return new Secret()};_=f}`,
		"tuple assignment":           `func pair()(int,Secret){return 1,new Secret()};class Public {public func read()int{return 1}};func main(){var r Reader=new Public();var n int;n,r=pair();_,_=n,r}`,
		"tuple argument":             `func pair()(int,Secret){return 1,new Secret()};func use(n int,r Reader){};func main(){use(pair())}`,
		"tuple variadic":             `class Public {public func read()int{return 1}};func pair()(Public,Secret){return new Public(),new Secret()};func use(rs ...Reader){};func main(){use(pair())}`,
		"composite nominal bound":    `func leak[T interface{Secret}](s T)Reader{return s};func main(){_=leak(new Secret())}`,
		"generic composite argument": `func use[T Reader](s T){};func leak[T interface{Secret}](s T){use(s)};func main(){leak(new Secret())}`,
		"slice":                      `func main(){_= []Reader{new Secret()}}`,
		"map":                        `func main(){_=map[string]Reader{"x":new Secret()}}`,
		"struct":                     `func main(){_=struct{r Reader}{r:new Secret()}}`,
		"append":                     `func main(){rs:=[]Reader{};_=append(rs,new Secret())}`,
		"channel":                    `func main(){ch:=make(chan Reader,1);ch<-new Secret()}`,
	}
	for _, visibility := range []string{"private", "protected"} {
		for name, body := range contexts {
			t.Run(visibility+"/"+name, func(t *testing.T) {
				dir := t.TempDir()
				source := "namespace main\ninterface Reader {func read() int}\nclass Secret {" + visibility + " func read() int{return 42}}\n" + body
				if err := os.WriteFile(filepath.Join(dir, "main.ghi"), []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
				err := compiler.Check(context.Background(), compiler.Options{Dir: dir})
				if err == nil || !strings.Contains(err.Error(), "cannot satisfy interface") || !strings.Contains(err.Error(), "method read is "+visibility) {
					t.Fatalf("expected visibility rejection, got %v", err)
				}
			})
		}
	}
}

func TestPublicStructuralInterfacesAndNominalUpcasts(t *testing.T) {
	runMatchSource(t, `namespace main
interface Reader[T any]{func read() T}
class Base {protected func hidden()int{return 7};public func read()int{return this.hidden()}}
class Child extends Base {public override func read()int{return parent.read()+1}}
type Alias = Reader[int]
func use(r Alias)int{return r.read()}
func pair()(Child,Child){return new Child(),new Child()}
func many(rs ...Alias)int{return rs[0].read()+rs[1].read()}
class Secret {private func read()int{return 42}}
func identity[T any](s T)T{return s}
func pass[T interface{Secret}](s T)T{return identity(s)}
func nominal[T Secret](s T)T{return s}
func main(){c:=new Child();var b Base=c;rs:=[]Alias{c};println(use(c),b.read(),rs[0].read());println(many(pair()));_=pass(new Secret());_=nominal(new Secret())}
`, "8 8 8\n16\n")
}
