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
        String name, kind, type="", base="", visibility="public", importNamespace="", resultSignature=""; int offset;
        PsiFile file; Scope scope, body; List<String> parameters=new ArrayList<>();
        List<String> typeParameters=new ArrayList<>(), contracts=new ArrayList<>();
        PsiElement external; boolean explicitAlias, callable;
        Symbol(String name,String kind,int offset,PsiFile file,Scope scope) {
            this.name=name;this.kind=kind;this.offset=offset;this.file=file;this.scope=scope;
        }
        PsiElement psi() { if(external!=null)return external;if(offset<0)return null;return PsiTreeUtil.getParentOfType(file.findElementAt(offset),GhiIdentifier.class,false); }
    }
    static final class Source {
        PsiFile file; String namespace=""; List<Token> tokens=new ArrayList<>();
        List<Scope> scopes=new ArrayList<>(); Map<Integer,Symbol> declarations=new HashMap<>(), arrows=new HashMap<>(); Set<Integer> importPathTokens=new HashSet<>(); Map<Integer,Symbol> selectedPaths=new HashMap<>();
        Source(PsiFile file) { this.file=file; }
        Scope scope(int offset) {
            Scope result=scopes.getFirst();
            for(Scope scope:scopes)if(scope.contains(offset)&&scope.start>=result.start)result=scope;
            return result;
        }
    }
    final List<Symbol> symbols=new ArrayList<>(); final Map<PsiFile,Source> sources=new LinkedHashMap<>();
    static GhiSymbols forFile(PsiFile file) {
        // The lock and installed tree can change outside PSI edits (for example, mojave update).
        // Rebuild these models so removed packages cannot survive in completion caches.
        if(GhiDependencies.applicationRoot(file)!=null)return build(file);
        return com.intellij.psi.util.CachedValuesManager.getCachedValue(file, () -> com.intellij.psi.util.CachedValueProvider.Result.create(build(file), com.intellij.psi.util.PsiModificationTracker.MODIFICATION_COUNT));
    }
    private static GhiSymbols build(PsiFile file) {
        GhiSymbols result=new GhiSymbols(); result.add(file);
        var root=GhiDependencies.applicationRoot(file);
        if(!DumbService.isDumb(file.getProject()))for(var vf:FileTypeIndex.getFiles(GhiFileType.INSTANCE,GlobalSearchScope.projectScope(file.getProject()))) {
            if(file.getVirtualFile()!=null&&vf.equals(file.getVirtualFile())||GhiDependencies.generated(vf)
                ||root!=null&&!GhiDependencies.inApplication(vf,root))continue;
            PsiFile other=PsiManager.getInstance(file.getProject()).findFile(vf);
            if(other!=null)result.add(other);
        }
        GhiDependencies.add(file,result);
        return result;
    }
    void addDependency(PsiFile file,String namespace){
        int before=symbols.size();add(file);
        String actual=sources.get(file).namespace;
        if(!actual.equals(namespace)&&!actual.startsWith(namespace+".")){
            sources.remove(file);symbols.subList(before,symbols.size()).clear();
        }
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
            if(word.equals("import")&&i+1<ts.size()&&ts.get(i+1).identifier&&is(ts,i+2,".")){
                int end=i+1;while(is(ts,end+1,".")&&end+2<ts.size()&&ts.get(end+2).identifier)end+=2;
                for(int at=i+1;at<=end;at+=2)s.importPathTokens.add(ts.get(at).start);
                boolean aliased=is(ts,end+1,"as")&&end+2<ts.size()&&ts.get(end+2).identifier;
                Symbol selected=declare(s,ts.get(aliased?end+2:end),"typeImport",root);selected.explicitAlias=aliased;
                selected.importNamespace=qualified(ts,i+1);s.selectedPaths.put(ts.get(end).start,selected);continue;
            }
            if(Set.of("class","interface","type","func","import").contains(word)&&i+1<ts.size()&&ts.get(i+1).identifier){
                int nameIndex=i+1;Token name=ts.get(nameIndex);Scope scope=s.scope(t.start);
                Symbol symbol=declare(s,name,word,scope);symbol.visibility=scope.owner!=null&&scope.owner.kind.equals("interface")?"public":visibility(ts,i);
                if(is(ts,nameIndex+1,"[")){
                    int end=matching(ts,nameIndex+1,"[","]");
                    if(end>=0)for(String parameter:GhiGenericTypes.arguments(typeAt(ts,nameIndex)))symbol.typeParameters.add(parameter.split("[^\\p{L}\\p{N}_]",2)[0]);
                }
                if(word.equals("import")&&nameIndex+1<ts.size())symbol.importNamespace=ts.get(nameIndex+1).text.replace("\"","");
                if(word.equals("class")||word.equals("interface")||word.equals("func")) {
                    int open=find(ts,nameIndex+1,"{",Set.of(";","}"));
                    if(word.equals("func")) {
                        int paren=nameIndex+1;if(is(ts,paren,"[")){int genericEnd=matching(ts,paren,"[","]");if(genericEnd>=0)paren=genericEnd+1;}
                        if(is(ts,paren,"(")){int close=matching(ts,paren,"(",")");
                            if(close>=0){open=is(ts,close+1,"{")?close+1:findOnLine(ts,close+1,"{",text);
                                symbol.type=typeAt(ts,close+1);
                                int resultEnd=close+1;
                                while(resultEnd<ts.size()&&!Set.of("{","}",";").contains(ts.get(resultEnd).text)&&!lineBreak(text,ts.get(resultEnd-1).end,ts.get(resultEnd).start))resultEnd++;
                                if(resultEnd>close+1)symbol.resultSignature=text.substring(ts.get(close+1).start,ts.get(resultEnd-1).end);
                                if(open>=0)symbol.body=scopeAt(s,ts.get(open).start);
                                parameters(s,symbol,paren+1,close,text);
                            }
                        }
                    } else {
                        if(open>=0)symbol.body=scopeAt(s,ts.get(open).start);
                        for(int k=nameIndex+1;k<ts.size()&&(open<0||k<open);k++)if(is(ts,k,"extends")){symbol.base=typeAt(ts,k+1);break;}
                    }
                    if(!word.equals("func"))for(int k=nameIndex+1;k<ts.size()&&(open<0||k<open);k++)if(is(ts,k,"implements")){
                        for(int at=k+1;at<ts.size()&&(open<0||at<open);at++)if(ts.get(at).identifier&&(at==k+1||is(ts,at-1,",")))symbol.contracts.add(typeAt(ts,at));
                        break;
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
            if(word.equals("(")){
                Arrow arrow=arrowAt(ts,i,text);
                if(arrow!=null){
                    Scope body=scopeAt(s,ts.get(arrow.body).start);
                    if(body!=null){
                        Symbol function=new Symbol("<arrow>","arrow",t.start,file,s.scope(t.start));
                        function.body=body;function.type=typeAt(ts,arrow.close+1);
                        if(arrow.arrow>arrow.close+1)function.resultSignature=text.substring(ts.get(arrow.close+1).start,ts.get(arrow.arrow-1).end);
                        body.owner=function;s.arrows.put(t.start,function);
                        parameters(s,function,i+1,arrow.close,text);
                        namedArrowResults(s,function,arrow.close+1,arrow.arrow);
                    }
                    i=arrow.body;
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
                int value=shortDecl?i+3:is(ts,i+1,"=")?i+2:-1;
                if(value>=0&&value<ts.size()){
                    Symbol arrow=s.arrows.get(ts.get(value).start);
                    if(arrow!=null){symbol.callable=true;symbol.parameters.addAll(arrow.parameters);symbol.resultSignature=arrow.resultSignature;symbol.type=arrow.type;}
                }
            }
        }
    }
    private static boolean lineBreak(String text,int from,int to){return text.substring(from,to).contains("\n");}
    private record Arrow(int close,int arrow,int body){}
    private static Arrow arrowAt(List<Token> ts,int open,String text){
        int close=matching(ts,open,"(",")");if(close<0)return null;
        for(int at=close+1;at<ts.size();at++){
            if(lineBreak(text,ts.get(at-1).end,ts.get(at).start))return null;
            if(is(ts,at,"=>"))return is(ts,at+1,"{")?new Arrow(close,at,at+1):null;
            if(Set.of(";","{","}",",").contains(ts.get(at).text))return null;
            if(is(ts,at,"(")||is(ts,at,"[")){
                int end=matching(ts,at,ts.get(at).text,is(ts,at,"(")?")":"]");
                if(end<0)return null;at=end;
            }
        }
        return null;
    }
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
    private void namedArrowResults(Source s,Symbol function,int open,int arrow){
        if(!is(s.tokens,open,"("))return;
        int end=matching(s.tokens,open,"(",")");if(end<0||end>=arrow)return;
        for(int at=open+1;at<end;){
            int next=at;
            while(next<end&&!is(s.tokens,next,",")){
                if(is(s.tokens,next,"(")||is(s.tokens,next,"[")){
                    int nested=matching(s.tokens,next,s.tokens.get(next).text,is(s.tokens,next,"(")?")":"]");
                    if(nested>next)next=nested;
                }
                next++;
            }
            if(at+1<next&&s.tokens.get(at).identifier&&!is(s.tokens,at+1,".")&&!is(s.tokens,at+1,"["))
                declare(s,s.tokens.get(at),"result",function.body);
            at=next+1;
        }
    }
    static boolean is(List<Token> ts,int i,String text){return i>=0&&i<ts.size()&&ts.get(i).text.equals(text);}
    private static int find(List<Token> ts,int start,String text,Set<String> stop){for(int i=start;i<ts.size();i++){if(is(ts,i,text))return i;if(stop.contains(ts.get(i).text))break;}return -1;}
    private static int findOnLine(List<Token> ts,int start,String value,String text){for(int i=start;i<ts.size();i++){if(i>0&&lineBreak(text,ts.get(i-1).end,ts.get(i).start))break;if(is(ts,i,value))return i;if(is(ts,i,"}"))break;}return -1;}
    static int matching(List<Token> ts,int start,String open,String close){int depth=0;for(int i=start;i<ts.size();i++){if(is(ts,i,open))depth++;if(is(ts,i,close)&&--depth==0)return i;}return -1;}
    private static String typeAt(List<Token> ts,int index){
        while(is(ts,index,"new")||is(ts,index,"?")||is(ts,index,"*")||is(ts,index,"&"))index++;
        if(index>=ts.size()||!ts.get(index).identifier)return "";
        String name=qualified(ts,index);int after=index+1;
        while(is(ts,after,".")&&after+1<ts.size()&&ts.get(after+1).identifier)after+=2;
        if(is(ts,after,"[")){int end=matching(ts,after,"[","]");if(end>=0){StringBuilder generic=new StringBuilder();for(int i=after;i<=end;i++){if(i>after&&ts.get(i-1).identifier&&ts.get(i).identifier)generic.append(' ');generic.append(ts.get(i).text);}name+=generic;}}
        return name;
    }
    private static String qualified(List<Token> ts,int index){StringBuilder name=new StringBuilder();while(index<ts.size()&&ts.get(index).identifier){name.append(ts.get(index++).text);if(!is(ts,index,".")||index+1>=ts.size()||!ts.get(index+1).identifier)break;name.append('.');index++;}return name.toString();}
    private static String visibility(List<Token> ts,int index){for(int i=index-1;i>=Math.max(0,index-3);i--){String word=ts.get(i).text;if(Set.of("public","private","protected").contains(word))return word;if(!word.equals("override"))break;}return "private";}
    private boolean sameNamespace(PsiFile first,PsiFile second){return sources.get(first).namespace.equals(sources.get(second).namespace);}
    Symbol symbolAt(PsiFile file,int offset){Source s=sources.get(file);if(s==null)return null;for(int i=0;i<s.tokens.size();i++){Token token=s.tokens.get(i);if(token.start<=offset&&offset<token.end)return resolveToken(s,i,new HashSet<>());}return null;}
    PsiElement resolve(PsiFile file,int offset){Symbol symbol=symbolAt(file,offset);return symbol==null?null:symbol.psi();}
    private Symbol resolveToken(Source s,int index,Set<Integer> visited){
        if(index<0||!visited.add(index))return null;Token token=s.tokens.get(index);
        Symbol selected=s.selectedPaths.get(token.start);if(selected!=null)return selectedTarget(selected);
        Symbol declaration=s.declarations.get(token.start);if(declaration!=null)return declaration;
        if(s.importPathTokens.contains(token.start))return null;
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
            if(result!=null)return at.parent==null&&hasSelectedImport(s.file,token.text)?selectedBinding(s.file,token.text):result;
        }
        for(Symbol symbol:symbols)if(symbol.scope.parent==null&&!isImport(symbol)&&symbol.name.equals(token.text)&&sameNamespace(s.file,symbol.file))return symbol;
        return null;
    }
    private Symbol receiver(Source s,int index,Set<Integer> visited){
        if(index<0)return null;
        if(is(s.tokens,index,")")){int depth=1;for(int i=index-1;i>=0;i--){if(is(s.tokens,i,")"))depth++;if(is(s.tokens,i,"(")&&--depth==0){Symbol call=resolveToken(s,calleeIndex(s.tokens,i-1),visited);if(call==null)return null;Symbol result=asType(call,s.file);
            if(result!=null&&(call.kind.equals("class")||call.kind.equals("typeImport"))){Symbol concrete=new Symbol(result.name,result.kind,result.offset,result.file,result.scope);concrete.body=result.body;concrete.base=result.base;concrete.type=typeAt(s.tokens,calleeIndex(s.tokens,i-1));return concrete;}return result;}}return null;}
        return resolveToken(s,index,visited);
    }
    private Symbol imported(Source s,Symbol alias,String name){
        String namespace=alias.importNamespace;
        if(namespace.startsWith("go:"))return GhiGoSymbols.packageMembers(namespace.substring(3),s.file).stream().filter(symbol->symbol.name.equals(name)).findFirst().orElse(null);
        for(Symbol symbol:symbols)if(symbol.scope.parent==null&&!isImport(symbol)&&symbol.name.equals(name)&&sources.get(symbol.file).namespace.equals(namespace))return symbol;
        return null;
    }
    private static boolean isImport(Symbol symbol){return symbol.kind.equals("import")||symbol.kind.equals("typeImport");}
    private static boolean isType(Symbol symbol){return Set.of("class","interface","type").contains(symbol.kind);}
    private Symbol selectedTarget(Symbol selected){
        Symbol found=null;
        for(Symbol candidate:symbols)if(isType(candidate)&&candidate.scope.parent==null&&(sources.get(candidate.file).namespace+"."+candidate.name).equals(selected.importNamespace)){
            if(found!=null)return null;found=candidate;
        }
        return found;
    }
    private boolean hasSelectedImport(PsiFile file,String name){return symbols.stream().anyMatch(symbol->symbol.file==file&&symbol.kind.equals("typeImport")&&symbol.name.equals(name));}
    private Symbol selectedBinding(PsiFile file,String name){
        Symbol found=null;int count=0;
        for(Symbol candidate:symbols)if(candidate.scope.parent==null&&candidate.name.equals(name)){
            if(candidate.file==file&&isImport(candidate)){count++;found=candidate.kind.equals("typeImport")&&!candidate.explicitAlias?selectedTarget(candidate):candidate;}
            else if(!isImport(candidate)&&sameNamespace(file,candidate.file))count++;
        }
        return count==1?found:null;
    }
    boolean importedRenameCollision(Symbol target,String newName){
        for(Symbol selected:symbols)if(selected.kind.equals("typeImport")&&!selected.explicitAlias&&selectedTarget(selected)==target){
            if(selectedBinding(selected.file,selected.name)!=target)return true;
            for(Symbol candidate:symbols)if(candidate!=target&&((candidate.file==selected.file&&candidate.typeParameters.contains(newName))||(candidate.name.equals(newName)
                &&(candidate.file==selected.file||(!isImport(candidate)&&candidate.scope.parent==null&&sameNamespace(selected.file,candidate.file))))))return true;
        }
        return false;
    }
    Symbol type(String name,PsiFile context){
        name=GhiGenericTypes.base(name);
        if(hasSelectedImport(context,name)){Symbol binding=selectedBinding(context,name);return binding!=null&&binding.kind.equals("typeImport")?selectedTarget(binding):binding;}
        int dot=name.indexOf('.');
        if(dot>0)for(Symbol alias:symbols)if(alias.file==context&&alias.kind.equals("import")&&alias.name.equals(name.substring(0,dot)))return imported(sources.get(context),alias,name.substring(dot+1));
        for(Symbol symbol:symbols)if(Set.of("class","interface","type").contains(symbol.kind)&&(symbol.name.equals(name)&&sameNamespace(context,symbol.file)||(sources.get(symbol.file).namespace+"."+symbol.name).equals(name)))return symbol;return null;}
    private Symbol asType(Symbol symbol,PsiFile context){
        if(symbol.kind.equals("typeImport"))return selectedTarget(symbol);
        if(symbol.external!=null)return GhiGoSymbols.resultType(symbol);
        if(Set.of("class","interface","type").contains(symbol.kind))return symbol;
        Symbol result=type(symbol.type,context);return result!=null&&result.external!=null?GhiGoSymbols.resultType(result):result;
    }
    private Symbol member(Symbol type,String name,Set<Symbol> visited){if(type==null||!visited.add(type))return null;if(type.external!=null)return GhiGoSymbols.members(type).stream().filter(symbol->symbol.name.equals(name)).findFirst().orElse(null);for(Symbol symbol:symbols)if(symbol.scope==type.body&&symbol.name.equals(name))return symbol;return member(type(type.base,type.file),name,visited);}
    private static Symbol enclosingClass(Scope scope){for(;scope!=null;scope=scope.parent)if(scope.owner!=null&&scope.owner.kind.equals("class"))return scope.owner;return null;}
    private boolean inherits(Symbol child,Symbol parent,Set<Symbol> visited){
        if(child==null||!visited.add(child))return false;
        if(child==parent||inherits(type(child.base,child.file),parent,visited))return true;
        if(child.kind.equals("class")&&parent!=null&&parent.kind.equals("interface")&&structurallyImplements(child,parent))return true;
        for(String contract:child.contracts)if(inherits(type(contract,child.file),parent,visited))return true;return false;
    }
    private boolean structurallyImplements(Symbol implementation,Symbol contract){
        if(!contract.typeParameters.isEmpty())return false;
        boolean hasMethod=false;
        for(Symbol requirement:symbols)if(requirement.kind.equals("func")&&requirement.scope==contract.body){
            hasMethod=true;Symbol actual=member(implementation,requirement.name,new HashSet<>());
            if(actual==null||!actual.kind.equals("func")||!actual.visibility.equals("public")||!sameSignature(actual,requirement))return false;
        }
        return hasMethod;
    }
    private boolean sameSignature(Symbol first,Symbol second){
        if(first.parameters.size()!=second.parameters.size())return false;
        if(!canonicalType(first.resultSignature,first.file).equals(canonicalType(second.resultSignature,second.file)))return false;
        for(int i=0;i<first.parameters.size();i++){
            String left=parameterType(first.parameters.get(i)),right=parameterType(second.parameters.get(i));
            if(left.isEmpty()||right.isEmpty()||!canonicalType(left,first.file).equals(canonicalType(right,second.file)))return false;
        }
        return true;
    }
    private static String parameterType(String parameter){
        var lexer=new GhiLexer();lexer.start(parameter);if(lexer.getTokenType()!=GhiLexer.IDENTIFIER)return "";
        int nameEnd=lexer.getTokenEnd(),end=parameter.indexOf('=',nameEnd);
        return parameter.substring(nameEnd,end<0?parameter.length():end).trim();
    }
    private String canonicalType(String expression,PsiFile context){
        Map<String,String> names=new LinkedHashMap<>(Map.of("byte","uint8","rune","int32","any","interface{}"));
        for(Symbol symbol:symbols)if(symbol.scope.parent==null){
            if(symbol.file==context&&isImport(symbol))names.put(symbol.name,symbol.importNamespace);
            else if(Set.of("class","interface","type").contains(symbol.kind)&&sameNamespace(context,symbol.file))names.put(symbol.name,sources.get(symbol.file).namespace+"."+symbol.name);
        }
        return GhiGenericTypes.substitute(expression,names).replaceAll("\\s+","");
    }
    private boolean uncertainSignature(Symbol method){
        if(!method.typeParameters.isEmpty()||(method.scope.owner!=null&&!method.scope.owner.typeParameters.isEmpty()))return true;
        if(method.resultSignature.startsWith("(")||method.parameters.stream().anyMatch(parameter->parameterType(parameter).isEmpty()||parameter.contains("func")))return true;
        String signature=String.join(" ",method.parameters)+" "+method.resultSignature;
        for(Symbol declared:symbols)if(declared.kind.equals("type")&&signature.matches("(?s).*\\b"+java.util.regex.Pattern.quote(declared.name)+"\\b.*"))return true;
        return false;
    }
    boolean ambiguousStructuralFamily(Symbol target){
        if(!target.kind.equals("func")||target.scope==null||target.scope.owner==null)return false;
        for(Symbol contract:symbols)if(contract.kind.equals("interface")){
            Symbol requirement=member(contract,target.name,new HashSet<>());if(requirement==null)continue;
            if(!contract.typeParameters.isEmpty()&&(target.scope.owner==contract||target.visibility.equals("public")))return true;
            for(Symbol candidate:symbols)if(candidate.kind.equals("func")&&candidate.name.equals(target.name)&&candidate.visibility.equals("public"))
                if(uncertainSignature(requirement)||uncertainSignature(candidate))return true;
        }
        return false;
    }
    boolean related(Symbol first,Symbol second){
        if(inherits(first,second,new HashSet<>())||inherits(second,first,new HashSet<>()))return true;
        if(first.kind.equals("interface")||second.kind.equals("interface"))for(Symbol witness:symbols)
            if(witness.kind.equals("class")&&inherits(witness,first,new HashSet<>())&&inherits(witness,second,new HashSet<>()))return true;
        return false;
    }
    Set<Symbol> methodFamily(Symbol target){
        Set<Symbol> result=new LinkedHashSet<>();result.add(target);
        if(target.visibility.equals("private")||!target.kind.equals("func")||target.scope==null||target.scope.owner==null)return result;
        boolean changed;
        do {changed=false;for(Symbol candidate:symbols){
            if(result.contains(candidate)||candidate.visibility.equals("private")||!candidate.kind.equals("func")||!candidate.name.equals(target.name)||candidate.scope.owner==null)continue;
            for(Symbol member:new ArrayList<>(result))if(related(member.scope.owner,candidate.scope.owner)){result.add(candidate);changed=true;break;}
        }}while(changed);
        return result;
    }
    List<Symbol> complete(PsiFile file,int offset){
        Source s=sources.get(file);int index=0;while(index<s.tokens.size()&&s.tokens.get(index).end<=offset)index++;
        int dot=is(s.tokens,index-1,".")?index-1:is(s.tokens,index-2,".")?index-2:-1;
        // Import completion is restricted to the selected namespace's types.
        int importStart=index-1;
        while(importStart>=0&&(s.tokens.get(importStart).identifier||is(s.tokens,importStart,".")))importStart--;
        if(is(s.tokens,importStart,"import")){
            String namespace="";
            if(dot>=0){StringBuilder path=new StringBuilder();for(int at=importStart+1;at<dot;at++)path.append(s.tokens.get(at).text);namespace=path.toString();}
            final String selectedNamespace=namespace;
            return symbols.stream().filter(symbol->isType(symbol)&&symbol.scope.parent==null&&sources.get(symbol.file).namespace.equals(selectedNamespace)).toList();
        }
        boolean construction=constructionContext(s,index);
        LinkedHashMap<String,Symbol> result=new LinkedHashMap<>();
        if(dot>=0){Symbol receiver=receiver(s,dot-1,new HashSet<>());if(receiver==null)return List.of();
            if(receiver.kind.equals("import")){
                if(receiver.importNamespace.startsWith("go:"))return GhiGoSymbols.packageMembers(receiver.importNamespace.substring(3),file);
                for(Symbol symbol:symbols)if(symbol.scope.parent==null&&!isImport(symbol)&&sources.get(symbol.file).namespace.equals(receiver.importNamespace))result.putIfAbsent(symbol.name,symbol);
                return completionValues(result,construction,file);
            }
            Symbol cls=asType(receiver,file);if(cls!=null&&cls.external!=null)return GhiGoSymbols.members(cls);Set<Symbol> visited=new HashSet<>();Symbol current=enclosingClass(s.scope(offset));
            while(cls!=null&&visited.add(cls)){for(Symbol symbol:symbols)if(symbol.scope==cls.body&&!symbol.kind.equals("constructor")
                &&(!symbol.visibility.equals("private")||cls==current)&&(!symbol.visibility.equals("protected")||inherits(current,cls,new HashSet<>())))result.putIfAbsent(symbol.name,symbol);cls=type(cls.base,cls.file);}
        }else{

            for(Scope scope=s.scope(offset);scope!=null;scope=scope.parent)for(Symbol symbol:symbols)if(symbol.file==file&&symbol.scope==scope&&(!symbol.kind.equals("local")||symbol.offset<offset))result.putIfAbsent(symbol.name,symbol);
            for(Symbol symbol:symbols)if(symbol.scope.parent==null&&!isImport(symbol)&&sameNamespace(file,symbol.file))result.putIfAbsent(symbol.name,symbol);
            for(String name:List.of("Exception","GoError","StackFrame"))result.putIfAbsent(name,builtin(name,file));
        }
        if(dot<0)for(Symbol selected:symbols)if(selected.file==file&&selected.kind.equals("typeImport")){
            Symbol existing=result.get(selected.name);if(existing!=null&&existing.scope.parent!=null)continue;
            Symbol binding=selectedBinding(file,selected.name);if(binding==null)result.remove(selected.name);else result.put(selected.name,binding);
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
    private List<Symbol> completionValues(LinkedHashMap<String,Symbol> values,boolean construction,PsiFile file){
        if(construction)values.values().removeIf(symbol->!symbol.kind.equals("class")&&!symbol.kind.equals("import")&&!(symbol.kind.equals("typeImport")&&selectedTarget(symbol)!=null&&selectedTarget(symbol).kind.equals("class")));
        return new ArrayList<>(values.values());
    }    record Call(Symbol symbol,int open,int parameter) {}
    Call callAt(PsiFile file,int offset){Source s=sources.get(file);Deque<Integer> stack=new ArrayDeque<>();for(int i=0;i<s.tokens.size()&&s.tokens.get(i).start<offset;i++){if(is(s.tokens,i,"("))stack.push(i);else if(is(s.tokens,i,")")&&!stack.isEmpty())stack.pop();}
        if(stack.isEmpty())return null;int open=stack.peek();int callee=calleeIndex(s.tokens,open-1);
        Symbol callable=resolveToken(s,callee,new HashSet<>());
        if(callable==null && callee>=0 && !is(s.tokens,callee-1,"."))callable=builtin(s.tokens.get(callee).text,file);
        if(callable!=null&&callable.kind.equals("typeImport"))callable=selectedTarget(callable);
        if(callable==null)return null;
        Map<String,String> bindings=new LinkedHashMap<>();
        if(callable.kind.equals("class")){
            bindings.putAll(GhiGenericTypes.bind(callable.typeParameters,GhiGenericTypes.arguments(typeAt(s.tokens,callee))));
            Symbol ctor=member(callable,"constructor",new HashSet<>());if(ctor!=null)callable=ctor;
        }else{
            bindings.putAll(GhiGenericTypes.bind(callable.typeParameters,GhiGenericTypes.arguments(typeAt(s.tokens,callee))));
            if(callable.scope!=null&&callable.scope.owner!=null&&is(s.tokens,callee-1,".")){
                Symbol receiver=receiver(s,callee-2,new HashSet<>());
                if(receiver!=null)bindings.putAll(GhiGenericTypes.bind(callable.scope.owner.typeParameters,GhiGenericTypes.arguments(receiver.type)));
            }
        }
        if(!bindings.isEmpty()){
            Symbol concrete=new Symbol(callable.name,callable.kind,callable.offset,callable.file,callable.scope);
            concrete.parameters=callable.parameters.stream().map(parameter->GhiGenericTypes.parameter(parameter,bindings)).toList();
            concrete.type=GhiGenericTypes.substitute(callable.type,bindings);concrete.external=callable.external;callable=concrete;
        }
        int parameter=0,depth=0;for(int i=open+1;i<s.tokens.size()&&s.tokens.get(i).start<offset;i++){String token=s.tokens.get(i).text;if(Set.of("(","[","{").contains(token))depth++;if(Set.of(")","]","}").contains(token))depth--;if(token.equals(",")&&depth==0)parameter++;}
        return new Call(callable,s.tokens.get(open).start,parameter);
    }
}



