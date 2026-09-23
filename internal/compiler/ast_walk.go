package compiler

import (
	"go/ast"
	"reflect"
)

// walkNode rewrites AST nodes without walking resolution objects or scopes.
// All replacements retain the AST interface type of their original slot.
func walkNode(node ast.Node, transform func(ast.Node) ast.Node, post bool) ast.Node {
	if node == nil {
		return nil
	}
	value := reflect.ValueOf(node)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return node
	}
	if !post {
		for {
			replacement := transform(node)
			if replacement == nil {
				return nil
			}
			if replacement == node {
				break
			}
			node = replacement
		}
		value = reflect.ValueOf(node)
	}
	if value.Kind() == reflect.Pointer && value.Elem().Kind() == reflect.Struct {
		value = value.Elem()
		for i := 0; i < value.NumField(); i++ {
			field := value.Field(i)
			if !field.CanSet() {
				continue
			}
			if field.Kind() == reflect.Slice {
				for j := 0; j < field.Len(); j++ {
					rewriteSlot(field.Index(j), transform, post)
				}
			} else {
				rewriteSlot(field, transform, post)
			}
		}
	}
	if post {
		return transform(node)
	}
	return node
}
func rewriteSlot(slot reflect.Value, transform func(ast.Node) ast.Node, post bool) {
	if !slot.CanInterface() || !slot.CanSet() {
		return
	}
	if (slot.Kind() == reflect.Pointer || slot.Kind() == reflect.Interface) && slot.IsNil() {
		return
	}
	child, ok := slot.Interface().(ast.Node)
	if !ok {
		return
	}
	replacement := walkNode(child, transform, post)
	if replacement == nil {
		slot.Set(reflect.Zero(slot.Type()))
		return
	}
	value := reflect.ValueOf(replacement)
	if value.Type().AssignableTo(slot.Type()) {
		slot.Set(value)
	}
}
