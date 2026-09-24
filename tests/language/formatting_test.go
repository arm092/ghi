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
	"time"
)

func TestCanonicalFormattingLayout(t *testing.T) {
	source := `namespace main
// config import
import infrastructure.config as cfg // config tail
import z "go:z"
import (
 // grouped helper
 b "go:b" // b tail
)
import application.tasks.Service as TaskService
import a "go:a"
func empty() {}
func answer() int { value:=1; return value }
func main() {
 server:=&http.Server{Addr:"localhost",ReadTimeout:5*time.Second}
 values:=[]int{1,2}
 switch level {case "debug":println(1);case "warn":println(2);default:println(3)}
 select {case <-done:println("done");default:println("waiting")}
 try { run() } catch err Exception { println(err.message) } finally { cleanup() }
 callback:=() => {println("arrow")}
 _=server;_=values;_=callback
}
`
	formatted, err := compiler.FormatSource("main.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	text := string(formatted)
	last := -1
	for _, path := range []string{"import application.tasks.Service", `import a "go:a"`, "// grouped helper\nimport b \"go:b\" // b tail", `import z "go:z"`, "// config import\nimport infrastructure.config as cfg // config tail"} {
		index := strings.Index(text, path)
		if index < 0 || index <= last {
			t.Fatalf("imports not sorted with attached comments: %q\n%s", path, text)
		}
		last = index
	}
	for _, want := range []string{
		"func empty() {}",
		"func answer() int {\n\tvalue := 1\n\n\treturn value\n}",
		"&http.Server{\n\t\tAddr: \"localhost\",\n\t\tReadTimeout: 5 * time.Second,\n\t}",
		"[]int{\n\t\t1,\n\t\t2,\n\t}",
		"switch level {\n\t\tcase \"debug\":\n\t\t\tprintln(1)",
		"select {\n\t\tcase <-done:\n\t\t\tprintln(\"done\")",
		"try {\n\t\trun()\n\t} catch err Exception {\n\t\tprintln(err.message)\n\t} finally {\n\t\tcleanup()\n\t}",
		"() => {\n\t\tprintln(\"arrow\")\n\t}",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing layout %q\n%s", want, text)
		}
	}
	again, err := compiler.FormatSource("main.ghi", formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("not idempotent: %v\n%s", err, again)
	}
}

func TestCanonicalFormattingPreservesExecution(t *testing.T) {
	source := `namespace main
import (
 // string helpers
 strings "go:strings" // strings tail
 fmt "go:fmt"
)
type Record struct { Values []int; Labels map[string]string }
func record() Record { return Record{Values:[]int{1,2},Labels:map[string]string{"x":"a;b,{return}"}} }
func anonymous() struct { value int } { return struct { value int }{value:4} }
func main() {
 values:=[][]int{{1,2},{3,4}}
 value:=record()
 sum:=0
 for i:=0;i<2;i++ { for _,n:=range values[i] {sum+=n} }
 for _,n:=range []int{0,0} {sum+=n}
 done:=make(chan int,1);done<-3
 select {case n:=<-done:sum+=n;default:sum=0}
 switch sum {case 13:sum++;default:sum=0}
 if n:=sum;n<0 {sum=0}
 switch n:=sum;n {case 14:sum=n;default:sum=0}
 callbacks:=map[string]func() int{"answer":() int => {return 9}}
 commented:=[]int{8 /* trailing value */}
 f:=(n int) int => { x:=n+sum;return x }
 raw:=` + "`line  one\n  line two`" + `
 /* keep block
comment */
 try { if value.Values[0]==1 { throw new Exception("caught") } } catch err Exception {fmt.Println(err.message)}
 fmt.Println(f(2),strings.Contains(raw,"  line"),value.Labels["x"])
 fmt.Println(anonymous().value,callbacks["answer"](),commented[0])
}
`
	formatted, err := compiler.FormatSource("main.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	again, err := compiler.FormatSource("main.ghi", formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("not idempotent: %v\n%s", err, again)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var outputs []string
	for _, content := range [][]byte{[]byte(source), formatted} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "main.ghi"), content, 0600); err != nil {
			t.Fatal(err)
		}
		result, err := compiler.Build(ctx, compiler.Options{Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		out, err := exec.CommandContext(ctx, result.Executable).CombinedOutput()
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		outputs = append(outputs, strings.ReplaceAll(string(out), "\r\n", "\n"))
	}
	if outputs[0] != "caught\n16 true a;b,{return}\n4 9 8\n" || outputs[0] != outputs[1] {
		t.Fatalf("execution changed: %q", outputs)
	}
}
