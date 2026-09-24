package compiler

import "testing"

func TestDefaultsAcrossFilesInterfacesAndImplicitParent(t *testing.T) {
	got := runProgram(t, map[string]string{
		"main.ghi": `namespace main
import fmt "go:fmt"
import models "models"
class Child extends models.Parent {}
func main() { var p models.Named = new Child(); fmt.Println(p.label(), message()) }
`,
		"message.ghi": `namespace main
func message(value string = "default") string { return value }
`,
		"models/parent.ghi": `namespace models
interface Named { func label(suffix string = "!") string }
class Parent implements Named {
 protected name string
 constructor(name string = "base") { this.name = name }
 public func label(suffix string = "!") string { return this.name + suffix }
}
`,
	})
	if got != "base! default\n" {
		t.Fatalf("output %q", got)
	}
}

func TestFieldMutationEvaluatesReceiverOnce(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
var calls int
class Counter { public value int }
func pick(c Counter) Counter { calls++; return c }
func main() {
 c:=new Counter(); pick(c).value += 2; pick(c).value++
 for ; c.value < 5; pick(c).value++ {}
 for ; c.value < 9; pick(c).value += 2 {}
 fmt.Println(c.value,calls)
}
`})
	if got != "9 6\n" {
		t.Fatalf("output %q", got)
	}
}
