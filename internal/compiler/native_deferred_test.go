package compiler

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestNativeDeferredCloseAndArgumentCapture(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
import os "go:os"
import bytes "go:bytes"
import io "go:io"
var trace string
func receiver(buffer *bytes.Buffer) *bytes.Buffer { trace += "receiver "; return buffer }
func argument() string { trace += "argument "; return "captured" }
func pair(buffer *bytes.Buffer) (io.Writer, string) { trace += "pair "; return buffer, "tuple" }
func write(buffer *bytes.Buffer) {
 defer receiver(buffer).WriteString(argument())
 trace += "body "
 buffer = &bytes.Buffer{}
}
func variadic(buffer *bytes.Buffer) {
 values:=[]any{"one", "two"}
 defer fmt.Fprintln(buffer, values...)
 defer fmt.Fprintf(pair(buffer))
 defer fmt.Fprintln(buffer)
 values[0]="changed"
 trace += "variadic "
}
func closeLater(file *os.File) {
 defer file.Close()
 fmt.Println(file.Stat().Size())
}
func main() {
 file:=os.CreateTemp("", "ghi-native-defer-")
 defer os.Remove(file.Name())
 file.WriteString("open")
 closeLater(file)
 try { file.Stat() } catch err GoError { fmt.Println("closed") }
 buffer:=&bytes.Buffer{}
 write(buffer)
 fmt.Println(trace, buffer.String())
 variadic(buffer)
 fmt.Printf("%q\n", buffer.String())
 fmt.Println(trace)
}
`})
	expected := "4\nclosed\nreceiver argument body  captured\n\"captured\\ntuplechanged two\\n\"\nreceiver argument body pair variadic \n"
	if got != expected {
		t.Fatalf("output %q, want %q", got, expected)
	}
}

func TestNativeDeferredErrorOccursAtExecution(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
import strconv "go:strconv"
func failLater() {
 defer strconv.Atoi("not a number")
 fmt.Println("body before deferred failure")
}
func main() {
 try { failLater() } catch err GoError { fmt.Println("caught after body") }
}
`})
	if got != "body before deferred failure\ncaught after body\n" {
		t.Fatalf("output %q", got)
	}
}

func TestNativeGoroutineCallIsAsynchronous(t *testing.T) {
	dir := project(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
import io "go:io"
func main() {
 reader,writer:=io.Pipe()
 defer reader.Close()
 defer writer.Close()
 go fmt.Fprintln(writer,"asynchronous")
 buffer:=make([]byte,64)
 count:=reader.Read(buffer)
 fmt.Print(string(buffer[:count]))
}
`})
	result, err := Build(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, result.Executable).CombinedOutput()
	if err != nil || strings.ReplaceAll(string(output), "\r\n", "\n") != "asynchronous\n" {
		t.Fatalf("async native call: %v: %s", err, output)
	}
}

func TestNativeGoroutineErrorIsReported(t *testing.T) {
	dir := project(t, map[string]string{"main.ghi": `namespace main
import strconv "go:strconv"
import time "go:time"
func main() {
 go strconv.Atoi("not a number")
 time.Sleep(time.Second)
}
`})
	result, err := Build(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, result.Executable).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "fatal:") || !strings.Contains(string(output), "invalid syntax") {
		t.Fatalf("async error report: %v: %s", err, output)
	}
}

func TestGoroutineUserAndNoErrorNativeCapture(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
import sync "go:sync"
func worker(start chan bool, done chan string, values ...string) {
 <-start
 done <- values[0]
}
func defaultWorker(done chan string, value string = "default") { done<-value }
func main() {
 start:=make(chan bool)
 done:=make(chan string)
 value:="captured"
 go worker(start,done,value)
 value="changed"
 once:=&sync.Once{}
 go once.Do(func(){start<-true})
 fmt.Println(<-done,value)
 go defaultWorker(done)
 fmt.Println(<-done)
}
`})
	if got != "captured changed\ndefault\n" {
		t.Fatalf("output %q", got)
	}
}

func TestGoroutineUserFailureRetainsSourceFrame(t *testing.T) {
	for name, source := range map[string]string{
		"user exception": "namespace main\nimport time \"go:time\"\nfunc worker() {\n throw new Exception(\"worker failure\")\n}\nfunc main(){go worker();time.Sleep(time.Second)}\n",
		"builtin panic":  "namespace main\nimport time \"go:time\"\nfunc main(){\n go panic(\"worker failure\")\n time.Sleep(time.Second)\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := project(t, map[string]string{"main.ghi": source})
			result, err := Build(context.Background(), Options{Dir: dir})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, result.Executable).CombinedOutput()
			if err == nil || !strings.Contains(string(output), "fatal:") || !strings.Contains(string(output), "worker failure") || !strings.Contains(string(output), "main.ghi:4") {
				t.Fatalf("goroutine source report: %v: %s", err, output)
			}
		})
	}
}
