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
 "go:sync"
)
var specializedNames = map[string]string{}
// ReportPanic is installed at the application entry boundary. Runtime faults
// remain fatal; it only renders their Ghi source frames before exiting.
func ReportPanic() {
 value:=recover()
 if value==nil{return}
 var trace []StackFrame
 if exception,ok := value.(raised); ok {
  fmt.Fprintf(os.Stderr, "fatal: %s (code %d): %s\n", exception.value.GhiGet_6768692e72756e74696d65_Exception_typeName(), exception.value.GhiGet_6768692e72756e74696d65_Exception_code(), exception.value.GhiM_Error())
  trace = exception.value.GhiGet_6768692e72756e74696d65_Exception_stackTrace()
 } else { trace=CaptureStack(); fmt.Fprintln(os.Stderr, "fatal:", value) }
 for _,frame := range trace {
  fmt.Fprintf(os.Stderr,"  at %s (%s:%d)\n",frame.GhiGet_6768692e72756e74696d65_StackFrame_functionName(),frame.GhiGet_6768692e72756e74696d65_StackFrame_file(),frame.GhiGet_6768692e72756e74696d65_StackFrame_line())
 }
 os.Exit(2)
}
// Cache only immutable frame descriptions, never public mutable StackFrames.
// Full PC sequences distinguish callers, recursion and inline call sites.
type stackKey struct { count int; pcs [64]uintptr }
type deepStackKey struct { count int; pcs [256]uintptr }
type stackFrameData struct { name,file string; line int }
type frameCache[K comparable] struct {
 mu sync.RWMutex
 entries map[K][]stackFrameData
 keys []K
 next int
}
var stackCache = frameCache[stackKey]{entries:make(map[stackKey][]stackFrameData)}
var deepStackCache = frameCache[deepStackKey]{entries:make(map[deepStackKey][]stackFrameData)}
func CaptureStack() []StackFrame {
 // Capture once into the larger stack-local buffer, then use the smaller
 // cache key for shallow traces. Neither buffer escapes on cache hits.
 var key deepStackKey
 key.count=goruntime.Callers(2,key.pcs[:])
 if key.count<64 {
  var shallow stackKey
  shallow.count=key.count
  copy(shallow.pcs[:],key.pcs[:key.count])
  return materializeStack(cachedStack(shallow))
 }
 if key.count<len(key.pcs) {
  if data,ok:=deepStackCache.load(key);ok {return materializeStack(data)}
  data:=decodeDeepStack(key)
  return materializeStack(deepStackCache.store(key,data,16))
 }
 return captureUncachedStack()
}
func captureUncachedStack() []StackFrame {
 // Arbitrarily deep stacks stay complete and bypass both bounded caches.
 pcs:=make([]uintptr,512)
 count:=goruntime.Callers(3,pcs)
 for count==len(pcs) {pcs=make([]uintptr,len(pcs)*2);count=goruntime.Callers(3,pcs)}
 trace:=[]StackFrame{}
 walkStack(pcs[:count],func(name,file string,line int){trace=append(trace,GhiNew_StackFrame(name,file,line))})
 return trace
}
// Each trace owns its backing objects. One allocation for all objects avoids
// a heap allocation per frame without sharing mutable state between throws.
func materializeStack(data []stackFrameData) []StackFrame {
 objects:=make([]ghiData_StackFrame,len(data))
 trace:=make([]StackFrame,len(data))
 for i,frame:=range data {
  object:=&objects[i]
  object.F_6768692e72756e74696d65_StackFrame_functionName=frame.name
  object.F_6768692e72756e74696d65_StackFrame_file=frame.file
  object.F_6768692e72756e74696d65_StackFrame_line=frame.line
  trace[i]=object
 }
 return trace
}
func cachedStack(key stackKey) []stackFrameData {
 stackCache.mu.RLock()
 data,ok:=stackCache.entries[key]
 stackCache.mu.RUnlock()
 if ok {return data}
 return stackCache.store(key,decodeStack(key),128)
}
func (cache *frameCache[K]) load(key K) ([]stackFrameData,bool) {
 cache.mu.RLock()
 data,ok:=cache.entries[key]
 cache.mu.RUnlock()
 return data,ok
}
func (cache *frameCache[K]) store(key K,data []stackFrameData,limit int) []stackFrameData {
 cache.mu.Lock()
 // Another goroutine may have decoded the same trace while we were outside
 // the lock. The descriptions returned to readers are never mutated.
 if existing,ok:=cache.entries[key];ok {data=existing} else {
  if len(cache.keys)==limit {
   delete(cache.entries,cache.keys[cache.next])
   cache.keys[cache.next]=key
   cache.next=(cache.next+1)%limit
  } else {cache.keys=append(cache.keys,key)}
  cache.entries[key]=data
 }
 cache.mu.Unlock()
 return data
}
// Keep the PC slice consumed by CallersFrames on the miss path only.
func decodeStack(key stackKey) []stackFrameData {
 data:=[]stackFrameData{}
 walkStack(key.pcs[:key.count],func(name,file string,line int){data=append(data,stackFrameData{name,file,line})})
 return data
}
func decodeDeepStack(key deepStackKey) []stackFrameData {
 data:=[]stackFrameData{}
 walkStack(key.pcs[:key.count],func(name,file string,line int){data=append(data,stackFrameData{name,file,line})})
 return data
}
func walkStack(pcs []uintptr,visit func(string,string,int)) {
 frames:=goruntime.CallersFrames(pcs)
 for {
  frame,more:=frames.Next()
  name:=frame.Function
  if at:=strings.Index(name,".ghi_specialized_");at>=0 {
   symbol,suffix:=name,""
   if dot:=strings.IndexByte(name[at+1:],'.');dot>=0 {symbol,suffix=name[:at+1+dot],name[at+1+dot:]}
   if display,ok:=specializedNames[symbol];ok {name=display+suffix}
  }
  if strings.HasSuffix(frame.File,".ghi") && !strings.Contains(frame.File,".ghi-runtime") && !strings.Contains(name,"GhiM_") && !strings.Contains(name,"GhiNew_") && !strings.Contains(name,"GhiGet_") && !strings.Contains(name,"GhiSet_") && !strings.Contains(name,"GhiRef_") {
   if at:=strings.LastIndex(name,".GhiBody_");at>=0 {name=name[:at+1]+strings.Replace(name[at+9:],"_",".",1)}
   name=strings.ReplaceAll(name,"GhiInit_","constructor.")
   name=strings.TrimPrefix(name,"ghi.generated/")
   visit(name,frame.File,frame.Line)
  }
  if !more {break}
 }
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
