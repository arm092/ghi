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
 public user ?User
 constructor(user ?User = nil) { this.user = user }
}

func present(user ?User) bool { return user != nil }
func find() ?User { return new Admin() }
func main() {
 var user ?User = new Admin()
 fmt.Println(present(user), present(new Admin()), present(find()), new Holder().user == nil)
 user = nil
 user = new User()
 fmt.Println(present(user), present(new Holder(new Admin()).user))
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
func label(user ?User) string {
 if user == nil { return "missing" }
 return user.label()
}
func checked(user ?User) User {
 if user == nil { throw new Exception("missing user") }
 return user
}
func accept(user User) string {return user.name}
func named(flag bool) (user User) {if flag {user=new User("yes")}else{user=new User("no")};return}
func main() {
 var user ?User = new User("Ada")
 if user != nil { user.name = "Arman"; fmt.Println(user.name, user.label()) }
 fmt.Println(user != nil && user.name == "Arman", user == nil || user.label() == "Arman")
 fmt.Println(label(user), label(nil))
 if user != nil {var local User = user; local = user; fmt.Println(accept(user), checked(user).name, local.name); direct:=*user;fmt.Println(direct.name)}
 fmt.Println(named(true).name, named(false).name)
}
`})
	if got != "Arman Arman\ntrue true\nArman missing\nArman Arman Arman\nArman\nyes no\n" {
		t.Fatalf("output %q", got)
	}
}

func TestNullableUnsafeMemberAccessRejected(t *testing.T) {
	for name, body := range map[string]string{
		"unchecked":          `var u ?User; println(u.name)`,
		"wrong branch":       `var u ?User; if u == nil { println(u.name) }`,
		"mutation":           `var u ?User = new User(); if u != nil { u = nil; println(u.name) }`,
		"branch leak":        `var u ?User; if u != nil { println(u.name) }; println(u.name)`,
		"loop mutation":      `var u ?User = new User(); if u != nil { for i:=0;i<2;i++ { println(u.name);u=nil } }`,
		"captured mutation":  `var u ?User = new User(); reset:=func(){u=nil}; if u != nil { reset(); println(u.name) }`,
		"address escape":     `var u ?User = new User(); ptr:=&u; if u != nil { *ptr=nil; println(u.name) }`,
		"switch fallthrough": `var u ?User; switch {case u==nil:fallthrough;case u!=nil:println(u.name)}`,
		"switch alternative": `var u ?User; switch {case u!=nil,true:println(u.name)}`,
		"switch mutation":    `var u ?User=new User();switch {case u!=nil:u=nil;println(u.name)}`,
		"switch default":     `var u ?User;switch {case u!=nil:default:println(u.name)}`,
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
		"channel send":           `func main(){ch:=make(chan User,1);ch<-nil}`,
		"named function type":    `type Consumer func(User);func main(){var f Consumer=func(u User){};f(nil)}`,
		"defined nil conversion": `type Other User;func main(){x:=Other(nil);_=x}`,
		"defined nil default":    `type Other User;func unused(x Other=nil){};func main(){}`,

		"defined class type": `type Other User;func main(){var x Other;var user User=x;_=user}`,
		"named aggregate":    `func f()(x struct{user User}){return};func main(){_=f()}`,

		"declaration":                  `func main(){var u User = nil; _=u}`,
		"assignment":                   `func main(){u:=new User();u=nil;_=u}`,
		"argument":                     `func accept(u User){};func main(){accept(nil)}`,
		"return":                       `func get() User{return nil};func main(){_=get()}`,
		"field":                        `class Holder {public user User;constructor(){this.user=nil}};func main(){_=new Holder()}`,
		"uninitialized local":          `func main(){var u User;_=u}`,
		"uninitialized global":         `var u User;func main(){_=u}`,
		"unused default":               `func accept(u User = nil){};func main(){}`,
		"closure return":               `func main(){f:=func()User{return nil};_=f()}`,
		"named return":                 `func get()(user User){return};func main(){_=get()}`,
		"named read":                   `func get()(user User){return user};func main(){_=get()}`,
		"named one branch":             `func get(flag bool)(user User){if flag{user=new User()};return};func main(){_=get(false)}`,
		"named switch missing default": `func get(flag bool)(user User){switch flag{case true:user=new User()};return};func main(){_=get(false)}`,
		"named switch early break":     `func get(flag bool)(user User){switch {default:if flag{break};user=new User()};return};func main(){_=get(true)}`,
		"named finally conditional":    `func get(flag bool)(user User){try {} finally {if flag{user=new User()}};return};func main(){_=get(false)}`,
		"named catch partial":          `func get(flag bool)(user User){try {if flag{throw new Exception()};user=new User()}catch err Exception{_=err};return};func main(){_=get(true)}`,
		"named finally read":           `func get()(user User){try {user=new User()}finally{println(user)};return};func main(){_=get()}`,
		"named goto bypass":            `func get()(user User){goto done;user=new User();done:return};func main(){_=get()}`,
		"unchecked dereference":        `func main(){var user ?User;value:=*user;_=value}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\nclass User {}\n" + source})})
			if err == nil {
				t.Fatal("nil admitted to a nonnullable class")
			}
		})
	}
}

func TestNamedResultsAcrossSwitchAndFinally(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User {public name string;constructor(name string){this.name=name}}
func choose(n int)(user User){
 switch n {
 case 0: user=new User("zero")
 case 1: user=new User("one");break
 default: user=new User("other")
 }
 return
}
func falling(n int)(user User){switch n{case 0:fallthrough;default:user=new User("fall")};return}
func final()(user User){try {} finally {user=new User("final")};return}
func returning()(user User){try {return} finally {user=new User("return")}}
func caught()(user User){try {throw new Exception()}catch err Exception{user=new User("caught")};return}
func main(){fmt.Println(choose(0).name,choose(1).name,choose(2).name,falling(0).name,final().name,caught().name,returning().name)}
`})
	if got != "zero one other fall final caught return\n" {
		t.Fatalf("output %q", got)
	}
}

func TestNullableSwitchNarrowing(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User {public name string;constructor(name string){this.name=name}}
func name(user ?User) string {
 switch {case user==nil:return "missing";default:return user.name}
}
func checked(user ?User) string {
 switch {case user!=nil: return user.name; default: return "none"}
}
func main(){fmt.Println(name(new User("Ada")),name(nil),checked(new User("Arman")),checked(nil))}
`})
	if got != "Ada missing Arman none\n" {
		t.Fatalf("output %q", got)
	}
}

func TestRequiredFieldsInitializedOnEveryPath(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User { public name string;constructor(name string){this.name=name} }
class Holder {
 public user User
 constructor(flag bool){if flag {this.user=new User("yes")}else{this.user=new User("no")}}
}
func main(){fmt.Println(new Holder(true).user.name,new Holder(false).user.name)}
`})
	if got != "yes no\n" {
		t.Fatalf("output %q", got)
	}
	for name, body := range map[string]string{
		"implicit constructor": `class Holder {public user User}`,
		"one branch":           `class Holder {public user User;constructor(flag bool){if flag{this.user=new User()}}}`,
		"early return":         `class Holder {public user User;constructor(flag bool){if flag{return};this.user=new User()}}`,
		"read before write":    `class Holder {public user User;constructor(){this.user=this.user}}`,
		"loop may not execute": `class Holder {public user User;constructor(flag bool){for flag{this.user=new User();break}}}`,
		"virtual call":         `class Holder {public user User;constructor(){this.init()};public func init(){this.user=new User()}}`,
		"receiver escape":      `func escape(value any){};class Holder {public user User;constructor(){escape(this);this.user=new User()}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\nclass User {}\n" + body + "\nfunc main(){}"})})
			if err == nil {
				t.Fatal("incomplete object construction accepted")
			}
		})
	}
}
