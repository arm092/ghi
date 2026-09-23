package compiler

import (
	"context"
	"testing"
)

func TestClassMapLookupIsNullable(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User {public name string;constructor(name string){this.name=name}}
var calls int
func key()string{calls++;return "one"}
func main(){
 users:=map[string]User{"one":User("Ada")}
 user:=users[key()]
 if user!=nil {fmt.Println(user.name)}
 missing,ok:=users["missing"]
 fmt.Println(missing==nil,ok,calls)
 users["two"]=User("Arman")
 second,found:=users["two"]
 if found && second!=nil {fmt.Println(second.name)}
}
`})
	if got != "Ada\ntrue false 1\nArman\n" {
		t.Fatalf("output %q", got)
	}
}

func TestCollectionZeroValuesCannotCreateNonnullObjects(t *testing.T) {
	for name, body := range map[string]string{
		"new class":      `p:=new(User);_=p`,
		"slice make":     `items:=make([]User,1);_=items`,
		"slice nil":      `items:=[]User{nil};_=items`,
		"array zero":     `var items [1]User;_=items`,
		"array hole":     `items:=[2]User{User()};_=items`,
		"slice hole":     `items:=[]User{2:User()};_=items`,
		"map nil":        `items:=map[string]User{"x":nil};_=items`,
		"append nil":     `items:=[]User{};items=append(items,nil);_=items`,
		"clear slice":    `items:=[]User{User()};clear(items)`,
		"struct zero":    `var item struct{user User};_=item`,
		"struct omitted": `item:=struct{user User}{};_=item`,
		"new struct":     `item:=new(struct{user User});_=item`,
		"map unchecked":  `items:=map[string]User{};var user User=items["absent"];_=user`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\nclass User {}\nfunc main(){" + body + "}"})})
			if err == nil {
				t.Fatal("nonnullable zero value accepted")
			}
		})
	}
}

func TestInitializedCollectionsAndBoundedReslicing(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User {public name string;constructor(name string){this.name=name}}
func main(){
 users:=make([]User,0,8)
 users=append(users,User("Ada"))
 array:=[2]User{User("one"),User("two")}
 item:=struct{user User}{user:User("Arman")}
 fmt.Println(users[0].name,array[1].name,item.user.name,len(users[:]))
 func(){defer func(){fmt.Println(recover()!=nil)}();_=users[:2]}()
}

`})
	if got != "Ada two Arman 1\ntrue\n" {
		t.Fatalf("output %q", got)
	}
}

func TestNullableCollectionLiteralsAndReferenceEquality(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class User { public name string;constructor(name string){this.name=name} }
func main(){
 object:=User("Ada")
 values:=[]User?{object,nil}
 mapping:=map[string]User?{"present":object,"absent":nil}
 aggregate:=struct{value User?}{value:object}
 var left User?=object
 var right User?=object
 var different User?=User("Ada")
 var empty User?
 fmt.Println(left==right,left!=different,empty==nil,values[0]==mapping["present"],aggregate.value==left)
 snapshot:=values[0]
 if snapshot!=nil{fmt.Println(snapshot.name)}
 initialized:=[]User{object}
 var low uint8=0
 var high uint64=1
 fmt.Println(len(initialized[low:high]))
}
`})
	if got != "true true true true true\nAda\n1\n" {
		t.Fatalf("output %q", got)
	}
}
