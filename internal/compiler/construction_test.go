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
  switch mode {case 1:this.user=User("one");case 2:this.user=User("two");default:this.user=User("other")}
 }
}
class Finalized {
 public user User
 constructor(){try{return}finally{this.user=User("finalized")}}
}
class Recovered {
 public user User
 constructor(){try{throw Exception("recover")}catch e Exception {this.user=User("caught")}}
}
func main(){fmt.Println(Holder(1).user.name,Holder(2).user.name,Holder(3).user.name,Finalized().user.name,Recovered().user.name)}
`})
	if got != "one two other finalized caught\n" {
		t.Fatalf("output %q", got)
	}
}

func TestConstructorControlFlowCannotSkipRequiredFields(t *testing.T) {
	for name, body := range map[string]string{
		"switch no default":        `switch mode {case 1:this.user=User()}`,
		"switch break":             `switch mode {case 1:break;this.user=User();default:this.user=User()}`,
		"conditional break":        `switch mode {case 1:if mode==1{break};this.user=User();default:this.user=User()}`,
		"catch leaves field empty": `try{throw Exception("x")}catch e Exception {}`,
		"finally incomplete":       `try{return}finally{if mode==1{this.user=User()}}`,
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
class Base { public user User;constructor(){this.user=User()} }
class Child extends Base {constructor(skip bool){if skip{return};parent()}}
func main(){_=Child(true)}
`})})
	if err == nil {
		t.Fatal("early return bypassed parent initialization")
	}
}
