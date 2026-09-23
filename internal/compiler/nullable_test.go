package compiler

import (
	"context"
	"testing"
)

func TestNullableAssignmentsArgumentsAndResults(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User {}
class Admin extends User {}
class Holder {
 public user User?
 constructor(user User? = nil) { this.user = user }
}

func present(user User?) bool { return user != nil }
func find() User? { return Admin() }
func main() {
 var user User? = Admin()
 fmt.Println(present(user), present(Admin()), present(find()), Holder().user == nil)
 user = nil
 user = User()
 fmt.Println(present(user), present(Holder(Admin()).user))
}
`})
	if got != "true true true true\ntrue true\n" {
		t.Fatalf("output %q", got)
	}
}

func TestNullableCheckedMemberAccess(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User {
 public name string
 constructor(name string) { this.name = name }
 public func label() string { return this.name }
}
func label(user User?) string {
 if user == nil { return "missing" }
 return user.label()
}
func checked(user User?) User {
 if user == nil { throw Exception("missing user") }
 return user
}
func accept(user User) string {return user.name}
func main() {
 var user User? = User("Ada")
 if user != nil { user.name = "Arman"; fmt.Println(user.name, user.label()) }
 fmt.Println(user != nil && user.name == "Arman", user == nil || user.label() == "Arman")
 fmt.Println(label(user), label(nil))
 if user != nil {var local User = user; local = user; fmt.Println(accept(user), checked(user).name, local.name)}
}
`})
	if got != "Arman Arman\ntrue true\nArman missing\nArman Arman Arman\n" {
		t.Fatalf("output %q", got)
	}
}

func TestNullableUnsafeMemberAccessRejected(t *testing.T) {
	for name, body := range map[string]string{
		"unchecked":         `var u User?; println(u.name)`,
		"wrong branch":      `var u User?; if u == nil { println(u.name) }`,
		"mutation":          `var u User? = User(); if u != nil { u = nil; println(u.name) }`,
		"branch leak":       `var u User?; if u != nil { println(u.name) }; println(u.name)`,
		"loop mutation":     `var u User? = User(); if u != nil { for i:=0;i<2;i++ { println(u.name);u=nil } }`,
		"captured mutation": `var u User? = User(); reset:=func(){u=nil}; if u != nil { reset(); println(u.name) }`,
		"address escape":    `var u User? = User(); ptr:=&u; if u != nil { *ptr=nil; println(u.name) }`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\nclass User { public name string }\nfunc main(){" + body + "}"})})
			if err == nil {
				t.Fatal("unsafe nullable access accepted")
			}
		})
	}
}

func TestNonnullableNilRejected(t *testing.T) {
	for name, source := range map[string]string{
		"declaration":          `func main(){var u User = nil; _=u}`,
		"assignment":           `func main(){u:=User();u=nil;_=u}`,
		"argument":             `func accept(u User){};func main(){accept(nil)}`,
		"return":               `func get() User{return nil};func main(){_=get()}`,
		"field":                `class Holder {public user User;constructor(){this.user=nil}};func main(){_=Holder()}`,
		"uninitialized local":  `func main(){var u User;_=u}`,
		"uninitialized global": `var u User;func main(){_=u}`,
		"unused default":       `func accept(u User = nil){};func main(){}`,
		"closure return":       `func main(){f:=func()User{return nil};_=f()}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\nclass User {}\n" + source})})
			if err == nil {
				t.Fatal("nil admitted to a nonnullable class")
			}
		})
	}
}

func TestRequiredFieldsInitializedOnEveryPath(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User { public name string;constructor(name string){this.name=name} }
class Holder {
 public user User
 constructor(flag bool){if flag {this.user=User("yes")}else{this.user=User("no")}}
}
func main(){fmt.Println(Holder(true).user.name,Holder(false).user.name)}
`})
	if got != "yes no\n" {
		t.Fatalf("output %q", got)
	}
	for name, body := range map[string]string{
		"implicit constructor": `class Holder {public user User}`,
		"one branch":           `class Holder {public user User;constructor(flag bool){if flag{this.user=User()}}}`,
		"early return":         `class Holder {public user User;constructor(flag bool){if flag{return};this.user=User()}}`,
		"read before write":    `class Holder {public user User;constructor(){this.user=this.user}}`,
		"loop may not execute": `class Holder {public user User;constructor(flag bool){for flag{this.user=User();break}}}`,
		"virtual call":         `class Holder {public user User;constructor(){this.init()};public func init(){this.user=User()}}`,
		"receiver escape":      `func escape(value any){};class Holder {public user User;constructor(){escape(this);this.user=User()}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\nclass User {}\n" + body + "\nfunc main(){}"})})
			if err == nil {
				t.Fatal("incomplete object construction accepted")
			}
		})
	}
}
