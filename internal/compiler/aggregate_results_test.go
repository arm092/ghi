package compiler

import (
	"context"
	"testing"
)

func TestAggregateZeroResultsRequireChecks(t *testing.T) {
	for name, body := range map[string]string{
		"map":       `values:=map[string]Record{};value:=values["missing"];value.user.name()`,
		"channel":   `values:=make(chan Record);close(values);value:=<-values;value.user.name()`,
		"assertion": `var source any=0;value,_:=source.(Record);value.user.name()`,
		"slice":     `values:=map[string][1]User{};value:=values["missing"];_=value[:]`,
		"range":     `values:=map[string][1]User{};value:=values["missing"];for _,user:=range value{user.name()}`,
		"array":     `values:=map[string][1]User{};value:=values["missing"];value[0].name()`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": `namespace main
class User {public func name()string{return "user"}}
type Record struct {user User}
func main(){` + body + `}`})})
			if err == nil {
				t.Fatal("unchecked absent aggregate accepted")
			}
		})
	}
}

func TestAggregateZeroResultsChecked(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User {public func name()string{return "user"}}
type Record struct {user User}
func consume(value Record){fmt.Println(value.user.name())}
func main(){
 values:=map[string]Record{"present":{user:new User()}}
 value:=values["present"];if value!=nil{consume(value)}
 missing:=values["missing"];fmt.Println(missing==nil)
 channel:=make(chan Record,1);channel<-Record{user:new User()};close(channel)
 received:=<-channel;if received!=nil{fmt.Println(received.user.name())}
 select{case empty,ok:=<-channel:fmt.Println(empty==nil,ok)}
 var source any=Record{user:new User()};cast,ok:=source.(Record)
 if ok && cast!=nil{consume(cast)}
 arrays:=map[string][1]User{"present":{new User()}}
 array:=arrays["present"];if array!=nil{fmt.Println(array[0].name()); _=array[:]; for _,user:=range array{_=user.name()}}
}
`})
	if got != "user\ntrue\nuser\ntrue false\nuser\nuser\n" {
		t.Fatalf("output %q", got)
	}
}
