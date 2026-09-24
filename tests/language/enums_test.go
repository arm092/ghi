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

func TestEnumsExecution(t *testing.T) {
	source := `namespace main
import json "go:encoding/json"
enum Direction { North, South, }
enum Other { North, South, }
enum Status string { Pending = "pending", Done = "done", }
enum Code int { OK = 200, Missing = 404, Negative = -1, }
enum Toggle bool { On = true, Off = false, }
func move(d Direction) string { return match d { Direction.North => "north", default => "south", } }
func show(s string, n int, b bool) { println(s,n,b) }
class Job {
 public direction Direction
 constructor(direction Direction = Direction.South) {this.direction = direction}
 public func label(s string = Status.Pending) string {return s}
}
func identity[T comparable](x T) T { return x }
func nullable(d ?Direction) bool { return d == nil }
func main() {
 show(Status.Pending, Code.OK, Toggle.On)
 println(move(Direction.North),move(new Job().direction))
 println(new Job().label(),identity(Direction.South)==Direction.South)
 state := Status.Pending
 state = "custom"
 println(state)
 const done = Status.Done
 println(done)
 encoded := json.Marshal(Status.Pending)
 println(string(encoded))
 d := map[Direction]string{Direction.North: "N", Direction.South: "S"}
 println(d[Direction.North])
 println(nullable(nil))
 var initial Direction
 println(initial == Direction.North)
}
`
	want := "pending 200 true\nnorth south\npending true\ncustom\ndone\n\"pending\"\nN\ntrue\ntrue\n"
	runMatchSource(t, source, want)
	formatted, err := compiler.FormatSource("main.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	again, err := compiler.FormatSource("main.ghi", formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("enum formatting not idempotent: %v\n%s\n%s", err, formatted, again)
	}
	runMatchSource(t, string(formatted), want)
}

func TestEnumsRejections(t *testing.T) {
	for _, part := range []struct{ name, decl, body string }{
		{"raw string", "enum Direction {North, South}", `var d Direction = "North"; println(d)`},
		{"other enum", "enum Direction {North}\nenum Other {North}", `var d Direction = Other.North; println(d)`},
		{"plain construction", "enum Direction {North}", `println(Direction{})`},
		{"plain conversion", "enum Direction {North}\nenum Other {North}", `println(Direction(Other.North))`},
		{"reserved enum name", "enum GhiEnum_1_A_B {C}", ""},
		{"reserved case", "enum Direction {ghi_enum_tag}", ""},
		{"plain mutation", "enum Direction {North}", `Direction.North = Direction.North`},
		{"backed mutation", `enum Status string {Pending = "pending"}`, `Status.Pending = "other"`},
		{"address", `enum Status string {Pending = "pending"}`, `println(&Status.Pending)`},
		{"missing backing", `enum Status {Pending = "pending"}`, ""},
		{"missing value", `enum Status string {Pending}`, ""},
		{"plain address", "enum Direction {North}", `println(&Direction.North)`},
		{"plain const", "enum Direction {North}", `const d = Direction.North; println(d == Direction.North)`},
		{"mixed values", `enum Status string {A = "a", B}`, ""},
		{"duplicate bool", `enum Toggle bool {On = true, AlsoOn = true}`, ""},
		{"overflow", `enum Code int {A = 999999999999999999999999999}`, ""},
		{"bool", `enum Toggle bool {On = 1}`, ""},
		{"empty", `enum Empty {}`, ""},
		{"duplicate name", `enum Status string {A = "a", A = "b"}`, ""},
		{"duplicate value", `enum Code int {A = 1, B = 0x1}`, ""},
		{"wrong value", `enum Status string {A = 1}`, ""},
		{"wrong backing", `enum Status float64 {A = 1}`, ""},
		{"expression", `enum Status string {A = "a" + "b"}`, ""},
		{"unknown case", `enum Status string {A = "a"}`, `println(Status.B)`},
	} {
		t.Run(part.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "bad.ghi")
			source := "namespace main\n" + part.decl + "\nfunc main() {" + part.body + "}\n"
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			err := compiler.Check(context.Background(), compiler.Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), "bad.ghi:") || strings.Contains(err.Error(), "GhiEnum_") && part.name != "reserved enum name" {
				t.Fatalf("expected source rejection, got %v", err)
			}
		})
	}
}

func TestEnumsImportsAndShadowing(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"model/status.ghi": `namespace model
enum Direction { North, South, }
enum Status string { Pending = "pending", Done = "done", }
`,
		"main.ghi": `namespace main
import model
import model.Direction as Heading
import model.Status as TaskStatus
class Box[TaskStatus any] { public value TaskStatus; constructor(value TaskStatus) { this.value=value } }
func selected(d Heading) string { return match d { Heading.North => TaskStatus.Pending, default => model.Status.Done, } }
func main() {
 println(selected(model.Direction.North),selected(Heading.South))
 TaskStatus := struct{Pending string}{Pending:"local"}
 println(TaskStatus.Pending)
 println(new Box[string]("generic").value)
}
`,
	}
	for name, source := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	built, err := compiler.Build(context.Background(), compiler.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(built.Executable).CombinedOutput()
	if err != nil || strings.ReplaceAll(string(out), "\r\n", "\n") != "pending done\nlocal\ngeneric\n" {
		t.Fatalf("%v %s", err, out)
	}
}

func TestEnumsDeclarationLocations(t *testing.T) {
	for _, tc := range []struct{ source, location string }{
		{"enum Direction {North}\nnamespace main\nfunc main() {}", "bad.ghi:1:1"},
		{"namespace main\nenum State string {\n A = \"x\",\n B = \"x\",\n}\nfunc main() {}", "bad.ghi:4"},
		{"namespace main\nenum State string { A = \"x\" }\nfunc main() {\n State.A = \"y\"\n}\n", "bad.ghi:4:"},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.ghi")
		if err := os.WriteFile(path, []byte(tc.source), 0600); err != nil {
			t.Fatal(err)
		}
		err := compiler.Check(context.Background(), compiler.Options{Dir: dir})
		if err == nil || !strings.Contains(err.Error(), tc.location) {
			t.Fatalf("wanted %s, got %v", tc.location, err)
		}
	}
}
