package compiler

import (
	"context"
	"testing"
)

func TestChannelAndTypeAssertionResultsAreNullable(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User {public name string;constructor(name string){this.name=name}}
func main(){
 channel:=make(chan User,2)
 channel<-new User("first")
 channel<-new User("second")
 close(channel)
 first:=<-channel
 if first!=nil{fmt.Println(first.name)}
 second,ok:=<-channel
 if ok && second!=nil{fmt.Println(second.name)}
 empty,open:=<-channel
 fmt.Println(empty==nil,open)
 select{case selected,ready:=<-channel:fmt.Println(selected==nil,ready)}
 values:=make(chan User,1);values<-new User("selected")
 select{case selected:=<-values:if selected!=nil{fmt.Println(selected.name)}}
 var object any=new User("asserted")
 cast,matched:=object.(User)
 if matched && cast!=nil{fmt.Println(cast.name)}
 object="other"
 failed,matched:=object.(User)
 fmt.Println(failed==nil,matched)
 optional:=make(chan ?User,1);optional<-new User("optional")
 item:=<-optional
 if item!=nil{channel2:=make(chan User,1);channel2<-item;result:=<-channel2;if result!=nil{fmt.Println(result.name)}}
}
`})
	if got != "first\nsecond\ntrue false\ntrue false\nselected\nasserted\ntrue false\noptional\n" {
		t.Fatalf("output %q", got)
	}
}

func TestZeroResultCannotBecomeUncheckedObject(t *testing.T) {
	for name, body := range map[string]string{
		"receive":   `channel:=make(chan User);close(channel);var user User=<-channel;_=user`,
		"select":    `channel:=make(chan User);close(channel);select{case user:=<-channel:user.name()}`,
		"assertion": `var value any="other";user,_:=value.(User);user.name()`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\nclass User {public func name()string{return \"name\"}}\nfunc main(){" + body + "}"})})
			if err == nil {
				t.Fatal("possibly absent object accepted without a check")
			}
		})
	}
}
