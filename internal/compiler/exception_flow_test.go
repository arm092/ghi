package compiler

import (
	"context"
	"testing"
)

func TestFinallyOverridesReturnAndRethrowsFromCatch(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
func override() int {
 try { return 1 } finally { return 2 }
}
func named() (result int) {
 try { return 10 } finally { result++ }
}
func nested() int {
 try { return 3 } finally {
  try { fmt.Println("inner") } finally { fmt.Println("nested cleanup") }
  fmt.Println("outer")
 }
}
func main() {
 fmt.Println(override(),named(),nested())
 try {
  try { throw Exception("first") } catch e Exception { throw Exception("second")
  } finally { fmt.Println("rethrow cleanup") }
 } catch e Exception { fmt.Println(e.message) }
}
`})
	expected := "inner\nnested cleanup\nouter\n2 11 3\nrethrow cleanup\nsecond\n"
	if got != expected {
		t.Fatalf("output %q", got)
	}
}

func TestExceptionsInsideAnonymousFunctions(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
import strconv "go:strconv"
func main() {
 f:=func(value string) int {
  try { return strconv.Atoi(value) } catch e GoError { return -1 }
 }
 fmt.Println(f("12"),f("bad"))
}
`})
	if got != "12 -1\n" {
		t.Fatalf("output %q", got)
	}
}

func TestExceptionTypesAndReturnCoverage(t *testing.T) {
	for name, body := range map[string]string{
		"throw primitive":       `func main(){ throw 42 }`,
		"missing return":        `func f() int { try {} finally {} }; func main(){}`,
		"catch unrelated class": `class Item {}; func main(){try {throw Exception("x")} catch e Item {}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\n" + body})})
			if err == nil {
				t.Fatal("invalid exception/control-flow program accepted")
			}
		})
	}
}
