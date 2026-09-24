package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"path/filepath"
	"strings"
)

const runtimeNamespace = "ghi.runtime"

const runtimeClasses = `namespace ghi.runtime
class StackFrame {
 public functionName string
 public file string
 public line int
 constructor(functionName string, file string, line int) {
  this.functionName = functionName; this.file = file; this.line = line
 }
}
class Exception {
 public code int
 public typeName string
 public message string
 public stackTrace []StackFrame
 constructor(message string = "", code int = 0) {
  this.message = message; this.code = code; this.typeName = ""
  this.stackTrace = []StackFrame{}
 }
 public func Error() string { return this.message }
}
class GoError extends Exception {
 public cause error
 constructor(cause error, code int = 0) { parent(cause.Error(), code); this.cause = cause }
}
`

const runtimeCode = `package runtime
import (
 "go:fmt"
 "go:os"
 goruntime "go:runtime"
 "go:strings"
)
// ReportPanic is installed at the application entry boundary. Runtime faults
// remain fatal; it only renders their Ghi source frames before exiting.
func ReportPanic() {
 value:=recover()
 if value==nil{return}
 trace := CaptureStack()
 if exception,ok := value.(raised); ok {
  fmt.Fprintf(os.Stderr, "fatal: %s (code %d): %s\n", exception.value.GhiGet_6768692e72756e74696d65_Exception_typeName(), exception.value.GhiGet_6768692e72756e74696d65_Exception_code(), exception.value.GhiM_Error())
  trace = exception.value.GhiGet_6768692e72756e74696d65_Exception_stackTrace()
 } else { fmt.Fprintln(os.Stderr, "fatal:", value) }
 for _,frame := range trace {
  fmt.Fprintf(os.Stderr,"  at %s (%s:%d)\n",frame.GhiGet_6768692e72756e74696d65_StackFrame_functionName(),frame.GhiGet_6768692e72756e74696d65_StackFrame_file(),frame.GhiGet_6768692e72756e74696d65_StackFrame_line())
 }
 os.Exit(2)
}
func CaptureStack() []StackFrame {
 trace:=[]StackFrame{}
 pcs:=make([]uintptr,64)
 count:=goruntime.Callers(2,pcs)
 for count==len(pcs) { pcs=make([]uintptr,len(pcs)*2); count=goruntime.Callers(2,pcs) }
 frames:=goruntime.CallersFrames(pcs[:count])
 for {
  frame,more:=frames.Next()
  name:=frame.Function
  if strings.HasSuffix(frame.File,".ghi") && !strings.Contains(frame.File,".ghi-runtime") && !strings.Contains(name,"GhiM_") && !strings.Contains(name,"GhiNew_") && !strings.Contains(name,"GhiGet_") && !strings.Contains(name,"GhiSet_") && !strings.Contains(name,"GhiRef_") {
   if at:=strings.LastIndex(name,".GhiBody_");at>=0 {name=name[:at+1]+strings.Replace(name[at+9:],"_",".",1)}
   name=strings.ReplaceAll(name,"GhiInit_","constructor.")
   name=strings.TrimPrefix(name,"ghi.generated/")
   trace=append(trace,GhiNew_StackFrame(name,frame.File,frame.Line))
  }
  if !more {break}
 }
 return trace
}
func Some[T any](value T) *T { return &value }
func Received[T any](value T,ok bool)*T{if !ok{return nil};return &value}
func Receive[T any](channel <-chan T)*T{value,ok:=<-channel;return Received(value,ok)}
func ReceiveOK[T any](channel <-chan T)(*T,bool){value,ok:=<-channel;return Received(value,ok),ok}
func Assert[T any](value any)(*T,bool){result,ok:=value.(T);return Received(result,ok),ok}
func MapGet[K comparable,V any](values map[K]V,key K)*V {value,ok:=values[key];if !ok{return nil};return &value}
func MapGetOK[K comparable,V any](values map[K]V,key K)(*V,bool) {value,ok:=values[key];if !ok{return nil,false};return &value,true}
type indexInteger interface {~int|~int8|~int16|~int32|~int64|~uint|~uint8|~uint16|~uint32|~uint64|~uintptr}
func Slice[S ~[]E,E any,L indexInteger,H indexInteger,M indexInteger](values S,low L,high H,max M,hasHigh,hasMax bool) S {
 length:=uint64(len(values))
 if low<0 || uint64(low)>length || (hasHigh && (high<0 || uint64(high)>length)) || (hasMax && (max<0 || uint64(max)>length)) {panic("slice extends beyond initialized elements")}
 end,capacity:=len(values),len(values)
 if hasHigh {end=int(high)}
 if hasMax {capacity=int(max)}
 return values[int(low):end:capacity]
}
func Equal[L any,R any](left *L,right *R)bool {
 if left==nil || right==nil{return left==nil && right==nil}
 return any(*left)==any(*right)
}
type raised struct { value Exception }
func (exception raised) Error() string { return "Ghi exception: " + exception.value.GhiM_Error() }
func Raise(value Exception) any {
 if len(value.GhiGet_6768692e72756e74696d65_Exception_stackTrace())==0 {
  value.GhiSet_6768692e72756e74696d65_Exception_stackTrace(CaptureStack())
 }
 return raised{value}
}
func Throw(value Exception) { panic(Raise(value)) }
func Try(body func(), handler func(Exception), finalizer func()) {
 if finalizer != nil { defer finalizer() }
 if handler != nil {
  defer func() {
   value:=recover()
   if value == nil { return }
   if exception,ok:=value.(raised);ok { handler(exception.value) } else { panic(value) }
  }()
 }
 body()
}
func Check(err error) { if err != nil { Throw(GhiNew_GoError(err,0)) } }
`

func (p *program) addRuntime() error {
	if p.Namespaces[runtimeNamespace] != nil {
		return fmt.Errorf("namespace %s is reserved", runtimeNamespace)
	}
	filename := filepath.Join(p.Root, ".ghi-runtime.ghi")
	_, tree, unit, err := parseFile(p.Fset, filename, []byte(runtimeClasses))
	if err != nil {
		return err
	}
	ns := &namespace{Name: runtimeNamespace, Dir: filepath.Join(p.Root, ".ghi-runtime"), GoName: "runtime"}
	file := &sourceFile{Path: filename, Tree: tree, Unit: unit}
	for _, c := range unit.Classes {
		c.Namespace = ns
		c.File = file
	}
	native, err := parser.ParseFile(p.Fset, filename+".native", runtimeCode, parser.AllErrors|parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	ns.Files = []*sourceFile{file, {Path: filename + ".native", Tree: native, Unit: &unitDataNative}}
	p.Namespaces[ns.Name] = ns
	p.Ordered = append(p.Ordered, ns)
	p.Runtime = ns
	p.Wrapped = map[*ast.CallExpr]bool{}
	p.Helpers = map[int]bool{}
	for _, other := range p.Ordered {
		if other == ns {
			continue
		}
		content := fmt.Sprintf("package %s\nimport ghi_runtime \"go:%s/ghi/runtime\"\ntype Exception = ghi_runtime.Exception\ntype GoError = ghi_runtime.GoError\ntype StackFrame = ghi_runtime.StackFrame\n", other.GoName, generatedModule)
		tree, err := parser.ParseFile(p.Fset, filename+".aliases", content, parser.AllErrors|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		other.Files = append(other.Files, &sourceFile{Path: filename + ".aliases", Tree: tree, Unit: &unitDataNative})
	}
	return nil
}

var unitDataNative = unit{Native: true, Functions: map[string]*functionDecl{}}

func (p *program) runtimeSymbol(name string, file *sourceFile, ns *namespace) string {
	if ns == p.Runtime {
		return name
	}
	alias := p.importAlias(nil, file, namespacePath(p.Runtime), "runtime")
	return alias + "." + name
}

func (p *program) ensureErrorHelper(count int) string {
	if count == 0 {
		return "Check"
	}
	name := fmt.Sprintf("Must%d", count)
	if p.Helpers[count] {
		return name
	}
	p.Helpers[count] = true
	var generics, params, results, values []string
	for i := 0; i < count; i++ {
		typ := fmt.Sprintf("T%d", i)
		value := fmt.Sprintf("v%d", i)
		generics = append(generics, typ+" any")
		params = append(params, value+" "+typ)
		results = append(results, typ)
		values = append(values, value)
	}
	params = append(params, "err error")
	source := fmt.Sprintf("package runtime\nfunc %s[%s](%s)(%s){Check(err);return %s}", name, strings.Join(generics, ","), strings.Join(params, ","), strings.Join(results, ","), strings.Join(values, ","))
	tree, err := parser.ParseFile(p.Fset, "ghi-runtime-helper", source, parser.SkipObjectResolution)
	if err != nil {
		panic(err)
	}
	p.Runtime.Files[1].Tree.Decls = append(p.Runtime.Files[1].Tree.Decls, tree.Decls...)
	return name
}
