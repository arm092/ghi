package language_test

import "testing"

// Concrete receiver optimization must preserve the interface type of aliases,
// captured/rebound receivers, generic fields and inherited virtual calls.
func TestReceiverSpecializationSemantics(t *testing.T) {
	runMatchSource(t, `namespace main
class Cell[T any] {
 public value T
 constructor(value T) { this.value = value }
 public func alias(other Cell[T]) T {
  copy := this
  copy = other
  return copy.value
 }
 public func rebound(other Cell[T]) T {
  change := () => { this = other }
  change()
  return this.value
 }
 public func captured() func() T { return () T => { return this.value } }
 public func address(other Cell[T]) T {
  ref := &this
  if ref != nil { *ref = other }
  return this.value
 }
}
class Base {
 public func value() int { return 1 }
 public func read() int { return this.value() }
}
class Child extends Base {
 public override func value() int { return 2 }
 public func viaParent() int { return parent.read() }
}
func main() {
 a := new Cell[int](3)
 b := new Cell[int](8)
 println(a.alias(b), a.rebound(b), a.address(b), a.captured()())
 println(new Child().viaParent())
}
`, "8 8 8 3\n2\n")
}
