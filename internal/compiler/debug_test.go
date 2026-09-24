package compiler

import (
	"context"
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A successful debug build is insufficient: rewritten field operations must
// still have executable DWARF statement rows at their original Ghi lines.
func TestDebugClassStatementLocations(t *testing.T) {
	source := `namespace main
class Counter {
 public value int
 public func add(amount int) int {
  this.value += amount
  this.value++
  this.value = this.value + 1
  return this.value
 }
}
func main() {
 counter := new Counter()
 answer := counter.add(5)
 println(answer)
}
`
	dir := project(t, map[string]string{"main.ghi": source})
	built, err := Build(context.Background(), Options{Dir: dir, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	metadataBytes, err := os.ReadFile(built.Executable + ".ghi-debug.json")
	if err != nil {
		t.Fatal(err)
	}
	var metadata debugMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Version != 1 || metadata.Fields["F_6d61696e_Counter_value"] != "value" || metadata.Types["main.ghiData_Counter"] != "main.Counter" || metadata.Functions["main.GhiBody_Counter_add"] != (debugFunction{"main.Counter.add", false}) || !metadata.Functions["main.(*ghiData_Counter).GhiM_add"].Helper {
		t.Fatalf("incorrect debugger metadata: %+v", metadata)
	}
	var data *dwarf.Data
	switch runtime.GOOS {
	case "windows":
		file, openErr := pe.Open(built.Executable)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer file.Close()
		data, err = file.DWARF()
	case "darwin":
		file, openErr := macho.Open(built.Executable)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer file.Close()
		data, err = file.DWARF()
	default:
		file, openErr := elf.Open(built.Executable)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer file.Close()
		data, err = file.DWARF()
	}
	if err != nil {
		t.Fatal(err)
	}
	statements := map[int]bool{}
	reader := data.Reader()
	for {
		entry, readErr := reader.Next()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if entry == nil {
			break
		}
		if entry.Tag != dwarf.TagCompileUnit {
			continue
		}
		lines, readErr := data.LineReader(entry)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if lines == nil {
			continue
		}
		for {
			var line dwarf.LineEntry
			readErr = lines.Next(&line)
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				t.Fatal(readErr)
			}
			if line.IsStmt && !line.EndSequence && line.File != nil && filepath.Base(line.File.Name) == "main.ghi" {
				statements[line.Line] = true
			}
		}
	}
	for index, line := range strings.Split(source, "\n") {
		if strings.Contains(line, "this.value") || strings.Contains(line, "answer :=") || strings.Contains(line, "println(answer)") {
			if !statements[index+1] {
				t.Errorf("missing executable Ghi source line %d: %s", index+1, line)
			}
		}
	}
}

func TestDebugMetadataPreservesUnderscoresAndInheritedFields(t *testing.T) {
	dir := project(t, map[string]string{"main.ghi": `namespace main
class Base_Class {
 public field_name int
 public func method_name() int { return this.field_name }
}
class Child_Class extends Base_Class {}
func main() { println(new Child_Class().method_name()) }
`})
	prepared, err := prepareProject(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	m := prepared.program.debugMetadata()
	if m.Fields["F_6d61696e_Base_Class_field_name"] != "field_name" || m.Functions["main.GhiBody_Base_Class_method_name"].Name != "main.Base_Class.method_name" || !m.Functions["main.(*ghiData_Child_Class).GhiGet_6d61696e_Base_Class_field_name"].Helper {
		t.Fatalf("incorrect inherited/underscore mapping: %+v", m)
	}
}
