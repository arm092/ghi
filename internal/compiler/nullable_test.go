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
func main() {
 var user User? = User("Ada")
 if user != nil { user.name = "Arman"; fmt.Println(user.name, user.label()) }
 fmt.Println(user != nil && user.name == "Arman", user == nil || user.label() == "Arman")
 fmt.Println(label(user), label(nil))
}
`})
	if got != "Arman Arman\ntrue true\nArman missing\n" {
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
