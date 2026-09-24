package am.ghi.ide;

import com.intellij.psi.*;
import com.intellij.psi.search.*;
import com.intellij.openapi.project.DumbService;
import com.intellij.psi.util.PsiTreeUtil;
import java.util.*;

/** Tolerant source model. Offsets always refer to PSI text, including unsaved edits. */
final class GhiSymbols {
    record Token(String text, int start, int end, boolean identifier) {}
    static final class Scope {
        int start, end; Scope parent; Symbol owner;
        Scope(int start, int end, Scope parent) { this.start=start; this.end=end; this.parent=parent; }
        boolean contains(int offset) { return start<=offset && offset<=end; }
    }
    static final class Symbol {
        String name, kind, type="", base="", visibility="public", importNamespace=""; int offset;
        PsiFile file; Scope scope, body; List<String> parameters=new ArrayList<>();
        Symbol(String name,String kind,int offset,PsiFile file,Scope scope) {
            this.name=name;this.kind=kind;this.offset=offset;this.file=file;this.scope=scope;
        }
        PsiElement psi() { if(offset<0)return null;return PsiTreeUtil.getParentOfType(file.findElementAt(offset),GhiIdentifier.class,false); }
    }
    static final class Source {
        PsiFile file; String namespace=""; List<Token> tokens=new ArrayList<>();
        List<Scope> scopes=new ArrayList<>(); Map<Integer,Symbol> declarations=new HashMap<>();
        Source(PsiFile file) { this.file=file; }
        Scope scope(int offset) {
            Scope result=scopes.getFirst();
            for(Scope scope:scopes)if(scope.contains(offset)&&scope.start>=result.start)result=scope;
            return result;
        }
    }
    final List<Symbol> symbols=new ArrayList<>(); final Map<PsiFile,Source> sources=new LinkedHashMap<>();
    static GhiSymbols forFile(PsiFile file) {
        return com.intellij.psi.util.CachedValuesManager.getCachedValue(file, () -> com.intellij.psi.util.CachedValueProvider.Result.create(build(file), com.intellij.psi.util.PsiModificationTracker.MODIFICATION_COUNT));
    }
    private static GhiSymbols build(PsiFile file) {
        GhiSymbols result=new GhiSymbols(); result.add(file);
        if(!DumbService.isDumb(file.getProject()))for(var vf:FileTypeIndex.getFiles(GhiFileType.INSTANCE,GlobalSearchScope.projectScope(file.getProject()))) {
            if(file.getVirtualFile()!=null&&vf.equals(file.getVirtualFile()))continue;
            PsiFile other=PsiManager.getInstance(file.getProject()).findFile(vf);
            if(other!=null)result.add(other);
        }
        return result;
    }
    private void add(PsiFile file) {
        Source s=new Source(file);sources.put(file,s); GhiLexer lexer=new GhiLexer();String text=file.getText();lexer.start(text);
        while(lexer.getTokenType()!=null){
            var type=lexer.getTokenType();
            if(type!=com.intellij.psi.TokenType.WHITE_SPACE&&type!=GhiLexer.COMMENT)
                s.tokens.add(new Token(text.substring(lexer.getTokenStart(),lexer.getTokenEnd()),lexer.getTokenStart(),lexer.getTokenEnd(),type==GhiLexer.IDENTIFIER));
            lexer.advance();
        }
        Scope root=new Scope(0,text.length(),null);s.scopes.add(root);Scope current=root;
        for(Token t:s.tokens){if(t.text.equals("{")){current=new Scope(t.start,text.length(),current);s.scopes.add(current);}else if(t.text.equals("}")&&current.parent!=null){current.end=t.end;current=current.parent;}}
        var ts=s.tokens;
        for(int i=0;i<ts.size();i++){
            Token t=ts.get(i);String word=t.text;
            if(word.equals("namespace")){s.namespace=qualified(ts,i+1);continue;}
            if(word.equals("import")&&i+1<ts.size()&&ts.get(i+1).text.startsWith("\"")){
                String namespace=ts.get(i+1).text.replace("\"","");String alias=namespace.substring(Math.max(namespace.lastIndexOf('.'),namespace.lastIndexOf('/'))+1);
                if(alias.startsWith("go:"))alias=alias.substring(3);
                Symbol symbol=new Symbol(alias,"import",ts.get(i+1).start,file,root);symbol.importNamespace=namespace;symbols.add(symbol);
            }
            if(Set.of("class","interface","type","func","import").contains(word)&&i+1<ts.size()&&ts.get(i+1).identifier){
                int nameIndex=i+1;Token name=ts.get(nameIndex);Scope scope=s.scope(t.start);
                Symbol symbol=declare(s,name,word,scope);symbol.visibility=scope.owner!=null&&scope.owner.kind.equals("interface")?"public":visibility(ts,i);
                if(word.equals("import")&&nameIndex+1<ts.size())symbol.importNamespace=ts.get(nameIndex+1).text.replace("\"","");
                if(word.equals("class")||word.equals("interface")||word.equals("func")) {
                    int open=find(ts,nameIndex+1,"{",Set.of(";","}"));
                    if(word.equals("func")) {
                        int paren=nameIndex+1;
                        if(is(ts,paren,"(")){int close=matching(ts,paren,"(",")");
                            if(close>=0){open=is(ts,close+1,"{")?close+1:findOnLine(ts,close+1,"{",text);
                                symbol.type=typeAt(ts,close+1);
                                if(open>=0)symbol.body=scopeAt(s,ts.get(open).start);
                                parameters(s,symbol,paren+1,close,text);
                            }
                        }
                    } else {
                        if(open>=0)symbol.body=scopeAt(s,ts.get(open).start);
                        for(int k=nameIndex+1;k<ts.size()&&(open<0||k<open);k++)if(is(ts,k,"extends")){symbol.base=qualified(ts,k+1);break;}
                    }
                    if(symbol.body!=null)symbol.body.owner=symbol;
                }
            }
            if(word.equals("constructor")&&is(ts,i+1,"(")){
                Scope scope=s.scope(t.start);int close=matching(ts,i+1,"(",")");
                if(close>=0&&scope.owner!=null){Symbol symbol=new Symbol("constructor","constructor",t.start,file,scope);symbols.add(symbol);
                    if(is(ts,close+1,"{")){symbol.body=scopeAt(s,ts.get(close+1).start);symbol.body.owner=symbol;}
                    parameters(s,symbol,i+2,close,text);
                }
            }
        }
        for(int i=0;i<ts.size();i++){
            Token t=ts.get(i);if(!t.identifier||s.declarations.containsKey(t.start))continue;
            Scope scope=s.scope(t.start);String prev=i>0?ts.get(i-1).text:"";
            boolean explicit=prev.equals("var")||prev.equals("const");
            boolean shortDecl=is(ts,i+1,":")&&is(ts,i+2,"=");
            boolean field=scope.owner!=null&&(scope.owner.kind.equals("class")||scope.owner.kind.equals("interface"))
                &&(Set.of("public","private","protected").contains(prev)||i==0||lineBreak(text,ts.get(i-1).end,t.start)||prev.equals("{"))
                &&!is(ts,i+1,"(")&&!is(ts,i+1,"}");
            if(explicit||shortDecl||field){
                Symbol symbol=declare(s,t,field?"field":prev.equals("const")?"const":"local",scope);symbol.visibility=scope.owner!=null&&scope.owner.kind.equals("interface")?"public":visibility(ts,i);
                symbol.type=shortDecl?typeAt(ts,i+3):typeAt(ts,i+1);
                if(symbol.type.isEmpty()&&is(ts,i+1,"="))symbol.type=typeAt(ts,i+2);
            }
        }
    }
    private static boolean lineBreak(String text,int from,int to){return text.substring(from,to).contains("\n");}
    private static Scope scopeAt(Source s,int start){for(Scope scope:s.scopes)if(scope.start==start)return scope;return null;}
    private Symbol declare(Source s,Token t,String kind,Scope scope){Symbol symbol=new Symbol(t.text,kind,t.start,s.file,scope);symbols.add(symbol);s.declarations.put(t.start,symbol);return symbol;}
    private void parameters(Source s,Symbol function,int start,int end,String text){
        int segment=start,depth=0;
        for(int i=start;i<=end;i++){
            if(i<end&&Set.of("(","[","{").contains(s.tokens.get(i).text))depth++;
            if(i<end&&Set.of(")","]","}").contains(s.tokens.get(i).text))depth--;
            if(i==end||(is(s.tokens,i,",")&&depth==0)){
                if(segment<i){Token name=s.tokens.get(segment);function.parameters.add(text.substring(name.start,s.tokens.get(i-1).end));
                    if(name.identifier&&function.body!=null){Symbol param=declare(s,name,"parameter",function.body);param.type=typeAt(s.tokens,segment+1);}}
                segment=i+1;
            }
        }
    }
    static boolean is(List<Token> ts,int i,String text){return i>=0&&i<ts.size()&&ts.get(i).text.equals(text);}
    private static int find(List<Token> ts,int start,String text,Set<String> stop){for(int i=start;i<ts.size();i++){if(is(ts,i,text))return i;if(stop.contains(ts.get(i).text))break;}return -1;}
    private static int findOnLine(List<Token> ts,int start,String value,String text){for(int i=start;i<ts.size();i++){if(i>0&&lineBreak(text,ts.get(i-1).end,ts.get(i).start))break;if(is(ts,i,value))return i;if(is(ts,i,"}"))break;}return -1;}
    static int matching(List<Token> ts,int start,String open,String close){int depth=0;for(int i=start;i<ts.size();i++){if(is(ts,i,open))depth++;if(is(ts,i,close)&&--depth==0)return i;}return -1;}
    private static String typeAt(List<Token> ts,int index){while(is(ts,index,"new")||is(ts,index,"?")||is(ts,index,"*"))index++;return index<ts.size()&&ts.get(index).identifier?qualified(ts,index):"";}
    private static String qualified(List<Token> ts,int index){StringBuilder name=new StringBuilder();while(index<ts.size()&&ts.get(index).identifier){name.append(ts.get(index++).text);if(!is(ts,index,".")||index+1>=ts.size()||!ts.get(index+1).identifier)break;name.append('.');index++;}return name.toString();}
    private static String visibility(List<Token> ts,int index){for(int i=index-1;i>=Math.max(0,index-3);i--){String word=ts.get(i).text;if(Set.of("public","private","protected").contains(word))return word;if(!word.equals("override"))break;}return "private";}
    private boolean sameNamespace(PsiFile first,PsiFile second){return sources.get(first).namespace.equals(sources.get(second).namespace);}
    Symbol symbolAt(PsiFile file,int offset){Source s=sources.get(file);if(s==null)return null;for(int i=0;i<s.tokens.size();i++){Token token=s.tokens.get(i);if(token.start<=offset&&offset<token.end)return resolveToken(s,i,new HashSet<>());}return null;}
    PsiElement resolve(PsiFile file,int offset){Symbol symbol=symbolAt(file,offset);return symbol==null?null:symbol.psi();}
    private Symbol resolveToken(Source s,int index,Set<Integer> visited){
        if(index<0||!visited.add(index))return null;Token token=s.tokens.get(index);
        Symbol declaration=s.declarations.get(token.start);if(declaration!=null)return declaration;
        if(is(s.tokens,index-1,".")){
            Symbol receiver=receiver(s,index-2,visited);if(receiver==null)return null;
            if(receiver.kind.equals("import"))return imported(s,receiver,token.text);
            Symbol type=asType(receiver,s.file);return member(type,token.text,new HashSet<>());
        }
        if(token.text.equals("this")||token.text.equals("parent")){
            Symbol owner=enclosingClass(s.scope(token.start));return token.text.equals("parent")&&owner!=null?type(owner.base,s.file):owner;
        }
        Scope scope=s.scope(token.start);
        for(Scope at=scope;at!=null;at=at.parent){
            Symbol result=null;
            for(Symbol symbol:symbols)if(symbol.file==s.file&&symbol.scope==at&&symbol.name.equals(token.text)
                &&(!symbol.kind.equals("local")&&!symbol.kind.equals("const")||symbol.offset<=token.start))
                if(result==null||symbol.offset>result.offset)result=symbol;
            if(result!=null)return result;
        }
        for(Symbol symbol:symbols)if(symbol.scope.parent==null&&symbol.kind.equals("import")==false&&symbol.name.equals(token.text)&&sameNamespace(s.file,symbol.file))return symbol;
        return null;
    }
    private Symbol receiver(Source s,int index,Set<Integer> visited){
        if(index<0)return null;
        if(is(s.tokens,index,")")){int depth=1;for(int i=index-1;i>=0;i--){if(is(s.tokens,i,")"))depth++;if(is(s.tokens,i,"(")&&--depth==0){Symbol call=resolveToken(s,calleeIndex(s.tokens,i-1),visited);return call==null?null:asType(call,s.file);}}return null;}
        return resolveToken(s,index,visited);
    }
    private Symbol imported(Source s,Symbol alias,String name){
        String namespace=alias.importNamespace;
        if(namespace.startsWith("go:"))return null;
        for(Symbol symbol:symbols)if(symbol.scope.parent==null&&symbol.name.equals(name)&&sources.get(symbol.file).namespace.equals(namespace))return symbol;
        return null;
    }
    Symbol type(String name,PsiFile context){
        int dot=name.indexOf('.');
        if(dot>0)for(Symbol alias:symbols)if(alias.file==context&&alias.kind.equals("import")&&alias.name.equals(name.substring(0,dot)))return imported(sources.get(context),alias,name.substring(dot+1));
        for(Symbol symbol:symbols)if(Set.of("class","interface","type").contains(symbol.kind)&&(symbol.name.equals(name)&&sameNamespace(context,symbol.file)||(sources.get(symbol.file).namespace+"."+symbol.name).equals(name)))return symbol;return null;}
    private Symbol asType(Symbol symbol,PsiFile context){return Set.of("class","interface","type").contains(symbol.kind)?symbol:type(symbol.type,context);}
    private Symbol member(Symbol type,String name,Set<Symbol> visited){if(type==null||!visited.add(type))return null;for(Symbol symbol:symbols)if(symbol.scope==type.body&&symbol.name.equals(name))return symbol;return member(type(type.base,type.file),name,visited);}
    private static Symbol enclosingClass(Scope scope){for(;scope!=null;scope=scope.parent)if(scope.owner!=null&&scope.owner.kind.equals("class"))return scope.owner;return null;}
    private boolean inherits(Symbol child,Symbol parent,Set<Symbol> visited){
        if(child==null||!visited.add(child))return false;
        return child==parent||inherits(type(child.base,child.file),parent,visited);
    }
    boolean related(Symbol first,Symbol second){return inherits(first,second,new HashSet<>())||inherits(second,first,new HashSet<>());}
    List<Symbol> complete(PsiFile file,int offset){
        Source s=sources.get(file);int index=0;while(index<s.tokens.size()&&s.tokens.get(index).end<=offset)index++;
        int dot=is(s.tokens,index-1,".")?index-1:is(s.tokens,index-2,".")?index-2:-1;
        boolean construction=constructionContext(s,index);
        LinkedHashMap<String,Symbol> result=new LinkedHashMap<>();
        if(dot>=0){Symbol receiver=receiver(s,dot-1,new HashSet<>());if(receiver==null)return List.of();
            if(receiver.kind.equals("import")){
                if(receiver.importNamespace.startsWith("go:"))return List.of();
                for(Symbol symbol:symbols)if(symbol.scope.parent==null&&!symbol.kind.equals("import")&&sources.get(symbol.file).namespace.equals(receiver.importNamespace))result.putIfAbsent(symbol.name,symbol);
                return completionValues(result,construction,file);
            }
            Symbol cls=asType(receiver,file);Set<Symbol> visited=new HashSet<>();Symbol current=enclosingClass(s.scope(offset));
            while(cls!=null&&visited.add(cls)){for(Symbol symbol:symbols)if(symbol.scope==cls.body&&!symbol.kind.equals("constructor")
                &&(!symbol.visibility.equals("private")||cls==current)&&(!symbol.visibility.equals("protected")||inherits(current,cls,new HashSet<>())))result.putIfAbsent(symbol.name,symbol);cls=type(cls.base,cls.file);}
        }else{

            for(Scope scope=s.scope(offset);scope!=null;scope=scope.parent)for(Symbol symbol:symbols)if(symbol.file==file&&symbol.scope==scope&&(!symbol.kind.equals("local")||symbol.offset<offset))result.putIfAbsent(symbol.name,symbol);
            for(Symbol symbol:symbols)if(symbol.scope.parent==null&&!symbol.kind.equals("import")&&sameNamespace(file,symbol.file))result.putIfAbsent(symbol.name,symbol);
            for(String name:List.of("Exception","GoError","StackFrame"))result.putIfAbsent(name,builtin(name,file));
        }
        return completionValues(result,construction,file);
    }
    private static int calleeIndex(List<Token> tokens,int index){
        if(is(tokens,index,"]")){
            int depth=1;
            for(int i=index-1;i>=0;i--){if(is(tokens,i,"]"))depth++;if(is(tokens,i,"[")&&--depth==0)return i-1;}
        }
        return index;
    }
    private static boolean constructionContext(Source source,int index){
        var tokens=source.tokens;
        if(index>=tokens.size()||!tokens.get(index).identifier)index--;
        while(index>=0){
            if(is(tokens,index,"new"))return true;
            if(!tokens.get(index).identifier&&!is(tokens,index,"."))return false;
            index--;
        }
        return false;
    }
    private static Symbol builtin(String name,PsiFile file){
        List<String> parameters=switch(name){
            case "Exception"->List.of("message string = \"\"","code int = 0");
            case "GoError"->List.of("cause error","code int = 0");
            case "StackFrame"->List.of("functionName string","file string","line int");
            default->null;
        };
        if(parameters==null)return null;
        Symbol symbol=new Symbol(name,"class",-1,file,null);symbol.parameters=parameters;return symbol;
    }
    private static List<Symbol> completionValues(LinkedHashMap<String,Symbol> values,boolean construction,PsiFile file){
        if(construction)values.values().removeIf(symbol->!symbol.kind.equals("class")&&!symbol.kind.equals("import"));
        return new ArrayList<>(values.values());
    }    record Call(Symbol symbol,int open,int parameter) {}
    Call callAt(PsiFile file,int offset){Source s=sources.get(file);Deque<Integer> stack=new ArrayDeque<>();for(int i=0;i<s.tokens.size()&&s.tokens.get(i).start<offset;i++){if(is(s.tokens,i,"("))stack.push(i);else if(is(s.tokens,i,")")&&!stack.isEmpty())stack.pop();}
        if(stack.isEmpty())return null;int open=stack.peek();int callee=calleeIndex(s.tokens,open-1);
        Symbol callable=resolveToken(s,callee,new HashSet<>());
        if(callable==null && callee>=0 && !is(s.tokens,callee-1,"."))callable=builtin(s.tokens.get(callee).text,file);
        if(callable==null)return null;
        if(callable.kind.equals("class")){Symbol ctor=member(callable,"constructor",new HashSet<>());if(ctor!=null)callable=ctor;}
        int parameter=0,depth=0;for(int i=open+1;i<s.tokens.size()&&s.tokens.get(i).start<offset;i++){String token=s.tokens.get(i).text;if(Set.of("(","[","{").contains(token))depth++;if(Set.of(")","]","}").contains(token))depth--;if(token.equals(",")&&depth==0)parameter++;}
        return new Call(callable,s.tokens.get(open).start,parameter);
    }
}



