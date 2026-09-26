package base

type GenericBase[T ~int] struct{ Value T }

func (c *GenericBase[T]) Add(n T) T { c.Value += n; return c.Value }

type Base struct{ Value int }

func (c *Base) Add(n int) int { c.Value += n; return c.Value }
