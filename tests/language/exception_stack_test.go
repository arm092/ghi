package language_test

import "testing"

// Cached metadata must never expose shared mutable frames, truncate deep stacks,
// or replace user-supplied traces and the original trace during a rethrow.
func TestExceptionStackOwnershipAndDepth(t *testing.T) {
	runMatchSource(t, `namespace main
func fail() { throw new Exception("failure", 7) }
func capture() Exception {
 try { fail() } catch err Exception { return err }
 throw new Exception("unreachable")
}
func deep(depth int) {
 if depth == 0 { fail() }
 deep(depth - 1)
}
func captureDeep(depth int) Exception {
 try { deep(depth) } catch err Exception { return err }
 throw new Exception("unreachable")
}
func pattern(bits int, remaining int) Exception {
 if remaining == 0 { return capture() }
 if bits & 1 == 0 { return pattern(bits >> 1, remaining - 1) }
 return pattern((bits >> 1) + 256, remaining - 1)
}
func main() {
 first := new Exception()
 for i := 0; i < 300; i++ {
  err := capture()
  if err.stackTrace[0].functionName != "main.fail" { panic("shared frame") }
  if i == 0 { first = err; first.stackTrace[0].functionName = "edited" }
 }
 println(first.stackTrace[0].functionName, first.code)
 try { throw first } catch err Exception { println(err.stackTrace[0] == first.stackTrace[0]) }
 first.stackTrace = []StackFrame{}
 try { throw first } catch err Exception { println(err.stackTrace[0].functionName != "edited") }
 custom := new Exception()
 custom.stackTrace = []StackFrame{new StackFrame("custom", "custom.ghi", 123)}
 try { throw custom } catch err Exception { println(err.stackTrace[0].functionName, err.stackTrace[0].line) }
 for _, depth := range []int{60, 64, 96, 180} {
  trace := captureDeep(depth).stackTrace
  count := 0
  for _, frame := range trace { if frame.functionName == "main.deep" { count++ } }
  if count != depth + 1 { panic("truncated stack") }
 }
 println("deep stacks complete")
 // Exercise more distinct shallow call paths than the cache can retain.
 for path := 0; path < 300; path++ {
  err := pattern(path, 8)
  if err.stackTrace[0].functionName != "main.fail" { panic("wrong evicted stack") }
  count := 0
  for _, frame := range err.stackTrace { if frame.functionName == "main.pattern" { count++ } }
  if count != 9 { panic("wrong cached depth") }
 }
 done := make(chan int, 16)
 for worker := 0; worker < 16; worker++ {
  go func() {
   for i := 0; i < 100; i++ {
    err := capture()
    if err.stackTrace[0].functionName != "main.fail" { done <- 0; return }
    err.stackTrace[0].functionName = "worker"
   }
   done <- 1
  }()
 }
 for worker := 0; worker < 16; worker++ { if <-done != 1 { panic("shared concurrent frame") } }
 println("concurrent stacks independent")
}
`, "edited 7\ntrue\ntrue\ncustom 123\ndeep stacks complete\nconcurrent stacks independent\n")
}
