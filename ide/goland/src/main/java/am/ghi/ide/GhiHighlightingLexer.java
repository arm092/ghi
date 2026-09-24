package am.ghi.ide;

import com.intellij.lexer.LexerBase;
import com.intellij.psi.TokenType;
import com.intellij.psi.tree.IElementType;
import java.util.*;

/** Contextual name colors. This does not claim cross-file symbol resolution. */
public final class GhiHighlightingLexer extends LexerBase {
    public static final IElementType TYPE=type("TYPE"), INTERFACE=type("INTERFACE"),
        BUILTIN_TYPE=type("BUILTIN_TYPE"), FUNCTION=type("FUNCTION"), METHOD=type("METHOD"),
        PARAMETER=type("PARAMETER"), FIELD=type("FIELD"), NAMESPACE=type("NAMESPACE"),
        CONSTANT=type("CONSTANT"), LOCAL=type("LOCAL");
    private static IElementType type(String name){return new IElementType("GHI_"+name,GhiLanguage.INSTANCE);}
    private static final Set<String> BUILTINS=Set.of("string","bool","byte","rune","int","int8","int16","int32","int64",
        "uint","uint8","uint16","uint32","uint64","uintptr","float32","float64","complex64","complex128","any","error");
    private record Word(String text,IElementType type,int start,int end){}
    private record Scope(int from,int to,Set<String> names){}
    private final GhiLexer delegate=new GhiLexer();
    private final Map<Integer,IElementType> colors=new HashMap<>();
    private List<Word> words;
    private CharSequence source;
    private int[] close;
    private final List<Scope> parameters=new ArrayList<>();
    private final BitSet classMembers=new BitSet(),functionMembers=new BitSet();

    public void start(CharSequence buffer,int startOffset,int endOffset,int initialState){
        source=buffer;colors.clear();classMembers.clear();functionMembers.clear();parameters.clear();words=new ArrayList<>();
        var scan=new GhiLexer();scan.start(buffer);
        while(scan.getTokenType()!=null){
            if(scan.getTokenType()!=TokenType.WHITE_SPACE && scan.getTokenType()!=GhiLexer.COMMENT)
                words.add(new Word(buffer.subSequence(scan.getTokenStart(),scan.getTokenEnd()).toString(),scan.getTokenType(),scan.getTokenStart(),scan.getTokenEnd()));
            scan.advance();
        }
        classify();delegate.start(buffer,startOffset,endOffset,initialState);
    }
    private String text(int i){return i>=0 && i<words.size()?words.get(i).text():"";}
    private boolean id(int i){return i>=0 && i<words.size() && words.get(i).type()==GhiLexer.IDENTIFIER;}
    private void color(int i,IElementType type){if(i>=0 && i<words.size())colors.put(words.get(i).start(),type);}
    private boolean newline(int left,int right){
        if(left<0 || right>=words.size())return true;
        for(int p=words.get(left).end();p<words.get(right).start();p++)if(source.charAt(p)=='\n')return true;
        return false;
    }
    private void classify(){
        close=new int[words.size()];Arrays.fill(close,-1);
        var stack=new ArrayDeque<Integer>();
        for(int i=0;i<words.size();i++){
            String t=text(i);
            if(Set.of("(","[","{").contains(t))stack.push(i);
            else if(Set.of(")","]","}").contains(t) && !stack.isEmpty()){
                int open=stack.peek();
                if((text(open)+t).equals("()") || (text(open)+t).equals("[]") || (text(open)+t).equals("{}")){close[stack.pop()]=i;}
            }
        }
        Map<String,IElementType> types=new HashMap<>();Set<String> namespaces=new HashSet<>(),constants=new HashSet<>();
        for(int i=0;i<words.size();i++){
            String t=text(i);
            if(Set.of("class","interface","type").contains(t) && id(i+1)){
                var role=t.equals("interface")?INTERFACE:TYPE;types.put(text(i+1),role);color(i+1,role);
                if(!t.equals("type")){
                    int body=i+2;while(body<words.size() && !text(body).equals("{"))body++;
                    if(body<words.size())classMembers.set(body+1,close[body]<0?words.size():close[body]);
                }
            }
            if(t.equals("namespace"))for(int j=i+1;j<words.size() && !newline(j-1,j) && !text(j).equals(";");j++)if(id(j))color(j,NAMESPACE);
            if(t.equals("import") && id(i+1)){namespaces.add(text(i+1));color(i+1,NAMESPACE);}
            if(t.equals("const") && id(i+1)){constants.add(text(i+1));color(i+1,CONSTANT);}
        }
        for(int i=0;i<words.size();i++){
            if(text(i).equals("func") || text(i).equals("constructor")){
                int open=i+1;
                if(id(open)){
                    color(open,inClass(i)?METHOD:FUNCTION);open++;
                }
                if(!text(open).equals("(") || close[open]<0)continue;
                int end=close[open],scopeEnd=end;
                int body=end+1;
                // Return types can include parentheses, arrays, pointers and qualifiers.
                while(body<words.size() && !text(body).equals("{") && !text(body).equals(";") && !newline(body-1,body))body++;
                if(text(body).equals("{"))scopeEnd=close[body]<0?words.size():close[body];
                Set<String> names=new HashSet<>();
                for(int at=open+1;at<end;){
                    int next=at;
                    while(next<end && !text(next).equals(",")){
                        if(close[next]>next)next=close[next];
                        next++;
                    }
                    if(id(at) && !BUILTINS.contains(text(at)) && !types.containsKey(text(at))){
                        // A lone identifier followed by a comma is a grouped parameter name.
                        if(at+1<next || next<end){names.add(text(at));color(at,PARAMETER);}
                    }
                    at=next+1;
                }
                parameters.add(new Scope(open,scopeEnd,names));
                functionMembers.set(open+1,scopeEnd);
            }
        }
        // Scan each function body once for its parameter references. Avoid searching
        // every function scope for every token on each editor rehighlight.
        for(Scope scope:parameters)for(int i=scope.from()+1;i<scope.to();i++){
            if(id(i) && scope.names().contains(text(i)) && !text(i-1).equals(".") && !colors.containsKey(words.get(i).start()))color(i,PARAMETER);
        }
        for(int i=0;i<words.size();i++){
            if(!id(i) || colors.containsKey(words.get(i).start()))continue;
            String t=text(i),before=text(i-1),after=text(i+1);
            if(before.equals(".")){color(i,after.equals("(")?METHOD:types.getOrDefault(t,FIELD));continue;}
            if(BUILTINS.contains(t)){color(i,BUILTIN_TYPE);continue;}
            if(types.containsKey(t)){color(i,types.get(t));continue;}
            if(namespaces.contains(t) && after.equals(".")){color(i,NAMESPACE);continue;}
            if(after.equals("(")){color(i,FUNCTION);continue;}
            if(constants.contains(t)){color(i,CONSTANT);continue;}
            if(inClass(i) && outsideFunction(i) && (id(i+1) || Set.of("[","*","?","map","chan").contains(after))){color(i,FIELD);continue;}
            color(i,LOCAL);
        }
    }
    private boolean inClass(int i){return classMembers.get(i);}
    private boolean outsideFunction(int i){return !functionMembers.get(i);}
    public int getState(){return delegate.getState();}
    public IElementType getTokenType(){var token=delegate.getTokenType();return token==null?null:colors.getOrDefault(delegate.getTokenStart(),token);}
    public int getTokenStart(){return delegate.getTokenStart();}
    public int getTokenEnd(){return delegate.getTokenEnd();}
    public void advance(){delegate.advance();}
    public CharSequence getBufferSequence(){return delegate.getBufferSequence();}
    public int getBufferEnd(){return delegate.getBufferEnd();}
}
