package base

type Base struct{ Value int }

func (c *Base) Add(n int) int { c.Value += n; return c.Value }
