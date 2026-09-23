package compiler

import "testing"

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
