package compiler

import (
	"context"
	"strings"
	"testing"
)

func TestSelectedTypeImports(t *testing.T) {
	got := runProgram(t, map[string]string{
		"main.ghi": `namespace main
import fmt "go:fmt"
import models.User
import models.Named
import models.Label
import collections.Box
class Child extends User implements Named {
 constructor(){parent("Ada")}
 public override func name() string {return parent.name()+"!"}
}
func optional(user ?User = nil) string {if user != nil {return user.name()};return "nil"}
func main(){
 var named Named = new Child()
 box:=new Box[User](User("Arman"))
 var label Label = "label"
 fmt.Println(named.name(),box.get().name(),optional(),optional(new User("Hi")),label)
}
`,
		"models/user.ghi": `namespace models
interface Named {func name() string}
type Label string
class User {
 private value string
 constructor(value string="Guest"){this.value=value}
 public func name() string{return this.value}
}
`,
		"collections/box.ghi": `namespace collections
class Box[T any] {
 private value T
 constructor(value T){this.value=value}
 public func get() T{return this.value}
}
`,
	})
	if got != "Ada! Arman nil Hi label\n" {
		t.Fatalf("output %q", got)
	}
}

func TestSelectedImportRespectsLexicalNames(t *testing.T) {
	got := runProgram(t, map[string]string{
		"main.ghi": `namespace main
import fmt "go:fmt"
import models.User
type Record struct {User string}
func shadow(User string) string{return User}
func identity[User any](value User) User{return value}
func main(){
 object:=new User()
 record:=Record{User:"field"}
 fmt.Println(object.name(),shadow("parameter"),record.User,identity("generic"))
 {User:="local";fmt.Println(User)}
}
`,
		"models/user.ghi": "namespace models\nclass User {public func name() string{return \"object\"}}",
	})
	if got != "object parameter field generic\nlocal\n" {
		t.Fatalf("output %q", got)
	}
}

func TestSelectedTypeImportAliases(t *testing.T) {
	files := map[string]string{
		"main.ghi": `namespace main
import fmt "go:fmt"
import local.User
import external.User as AuthUser
import external.User as RemoteUser
import external.Reader as AuthReader
import external.Label as AuthLabel
import local.Box as UserBox
class Member extends AuthUser implements AuthReader {}
func main(){
 var optional ?AuthUser = new AuthUser()
 if optional != nil {fmt.Println(optional.name())}
 var reader AuthReader = new Member()
 var label AuthLabel = "alias"
 box:=new UserBox[User](new User())
 fmt.Println(reader.name(),AuthUser().name(),box.get().name(),label,RemoteUser().name())
}
`,
		"local/user.ghi": `namespace local
class User {public func name()string{return "local"}}
class Box[T any]{value T;constructor(value T){this.value=value};public func get()T{return this.value}}
`,
		"external/user.ghi": `namespace external
interface Reader{func name()string}
type Label string
class User {public func name()string{return "external"}}
`,
	}
	for name, source := range files {
		formatted, err := FormatSource(name, []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		again, err := FormatSource(name, formatted)
		if err != nil || string(again) != string(formatted) {
			t.Fatalf("formatter not idempotent: %v", err)
		}
		files[name] = string(formatted)
	}
	got := runProgram(t, files)
	if got != "external\nexternal external local alias external\n" {
		t.Fatalf("output %q", got)
	}
}

func TestSelectedImportRejectsInvalidBindings(t *testing.T) {
	for name, source := range map[string]string{
		"unknown namespace":           "import missing.User\nfunc main(){_=new User()}",
		"unknown type":                "import models.Missing\nfunc main(){_=new Missing()}",
		"function is not type":        "import models.Factory\nfunc main(){Factory()}",
		"duplicate":                   "import models.User\nimport models.User\nfunc main(){_=new User()}",
		"declaration collision":       "import models.User\nclass User{}\nfunc main(){_=new User()}",
		"namespace collision":         "import models as User\nimport models.User\nfunc main(){_=new User()}",
		"unused":                      "import models.User\nfunc main(){}",
		"other members not exposed":   "import models.User\nfunc main(){_=new User();Factory()}",
		"alias collision":             "import models.User as Item\nimport models.User as Item\nfunc main(){_=new Item()}",
		"alias declaration collision": "import models.User as Item\nclass Item{}\nfunc main(){_=new Item()}",
		"alias original not exposed":  "import models.User as Item\nfunc main(){_=new Item();_=new User()}",
		"missing alias":               "import models.User as\nfunc main(){}",
		"keyword alias":               "import models.User as new\nfunc main(){}",
	} {
		t.Run(name, func(t *testing.T) {
			err := Check(context.Background(), Options{Dir: project(t, map[string]string{
				"main.ghi":        "namespace main\n" + source,
				"models/user.ghi": "namespace models\nclass User{}\nfunc Factory(){}",
			})})
			if err == nil {
				t.Fatal("invalid selected import accepted")
			}
			if strings.Contains(err.Error(), "expected 'STRING'") {
				t.Fatalf("import syntax unsupported: %v", err)
			}
		})
	}
}

func TestSelectedImportFileScopeAndDiagnostics(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"file scope": {
			"main.ghi":        "namespace main\nimport models.User\nfunc main(){_=new User();helper()}",
			"helper.ghi":      "namespace main\nfunc helper(){_=new User()}",
			"models/user.ghi": "namespace models\nclass User{}",
		},
		"alias diagnostics": {
			"main.ghi":        "namespace main\nimport models.User as AuthUser\nfunc main(){\nvar value AuthUser = 1\n_=value\n}",
			"models/user.ghi": "namespace models\nclass User{}",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := Check(context.Background(), Options{Dir: project(t, files)})
			if err == nil {
				t.Fatal("invalid program accepted")
			}
			if strings.Contains(err.Error(), "ghi_type_import_") {
				t.Fatalf("internal import name leaked: %v", err)
			}
			if name == "alias diagnostics" && !strings.Contains(err.Error(), "main.ghi:4") {
				t.Fatalf("original location missing: %v", err)
			}
		})
	}
}

func TestSelectedImportDoesNotCaptureNamespaceAlias(t *testing.T) {
	got := runProgram(t, map[string]string{
		"main.ghi": `namespace main
import fmt "go:fmt"
import models
import models.Label
import models.User
func main(){
 original:=new models.User().name()
 models:=1
 var label Label = "selected"
 fmt.Println(original,label,new User().name(),models)
}
`,
		"models/types.ghi": `namespace models
type Label string
class User {public func name()string{return "object"}}
`,
	})
	if got != "object selected object 1\n" {
		t.Fatalf("output %q", got)
	}
}
