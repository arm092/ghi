package compiler

import (
	"context"
	"testing"
)

func TestConstructorSwitchAndFinallyInitialization(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User {public name string;constructor(name string){this.name=name}}
class Holder {
 public user User
 constructor(mode int){
  switch mode {case 1:this.user=new User("one");case 2:this.user=new User("two");default:this.user=new User("other")}
 }
}
class Finalized {
 public user User
 constructor(){try{return}finally{this.user=new User("finalized")}}
}
class Recovered {
 public user User
 constructor(){try{throw new Exception("recover")}catch e Exception {this.user=new User("caught")}}
}
func main(){fmt.Println(new Holder(1).user.name,new Holder(2).user.name,new Holder(3).user.name,new Finalized().user.name,new Recovered().user.name)}
`})
	if got != "one two other finalized caught\n" {
		t.Fatalf("output %q", got)
	}
}

func TestConstructorControlFlowCannotSkipRequiredFields(t *testing.T) {
	for name, body := range map[string]string{
		"switch no default":        `switch mode {case 1:this.user=new User()}`,
		"switch break":             `switch mode {case 1:break;this.user=new User();default:this.user=new User()}`,
		"conditional break":        `switch mode {case 1:if mode==1{break};this.user=new User();default:this.user=new User()}`,
		"catch leaves field empty": `try{throw new Exception("x")}catch e Exception {}`,
		"finally incomplete":       `try{return}finally{if mode==1{this.user=new User()}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\nclass User {}\nclass Holder {public user User;constructor(mode int){" + body + "}}\nfunc main(){}"})})
			if err == nil {
				t.Fatal("constructor can return an uninitialized field")
			}
		})
	}
}

func TestParentConstructorCannotBeSkipped(t *testing.T) {
	_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": `namespace main
class User {}
class Base { public user User;constructor(){this.user=new User()} }
class Child extends Base {constructor(skip bool){if skip{return};parent()}}
func main(){_=new Child(true)}
`})})
	if err == nil {
		t.Fatal("early return bypassed parent initialization")
	}
}

func TestRequiredAggregateAndDefinedTypeFields(t *testing.T) {
	for name, field := range map[string]string{"defined": "Other", "aggregate": "struct { user User }", "array": "[1]User"} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\nclass User {}\ntype Other User\nclass Holder {public value " + field + "}\nfunc main(){_=new Holder()}"})})
			if err == nil {
				t.Fatal("required field escaped initialization through an aggregate or defined type")
			}
		})
	}
}
